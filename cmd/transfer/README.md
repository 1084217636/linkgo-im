# Transfer

`cmd/transfer` 当前已经落成实际服务，负责 Kafka 群聊任务消费与异步扩散。

## 当前职责

- 订阅 Kafka `group_message_dispatch` 主题。
- 订阅 `retry` 主题处理失败任务重试。
- 超过阈值后写入死信主题。
- 消费 Logic 写入的群聊投递任务。
- 调用统一的 RedisDelivery 把群聊消息投递到在线链路和待 ACK 链路。
- 暴露 `/metrics` 指标，观察 Kafka 消费与失败状态。

## 后续可继续增强

- 死信队列和失败重试。
- 分片消费和更细粒度的群聊扩散策略。
