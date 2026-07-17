# Transfer

`cmd/transfer` 当前已经落成实际服务，负责 Kafka 群聊任务消费与异步扩散。

## 当前职责

- 订阅 Kafka `group_message_dispatch` 主题。
- 订阅 `retry` 主题处理失败任务重试。
- 超过阈值后写入死信主题。
- 消费 Logic 写入的群聊投递任务。
- 调用统一的 RedisDelivery 把群聊消息投递到在线链路和待 ACK 链路。
- 使用 `FetchMessage + CommitMessages` 手动管理 offset；只有投递成功、retry 发布成功或 DLQ 发布成功后才提交。输出 topic 暂时不可用时保留当前消息并退避重试，避免后续 offset 越过失败消息。
- 每个 `(message_id, recipient)` 使用 Redis Lua 完成 `absent → processing(owner, lease) → done` 原子状态迁移；竞争中的任务不提交 offset，owner 崩溃后可在 lease 到期后重新领取，非 owner 不能完成或释放任务。
- 暴露 `/metrics` 指标，观察 Kafka 消费与失败状态。

## 后续可继续增强

- 分片消费和更细粒度的群聊扩散策略。
