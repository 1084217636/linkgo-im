# Server

`internal/server` 封装接入层运行时能力，重点是连接管理与消息分发辅助逻辑。

## 当前模块

- `manager.go`：维护本节点在线连接，并订阅 Redis `im_message_push`。
  当前按 `im_message_push:<gatewayID>` 定向订阅，避免全网广播式消息扩散。
- `client.go`：消费单个 WebSocket 连接上的 Protobuf 二进制消息、心跳、ACK 和 gRPC 转发。
- `pool.go`：按 uid 做 FNV 固定分片；每个 shard 使用独立有界队列和单 worker，避免 goroutine 无限制膨胀并保持同一 uid 的提交 FIFO。
- `sync.go`：用户重连后按顺序回放未 ACK 消息。

## 关键设计

- 连接池使用 `sync.Map` 管理，降低高并发读写锁竞争。
- Pub/Sub envelope 中携带 Base64 编码后的 Protobuf 载荷，避免 Redis 传输二进制时出现歧义。
- 利用 `route:<uid> -> gatewayID` 做定向发布，和简历中的“在线状态中心精准路由”保持一致。
- `SubmitResult` 明确区分 `accepted / queue_full / pool_closed / context_canceled`；关闭时停止接收并在调用方给定的超时内排空队列。
- 接入层通过 Prometheus 观察每个 shard 的队列深度、提交结果和处理时延，以及连接数、ACK 和上行消息状态。
- 队列拒绝会返回带原始 `client_msg_id`、`trace_id` 的结构化 `SYSTEM` 帧。`queue_full` 映射为可重试 `SERVER_BUSY`，池关闭映射为可重试 `SERVER_UNAVAILABLE`，请求取消不可重试；拒绝帧不是 ACK，也不会让客户端误判为服务端已接收。
- `public/index.html` 对可重试错误执行带随机抖动的指数退避，复用原 `client_msg_id`，单条消息最多自动重试 5 次。
