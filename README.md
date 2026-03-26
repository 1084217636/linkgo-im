# LinkGo-IM

LinkGo-IM 是一个基于 `Go + Go-Zero` 的分布式即时通讯系统，当前版本已经补齐到更接近简历描述的工程形态：`go-zero REST + zRPC` 脚手架、`WebSocket + gRPC` 分层、`Etcd` 服务发现、`Redis` 在线状态中心、`Lua` 会话序列号、`Kafka` 群聊异步分发、`Protobuf` 二进制消息协议、`JWT + 令牌桶限流`、`MySQL` 历史消息持久化。

说明：
当前项目已经把 `gateway` 和 `logic` 迁移到 `go-zero` 官方脚手架目录：`config / handler / logic / svc / types` 与 `zrpc` 入口；`transfer` 保持独立 Kafka 消费进程，负责异步扩散与重试死信。

## 项目亮点

- Gateway 和 Logic 解耦，接入层专注长连接，逻辑层专注消息编排。
- Gateway 使用 go-zero REST scaffold，Logic 使用 go-zero zRPC scaffold，简历里的 go-zero 表述可以落地。
- Logic 实例注册到 Etcd，Gateway 基于服务发现和 Rendezvous Hash 选择目标节点。
- WebSocket 消息载荷改为 Protobuf 二进制帧，不再依赖业务 JSON 文本。
- Redis `route:<uid>` 维护在线状态，结合网关定向 `Pub/Sub` 完成跨节点精准投递。
- Redis Lua 脚本为每个会话分配单调递增 `seq`，保证会话内顺序一致性。
- `pending_ack + ack_idx` 维护待确认消息，弱网断线后支持重放未 ACK 消息。
- 群聊消息先写入 Kafka，由 `transfer` 服务异步消费扩散，降低 Logic 同步扇出压力。
- `transfer` 对 Kafka 消费失败链路做重试和死信处理，避免异步链路静默丢消息。
- 网关集成 JWT 鉴权与令牌桶限流，避免恶意握手和暴力登录。
- 系统暴露 Prometheus `/metrics` 指标，可观测连接数、消息吞吐、ACK、Kafka 重试等状态。

## 架构分层

- `gateway`
  - 处理登录接口、JWT 校验、WebSocket 握手、心跳、ACK、离线消息回放
  - 从 Etcd 发现 Logic 节点，并按用户维度做一致性路由
- `logic`
  - 校验消息、补全 `session_id / seq / message_id / sent_at`
  - 单聊直接投递，群聊写入 Kafka 进行异步扩散
  - 持久化 MySQL，提供历史消息查询
- `transfer`
  - 消费 Kafka 群聊任务
  - 把群聊消息再投递到 Redis 在线链路和 ACK/离线链路
  - 失败任务重试，超过阈值进入死信主题
- `redis`
  - 在线路由、Pub/Sub、未确认消息、离线补偿、群组成员缓存
- `mysql`
  - 用户信息、消息历史
- `etcd`
  - Logic 服务注册与发现
- `kafka`
  - 群聊削峰填谷

## 核心链路

1. 用户调用 `/api/v1/login` 获取 JWT。
2. 客户端通过 `/ws?token=...` 建立 WebSocket。
3. Gateway 校验 JWT 后把 `route:<uid>` 写入 Redis，并同步 `pending_ack:<uid>`。
4. 客户端发送 Protobuf `WireMessage` 二进制帧到 Gateway。
5. Gateway 根据用户 ID 经 Etcd 发现 Logic 节点，通过 gRPC 转发消息。
6. Logic 用 Lua 分配会话 `seq`，补齐 `message_id / session_id / sent_at`。
7. 单聊消息直接投递；群聊消息写入 Kafka，由 `transfer` 异步消费扩散。
8. RedisDelivery 同时写入 `pending_ack`，在线用户按目标 `gatewayID` 定向走 Pub/Sub，离线用户写入 `offline_msg`。
9. 客户端收到消息后回传 ACK，服务端删除待确认消息。
10. 如果 `transfer` 消费后的投递失败，任务进入 retry topic；多次失败后进入 dead-letter topic。
11. Gateway / Transfer 暴露 Prometheus 指标，便于观测连接数、消息量和异常情况。
12. Logic 异步落库 MySQL，历史消息按 `session_id + seq` 查询。

## 协议说明

WebSocket 和 gRPC 上行消息共用 `api.WireMessage`：

```proto
message WireMessage {
  string message_id = 1;
  string session_id = 2;
  int64 seq = 3;
  string from = 4;
  string to = 5;
  string to_type = 6;
  MsgType msg_type = 7;
  string body = 8;
  int64 sent_at = 9;
  string ack_message_id = 10;
}
```

其中：

- 普通消息使用 `msg_type = NORMAL`
- 心跳使用 `msg_type = HEARTBEAT`
- ACK 使用 `msg_type = ACK`

## 目录结构

```text
.
├── api/                # protobuf 协议和 gRPC 生成代码
├── benchmark/          # 压测脚本和报告
├── cmd/                # gateway / logic / transfer 入口
├── docs/               # 简历、面试、教学材料
├── internal/           # 业务核心实现
├── pkg/                # 通用工具
├── public/             # 调试页面
├── sql/                # 初始化 SQL
├── docker-compose.yml
└── docker-compose.10node.yml
```

所有目录都已经补了独立 README。

## 快速启动

```bash
docker-compose up --build
```

默认组件：

- Gateway A: `http://127.0.0.1:8090`
- Gateway B: `http://127.0.0.1:8091`
- Gateway C: `http://127.0.0.1:8092`
- Etcd: `127.0.0.1:2379`
- Kafka: `127.0.0.1:9092`
- Redis: `127.0.0.1:6379`
- MySQL: `127.0.0.1:3306`
- Transfer Metrics: `127.0.0.1:9102/metrics`

测试账号：

- `userA / 123456` -> `1001`
- `userB / 123456` -> `1002`
- `userC / 123456` -> `1003`

## 当前和简历要求的对应关系

已完成：

- gRPC + Etcd 服务发现
- Redis 在线状态中心 + 跨节点精准路由
- Protobuf 二进制消息协议
- Lua 会话 Sequence ID
- ACK 未确认消息补偿
- Kafka 群聊异步扩散
- Kafka 重试与死信
- JWT 鉴权
- 令牌桶限流
- 双向心跳
- Prometheus 指标暴露

仍可继续增强：

- 更严格的客户端重传、去重和幂等策略
- Etcd watch 本地缓存，减少每次发现都查询 Etcd 的开销

## 可直接写进简历的版本

基于 Go 构建分布式即时通讯系统，采用 Gateway + Logic + Transfer 分层架构，使用 WebSocket 承载长连接、gRPC 解耦内部调用；基于 Etcd 做服务注册发现并结合一致性哈希实现跨节点精准路由，基于 Protobuf 二进制协议优化消息传输；利用 Redis Lua 生成会话级 Sequence ID，结合 ACK 未确认消息补偿解决弱网乱序与漏发；引入 Kafka 异步扩散群聊消息，降低同步扇出和核心链路写压；网关层接入 JWT 鉴权、令牌桶限流和双向心跳，支撑高并发长连接场景。
