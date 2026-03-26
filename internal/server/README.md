# Server

`internal/server` 封装接入层运行时能力，重点是连接管理与消息分发辅助逻辑。

## 当前模块

- `manager.go`：维护本节点在线连接，并订阅 Redis `im_message_push`。
  当前按 `im_message_push:<gatewayID>` 定向订阅，避免全网广播式消息扩散。
- `client.go`：消费单个 WebSocket 连接上的 Protobuf 二进制消息、心跳、ACK 和 gRPC 转发。
- `pool.go`：固定 worker pool 处理上行消息转发，避免 goroutine 无限制膨胀。
- `sync.go`：用户重连后按顺序回放未 ACK 消息。

## 关键设计

- 连接池使用 `sync.Map` 管理，降低高并发读写锁竞争。
- Pub/Sub envelope 中携带 Base64 编码后的 Protobuf 载荷，避免 Redis 传输二进制时出现歧义。
- 利用 `route:<uid> -> gatewayID` 做定向发布，和简历中的“在线状态中心精准路由”保持一致。
- 接入层可通过 Prometheus 指标观察连接数、ACK 和上行消息状态。
