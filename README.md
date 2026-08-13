# LinkGo Chat

LinkGo 是一个使用 Go 和 go-zero 实现的 `IM + 红包 + AI` 秋招项目。主线是多 Gateway 场景下的 WebSocket 连接、跨节点单聊、消息持久化、ACK/重连补偿和 Kafka 群聊投递；红包用于演示数据库并发一致性，AI 作为虚拟好友接入普通消息链路。

## 第一次看这个项目

如果你只会 Go 基本语法，请不要从架构图或面试题开始。

学习本项目时只看 [LinkGo 从零学习手册](docs/handbook/README.md)，从第 00 章开始按编号顺序读到第 21 章。`docs/handbook/` 是唯一学习主线；其他 docs 目录分别用于查代码、执行操作、查看证据或提供程序运行时语料。

完整文档分类见 [docs/README.md](docs/README.md)。

## 当前客户端

`public/index.html` 是零构建的原生 HTML/CSS/JavaScript 调试客户端，不依赖 React、Vue 或 npm。它通过 HTTP 登录和查询业务，通过 Protobuf WebSocket 二进制帧收发实时消息，支持：

- 登录、好友、群组和会话列表。
- 单聊、群聊、历史查询。
- 心跳、ACK、`last_seq` 短期补偿和过载退避重试。
- 红包创建、领取、详情查询。
- AI 虚拟好友入口。

它用于验证后端链路，不定位为商业级前端；Token 只保存在页面内存，刷新后需要重新登录，断线自动重连和浏览器 E2E 仍不完整。

## 架构

```text
Client A -> LB -> Gateway-1 -> Etcd/p2c_ewma -> Logic-2
                         |                         |-> MySQL
                         |                         |-> Redis
Client B -> LB -> Gateway-3 <- Redis Pub/Sub <-----'

群聊：Logic -> Kafka -> Transfer -> Redis -> target Gateway
```

### Gateway

- 提供 HTTP 和 `/ws` WebSocket 入口。
- 校验 JWT、Origin，维护本机连接、心跳、ACK 和 UID 分片有界推送队列。
- `/healthz` 只表示进程存活，`/readyz` 检查 Logic gRPC、Redis、MySQL；Logic 的 gRPC Health Check 还会检查 Redis、MySQL、Kafka，避免依赖故障时继续接收新流量。
- 通过 Etcd 发现 Logic 实例，使用 go-zero zRPC `p2c_ewma`。
- 当前好友、群、红包和部分 AI HTTP 接口也在 Gateway 进程直接访问 MySQL，因此不能笼统描述成“Gateway 完全不碰数据库”。

### Logic

- 处理登录、历史查询和 WebSocket 上行消息。
- 生成 `message_id`、稳定会话 ID 和会话 `seq`。
- 使用 Redis 与 MySQL 双层幂等，同步写入消息历史后再投递。
- 单聊进入 RedisDelivery；群聊先解析收件人并写 Kafka 任务。

### Transfer

- 消费已经携带收件人列表的 Kafka 群聊任务。
- 逐收件人写入投递状态并通知目标 Gateway。
- 使用 `processing(owner, lease) -> done` 做成员级幂等，并处理 retry、DLQ 和手动位点提交。

## 数据组件

- MySQL：消息最终历史、用户/好友/群/会话关系、红包和 AI 记录。
- Redis：在线路由、会话 seq、短期 pending/ACK payload/timeline 和热点索引。
- Kafka：群聊逐成员投递任务、重试和死信。
- Etcd：Logic 服务注册与发现。

Redis Pub/Sub 只负责在线实时通知，不是可靠消息存储。当前 WebSocket 重连自动回放 Redis `pending_ack + ack_idx`，并可按一个 `session_id + last_seq` 从 Redis timeline 补最多 200 条；MySQL 历史通过独立 HTTP 接口查询，当前不会在重连时自动扫描所有会话完整补齐。

## 快速验证

### 只运行代码检查

```bash
make test
make build
make frontend-static-check
make compose-config
make k8s-check
make fault-check
```

### 启动本地完整环境

```bash
make docker-up
```

启动网页：

```bash
python3 -m http.server 8088 --directory public
```

打开 <http://127.0.0.1:8088>。测试账号：

```text
userA / 123456 -> 1001
userB / 123456 -> 1002
userC / 123456 -> 1003
```

本地默认端口：

```text
Gateway A/B/C  8090/8091/8092
Logic          9001
Redis          6379
MySQL          3306
Kafka          9092
Etcd           2379
```

详细操作见 [本地演示手册](docs/runbooks/LOCAL_DEMO.md) 和 [DevOps 参考](docs/runbooks/DEVOPS.md)。
故障停止、替换、扩容和恢复演练见 [故障先行手册](docs/runbooks/FAULT_HANDLING.md)。
面试准备见 [LinkGo IM 45 分钟逐字讲稿](docs/interview/LINKGO_IM_30_MINUTE_TALK.md)。

## 目录

```text
api/             Protobuf 和 gRPC 协议
cmd/gateway/     HTTP/WebSocket 接入服务
cmd/logic/       核心消息 gRPC 服务
cmd/transfer/    Kafka 群聊消费者
internal/        消息、投递、AI、鉴权和指标实现
public/          浏览器调试客户端
sql/             MySQL 初始化和迁移
deploy/          Kubernetes 与监控配置
docs/handbook/   唯一学习主线
docs/reference/  代码参考
docs/runbooks/   操作手册
docs/evidence/   验证记录
docs/knowledge/  AI 问答运行时语料
```

## 当前边界

- 消息正文同步写 MySQL；会话摘要的 MySQL 更新目前是异步尽力写入，不处在同一事务。
- Gateway 的 route 和本机连接均为单值，当前不是完整的同账号多端在线模型。
- `ack_idx` 仍按接收用户保存完整编码 payload，尚未完全优化为纯消息 ID 引用。
- `offline_msg` 当前是标记索引，重连实际读取 `pending_ack`；MySQL 历史接口固定返回最近 50 条，没有游标参数。
- Logic 仍同步解析群成员后才写 Kafka，所以群消息发送成本并非完全与群规模无关。
- 红包是等额账本模型，没有钱包扣款、入账、退款、资金流水和对账。
- AI 默认使用 mock；FAQ 是轻量文本召回，AI 回复任务是非持久 goroutine，不是可靠任务队列。
- `deploy/k8s/production` 是应用工作负载的可渲染示例，没有 Ingress/TLS、前端部署或真实生产集群证据。
- 当前应用使用 Redis 稳定单入口和 MySQL 单 DSN，不原生实现 Redis Cluster/Sentinel 客户端或 MySQL 应用层读写分离。
- 完整 Compose 故障演练已验证单实例 Redis、MySQL、Kafka、Logic、Transfer 故障和 Gateway 替换；这不等于已经验证 Redis Cluster、MySQL 主从或 Kafka 多副本 HA。

以上边界是面试时必须主动说明的当前事实；演进方案不能写成已经上线。
