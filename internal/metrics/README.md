# Metrics

`internal/metrics` 提供统一的 Prometheus 指标定义。

## 当前指标

- `linkgo_ws_connections`
- `linkgo_inbound_messages_total`
- `linkgo_outbound_messages_total`
- `linkgo_ack_operations_total`
- `linkgo_kafka_operations_total`
- `linkgo_rate_limit_hits_total`

## 作用

它让这个项目不只是“实现功能”，还能回答下面这些工程问题：

- 现在在线连接有多少？
- 上行消息有没有堆积或解码失败？
- ACK 是否正常回收？
- Kafka 是否出现重试或死信？
- 有没有接口被频繁限流？
