# 19 完整调用链与代码地图

## 本章前置

你已经完成 00–18 章。本章不再从零解释 HTTP、WebSocket、MySQL、Redis、Kafka 或 Kubernetes，而是把它们重新拼成当前代码。

## 本章目标

能从用户动作追到入口函数、核心结构体、存储和失败出口；能独立在源码中定位，而不是背文件名列表。

## 阅读代码的统一方法

每条链路都按同一顺序：

```text
外部输入
→ 协议结构
→ 网络入口
→ 业务编排
→ 状态/存储
→ 外部输出
→ 失败处理
```

## 链路一：登录

```text
public/index.html login()
→ POST /api/v1/login
→ cmd/gateway/internal/handler/loginhandler.go
→ cmd/gateway/internal/logic/loginlogic.go
→ LogicRouterPool 中的 gRPC client
→ cmd/logic/internal/server/logicserver.go
→ cmd/logic/internal/logic/loginlogic.go
→ internal/logic/handler.go Login
→ MySQL users
→ bcrypt 校验、JWT 签发
→ LoginReply
→ 浏览器页面内存保存 token
```

必须能指出两个进程边界：HTTP 到 Gateway，gRPC 从 Gateway 到 Logic。

## 链路二：WebSocket 建连

```text
new WebSocket(/ws?token=...)
→ AuthMiddleware / RateLimitMiddleware
→ websockethandler.go
→ Origin 校验与 Upgrade
→ ClientConn
→ ClientManager 注册本机连接
→ Redis route/gateway 反向索引
→ 回放 pending
→ 可选按一个 session_id + last_seq 补 Redis timeline
→ 启动读写和心跳
```

连接只存在本机 `ClientManager`。Redis 保存的是定位信息和短期状态，不保存 WebSocket 对象。

## 链路三：跨 Gateway 单聊

```text
A 在 Gateway-1 发送 WireMessage
→ Gateway 解码 Protobuf
→ PushWorkerPool 按 uid 分片
→ gRPC Logic.PushMessage
→ 校验 from、好友关系和输入
→ client_msg_id 幂等
→ 计算会话 ID
→ Redis Lua 分配 seq
→ MySQL INSERT messages
→ 更新 Redis 会话热状态；MySQL 会话摘要异步尽力更新
→ RedisDelivery.Deliver(B)
→ pending_ack / ack_idx / timeline 等短期状态
→ 查 route:B = Gateway-3|conn
→ PUBLISH Gateway-3 的频道
→ Gateway-3 SubscribeRedis 收到
→ 本机 ClientManager 找 B
→ WebSocket 写出
→ B 返回 ACK
→ 清理 pending/ack 状态并推进客户端收到进度
```

必须主动说明两条边界：MySQL 消息与会话摘要不是同一事务；Redis Pub/Sub 通知丢失不能靠 Pub/Sub 自己重放。

## 链路四：群聊

```text
A 发 group 消息
→ Gateway -> Logic
→ Logic 校验 A 是群成员
→ Logic 同步解析群成员 recipients
→ 幂等、seq、MySQL 消息落库
→ Kafka group topic，job 已携带 recipients
→ Transfer consumer group FetchMessage
→ 逐 recipient 领取 delivery lease
→ RedisDelivery
→ 成功 done；失败 retry/DLQ
→ 输出耐久后 CommitMessages
```

Transfer 不重新查询群成员，也没有按 Gateway 批量聚合。它解耦的是逐成员投递阶段，Logic 解析成员的成本仍存在。

## 链路五：红包

```text
网页 HTTP 请求
→ Gateway RedPacket Handler/Logic
→ RedPacketService
→ MySQL transaction
→ SELECT ... FOR UPDATE 锁红包行
→ 检查剩余份数和领取唯一约束
→ 写 claim 并更新剩余数量/金额
→ commit
→ 返回结果
```

红包不是 WebSocket 协议中的结构化卡片。当前网页创建后发送一条带红包 ID 的普通文本消息。

## 链路六：AI 虚拟好友

```text
用户向 9001 发送普通单聊
→ 原消息完成正常 PushMessage
→ Logic 启动非持久 goroutine
→ AskService 检索 docs/knowledge
→ mock 或 openai-compatible provider
→ 保存问答/attempt 记录
→ AI 回复作为 9001 发出的普通消息重新 PushMessage
```

Logic 崩溃会丢失尚未完成的 goroutine 任务。当前不是 Kafka/数据库任务队列，也不是自主 Agent。

## 十二个必须掌握的结构体

| 结构体 | 位置 | 先记职责 |
| --- | --- | --- |
| `api.WireMessage` | `api/protocol.proto`/生成代码 | WebSocket/gRPC 消息字段 |
| `ClientConn` | `internal/server/manager.go` | 一个用户在本 Gateway 的连接 |
| `ClientManager` | 同上 | 本机连接表 |
| `PushWorkerPool` | `internal/server/pool.go` | UID 分片有界队列 |
| Gateway `ServiceContext` | `cmd/gateway/internal/svc` | Gateway 共享依赖 |
| `LogicRouterPool` | 同上 | Etcd/zRPC Logic client |
| Logic `ServiceContext` | `cmd/logic/internal/svc` | Logic DB、Redis、Kafka 依赖 |
| `LogicHandler` | `internal/logic/handler.go` | 登录、消息、历史核心逻辑 |
| `RedisDelivery` | `internal/delivery/redis.go` | pending、route、Pub/Sub 投递 |
| `groupDispatchJob` | Logic/Transfer | Kafka 群任务载荷 |
| `RedPacketService` | `internal/logic/redpacket.go` | 红包事务 |
| `AskService` | `internal/ai/ask_service.go` | 检索、provider 和问答记录 |

每个结构体要按“谁创建、谁持有、关键字段、两个方法、一个失败路径”学习。

## 不要背生成代码

`.pb.go` 和 goctl 生成 Handler 只需要知道入口。把精力放在：

- `api/protocol.proto`
- `internal/logic/handler.go`
- `internal/delivery/redis.go`
- `internal/server/`
- `cmd/transfer/main.go`
- `internal/logic/redpacket.go`
- `internal/ai/`

## 动手练习

任选一条链路，关闭本手册后执行：

```bash
rg "PushMessage" cmd internal
rg "Deliver\(" internal cmd
rg "pending_ack" internal
rg "FetchMessage|CommitMessages" cmd/transfer
```

画出你实际看到的函数调用，不要照抄本章。

## 闭卷检查

1. 登录跨越哪两个进程？
2. WebSocket 对象保存在哪里？
3. 跨 Gateway 单聊在什么时候写 MySQL？
4. Transfer 收件人列表来自哪里？
5. ACK 清理哪些短期状态，它是否代表用户阅读？
6. AI 回复为什么可能在 Logic 宕机时丢失？

下一步：[20 面试讲述与逐层追问](20_INTERVIEW_PREP.md)
