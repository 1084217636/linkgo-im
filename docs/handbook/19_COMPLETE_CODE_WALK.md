# 19 完整调用链与代码地图

## 本章前置

你已经完成第 00 到 18 章。本章不再从零解释 HTTP、WebSocket、MySQL、Redis、Kafka 或 Kubernetes，而是把它们重新拼成当前代码。

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

箭头顺序不是为了画得整齐。每次读链路都要追问：这一步为什么必须在前，交换顺序会出现什么故障？本项目最重要的三个顺序约束是：

```text
先认证身份，再读取或修改用户资源；新消息落库/投递前再校验目标关系权限
先把消息正文提交 MySQL，再尝试实时通知
先把 pending 记好，再发可能瞬间完成的在线通知
```

反过来分别可能造成冒充/越权、客户端看到数据库中不存在的“幽灵消息”，以及消息已经到达但服务端还没建立 ACK 跟踪的窗口。`client_msg_id` 的预占/查重是一个发送者自有的幂等资源，所以它可以在目标好友/群权限前处理；用它恢复已落库消息的语义在第 11 章说明。

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

顺序理由：先查询账号并验证 bcrypt，才能签发带 user_id 的 JWT；如果先发 Token 再校验，等于把身份交给尚未认证的人。最近会话读取失败则被降级处理，因为它是登录后的附加数据，不应掩盖已经完成的身份校验。

## 链路二：WebSocket 建连

```text
new WebSocket(/ws?token=...)
→ AuthMiddleware / RateLimitMiddleware
→ websockethandler.go
→ Origin 校验与可选 session_id 资源授权
→ Upgrade
→ ClientConn
→ ClientManager 注册本机连接
→ Redis route/gateway 反向索引
→ 回放 pending
→ 可选按一个 session_id + last_seq 补 Redis timeline
→ 启动读写和心跳
```

连接只存在本机 `ClientManager`。Redis 保存的是定位信息和短期状态，不保存 WebSocket 对象。

顺序理由：JWT、限流、Origin 和可选的会话回放授权必须在 Upgrade 前检查。Upgrade 成功后已经进入长连接生命周期，再拒绝不仅难以返回标准 HTTP 错误，也会白白占用连接资源。连接登记到本机和 Redis 后，其他服务才知道后续通知应送到哪里。

## 链路三：跨 Gateway 单聊

```text
A 在 Gateway-1 发送 WireMessage
→ Gateway 解码 Protobuf
→ PushWorkerPool 按 uid 分片
→ gRPC Logic.PushMessage
→ 规范化输入，用认证 uid 校验 from
→ 预占 client_msg_id，查找已落库的重试消息
├→ 若找到数据库标准旧消息：重新校验该消息当前权限
│  → 保留旧 message_id/seq，跳过新序号和 INSERT
└→ 若是首次发送：计算会话 ID，再校验好友关系
   → Redis Lua 分配 seq
   → MySQL INSERT messages
→ 解析收件人并编码最终消息
→ RedisDelivery.Deliver(B)
→ pending_ack / ack_idx 等用户待确认状态
→ 查 route:B = Gateway-3|conn
→ PUBLISH Gateway-3 的频道
├→ Logic 路径：Redis session timeline / message payload
│  → 同步更新 Redis 会话热状态；MySQL 会话摘要异步尽力更新
└→ 并发的 Gateway-3 路径：SubscribeRedis 收到
   → 本机 ClientManager 找 B
   → WebSocket 写出
   → B 返回 ACK
   → 清理 pending/ack 状态并推进客户端收到进度
→ Gateway-1 Worker 在 Logic 调用返回后，向 A 写 MESSAGE_ACCEPTED 或 MESSAGE_REJECTED
```

必须主动说明两条边界：MySQL 消息与会话摘要不是同一事务；Redis Pub/Sub 通知丢失不能靠 Pub/Sub 自己重放。

PUBLISH 之后的两个分支并发执行，不能背成“timeline 一定先于客户端收到”。源码中 Logic 协程是 Deliver 返回后再 RememberSessionMessage，但目标 Gateway 可能已经并发处理通知。

顺序理由：MySQL 先于通知，保证接收方看到 message_id 后能够查到历史；pending 先于 Pub/Sub，保证快速到达并快速 ACK 时服务端已经有状态可清理。代价是 MySQL 提交到 pending/通知之间仍有 Outbox 缺口，所以这套顺序缩小了风险，但没有消除所有崩溃窗口。

## 链路四：群聊

```text
A 发 group 消息
→ Gateway -> Logic
→ 规范化输入并先处理 client_msg_id
→ 构造会话并校验 A 是群成员
→ seq、message_id、MySQL 消息落库
→ Logic 同步解析群成员 recipients
→ Kafka group topic，job 已携带 recipients
├→ Logic 路径：记录 timeline，更新会话热状态/异步摘要
└→ 并发的 Transfer 路径：consumer group FetchMessage
   → 逐 recipient 领取 delivery lease
   → RedisDelivery
   → 成功 done；失败 retry/DLQ
   → 输出耐久后 CommitMessages
```

Transfer 不重新查询群成员，也没有按 Gateway 批量聚合。它解耦的是逐成员投递阶段，Logic 解析成员的成本仍存在。

Kafka publish 返回后 Logic 才记录 timeline，但 Transfer 可能已经并发取到任务；这两个分支也没有全局先后保证。

顺序理由：Transfer 必须先完成收件人处理，或把失败任务可靠写入 retry/DLQ，才能提交原 Kafka offset。若先提交再处理，进程崩溃后任务会被跳过；若永不提交，重启后会无限重复。因此项目接受“可能重复”，再用收件人 lease 幂等约束副作用。

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

顺序理由：事务先 `SELECT ... FOR UPDATE` 读取锁定后的剩余量，再插入 claim、条件扣减并 commit，使同一红包的并发领取串行修改同一份余额。唯一索引仍负责拦截同一用户的重复记录；只加锁不加唯一约束，会把这项约束完全交给容易遗漏的应用判断。

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

顺序理由：用户原消息先按普通 IM 链路落库和投递，再异步触发模型。否则外部模型超时会把普通聊天一起拖慢甚至回滚。当前选择优先保证聊天主链路，代价是非持久 goroutine 可能丢失 AI 回复；要保证任务恢复，需要另加持久任务队列。

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

### 三张所有权示范卡

`ClientConn`：

```text
谁创建      WebSocket Handler 在 Upgrade 成功后调用 NewClientConn
谁持有      本机 ClientManager.UserConns，以 uid 为 key
关键字段    Conn、SessionID、writeMu
主要方法    WriteBinary、Close
失败路径    WriteBinary 失败后标记离线、比较清理 route、关闭并移除连接
```

这里需要 `writeMu`，因为 gorilla/websocket 不允许多个 goroutine 随意并发写同一连接；没有串行化可能出现并发写错误或帧损坏。

`LogicHandler`：

```text
谁创建      Logic 启动装配代码
谁持有      gRPC LogicServer
关键字段    Rdb、DB、Delivery、GroupDispatcher、BotResponder
主要方法    Login、PushMessage、GetHistory
失败路径    MySQL 已提交但后续投递失败时缺少事务 Outbox 自动恢复
```

它把依赖放在结构体中而不是每次函数里重新创建，便于复用连接池并在测试中注入替身；代价是装配错误会影响整个 Logic 实例。

`RedisDelivery`：

```text
谁创建      Logic/Transfer 启动装配
谁持有      LogicHandler 和 Transfer 消费流程
关键字段    Rdb
主要方法    Deliver、trackPendingAck
失败路径    Pub/Sub 无订阅者时清理匹配的旧 route 并写 offline 标记
```

把它抽出来是为了让单聊 Logic 与群聊 Transfer 共用同一套 pending、route 和 Pub/Sub 规则；代价是 Redis 成为两条实时投递链路的共同强依赖。

## 不要背生成代码

`.pb.go` 和 goctl 生成 Handler 只需要知道入口。把精力放在：

- `api/protocol.proto`
- `internal/logic/handler.go`
- `internal/delivery/redis.go`
- `internal/server/`
- `cmd/transfer/main.go`
- `internal/logic/redpacket.go`
- `internal/ai/`

## 本章代码阅读任务

这次不再按知识点读，而是为六条链路建立可现场定位的代码地图。

| 顺序 | 打开位置 | 这次必须记录什么 |
| --- | --- | --- |
| 1 | `api/protocol.proto` 的 `WireMessage`、`PushMsgReq`、`Logic` service | 写出消息 10 个关键字段，标记哪些来自客户端、哪些由服务端生成；生成的 `.pb.go` 只确认类型存在 |
| 2 | `cmd/gateway/internal/svc/servicecontext.go` 的 `ServiceContext`、`logicrouter.go` 的 `LogicRouterPool` | 写出谁在 Gateway 启动时创建它们、DB/Redis/Manager/LogicRouter 等字段由谁使用 |
| 3 | `internal/server/manager.go` 的 `ClientConn`、`ClientManager` 与 `internal/server/pool.go` 的 `PushWorkerPool` | 为三个结构各做一张“谁创建、谁持有、关键字段、两个方法、失败路径”卡 |
| 4 | `cmd/logic/internal/svc/servicecontext.go` 的 `ServiceContext` 与 `internal/logic/handler.go` 的 `LogicHandler` | 写出 Core 怎样获得 DB/Rdb/Delivery/GroupDispatcher/BotResponder，再沿 `PushMessage` 走一遍 |
| 5 | `internal/delivery/redis.go` 的 `RedisDelivery`、`cmd/logic/internal/svc/kafka_dispatcher.go` 与 `cmd/transfer/main.go` 的 `groupDispatchJob` | 对比单聊直接 Deliver 与群聊 job 后逐收件人 Deliver，确认 job 两端 JSON 字段一致 |
| 6 | `internal/logic/redpacket.go` 的 `RedPacketService` 与 `internal/ai/ask_service.go` 的 `AskService` | 各写出依赖字段、两个入口方法和一个当前无法自动恢复的失败路径 |
| 7 | 关闭文档，使用练习中的四条 `rg` 命令 | 任意选择登录、单聊、群聊、红包或 AI，十分钟内从外部入口画到存储与失败出口 |

看到这个程度才算完成：十二个结构体都能说出所有权，六条链路任意抽一条都能在十分钟内定位真实函数，不需要背行号。暂时不必阅读任何生成文件实现、第三方库源码和未出现在主链路的辅助函数。

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

## 动手练习与闭卷检查参考答案

### 动手练习答案

`rg "PushMessage"` 应把 Gateway gRPC client、Logic gRPC server、核心 `LogicHandler.PushMessage` 和 Bot 的第二次调用连起来；`rg "Deliver\("` 应看到单聊核心与 Transfer 都复用 `RedisDelivery.Deliver`；`rg "pending_ack"` 应回到 Key 定义、投递登记、重试/回放/ACK 清理；`rg "FetchMessage|CommitMessages"` 应在 Transfer 看到先取记录、处理或耐久转存、最后提交。画图必须来自这些调用点，不要求抄出所有搜索结果。

### 闭卷检查答案

1. 浏览器 HTTP 先进入 Gateway，Gateway 再通过 gRPC 进入独立 Logic 进程。
2. `*websocket.Conn` 包在当前 Gateway 的 `ClientConn` 中，并由本机 `ClientManager` 以 uid 持有；Redis 只保存 routeValue。
3. Logic 完成身份、权限、幂等和 seq 后，先 `saveMessage` 写 MySQL，再调用 RedisDelivery；群聊也先写 MySQL，再写 Kafka job。
4. Logic 在发送时从 MySQL `group_members` 查询 active 成员、排除发送者，作为 recipients 快照写进 Kafka；Transfer 不重新查群成员。
5. ACK 的 Lua 清理 `pending_ack`、`offline_msg`、`ack_idx`、`ack_retry`，并另用 payload 推进 Redis read seq。它表示客户端程序收到并处理到消息，不是用户肉眼阅读。
6. `triggerBotResponse` 只启动 Logic 进程内 goroutine，没有持久任务、claim 或重试状态；进程退出时未完成任务随内存消失。

下一步：[20 面试讲述与逐层追问](20_INTERVIEW_PREP.md)
