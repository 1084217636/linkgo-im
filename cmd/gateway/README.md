# Gateway

`cmd/gateway` 是 IM 的接入层入口，负责把客户端连接生命周期和后端业务服务对接起来。

## 负责的能力

- 提供登录、历史消息、群组接口。
- 通过 JWT 校验 HTTP 与 WebSocket 请求。
- 建立 WebSocket 连接并把在线路由注册到 Redis。
- 从 Etcd 发现 Logic 服务，并通过 go-zero zRPC 的 `p2c_ewma` 负载均衡选择实例。
- 使用 Protobuf 二进制帧承载 WebSocket 消息。
- 订阅 Redis Pub/Sub，把跨节点消息投递给本机连接。
- 在用户连接建立后回放 Redis 中尚未 ACK 的短期消息。

## 关键点

- `route:<uid>` 使用 TTL 续约，结合心跳避免脏路由长期残留。
- WebSocket 上行消息通过 gRPC 转发到 Logic；但好友、群组、红包和部分 AI HTTP 接口当前仍在 Gateway 进程内直接访问 MySQL。
- 连接层与业务层解耦，便于横向扩容多个 Gateway 节点。

`route:<uid>` 和本机连接当前都是单值模型，不代表同账号多端同时在线。完整链路请按顺序阅读 [`docs/handbook`](../../docs/handbook/README.md)。
