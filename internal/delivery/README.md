# Delivery

`internal/delivery` 负责统一的 Redis 消息投递链路，让 Logic 和 Transfer 可以复用同一套实现。

## 当前能力

- 将 Protobuf 二进制消息编码为 Base64 后写入 Redis。
- 维护 `pending_ack:<uid>`，跟踪待确认消息。
- 维护 `ack_idx:<uid>`，支持按 `message_id` 精确删除。
- `pending_ack/offline_msg` 的 ZSet 成员保存 `message_id`；当前 `ack_idx:<uid>` 仍按接收用户保存完整 Base64 Protobuf payload，近期热缓存和时间线另存于 `message_payload:<message_id>`、`session_timeline:<session_id>`。
- 在线用户走 Redis `im_message_push`。
- 在线用户根据 `route:<uid>` 中记录的 `gatewayID` 走定向 Redis 频道。
- 离线用户写入 `offline_msg:<uid>`。
- 对投递结果返回错误，让上层决定是否重试或进入死信。

当前重连正文实际来自 `pending_ack + ack_idx`，`offline_msg` 是离线标记并在 ACK 时清理，不是直接回放来源。

## 为什么单独拆出来

如果不拆，Logic 和 Transfer 都要重复实现在线投递、离线补偿和 ACK 跟踪，后续很难维护。
