# LinkGo 当前消息可靠性事实

## 单聊

发送者提供 `client_msg_id`。Redis 快速幂等与 MySQL `(from_uid, client_msg_id)` 唯一约束避免重复业务消息。Logic 使用 Redis Lua 分配会话 seq，消息同步插入 MySQL后再进入 RedisDelivery。

在线接收者通过 `route:<uid>` 定位 Gateway，Redis Pub/Sub 只负责实时通知。`PUBLISH` 成功不等于客户端收到，最终由客户端 ACK 确认。

## 当前重连能力

- 重连自动读取 `pending_ack`，再从每用户 `ack_idx` 取得完整编码 payload。
- 客户端若携带一个 `session_id + last_seq`，服务端从 Redis `session_timeline` 找消息 ID，再从 `message_payload` 读取短期 payload，每次最多 200 条。
- `offline_msg` 当前作为标记写入并在 ACK 时清理，不是重连回放的数据来源。
- Redis 短期数据约保留 7 天。
- MySQL `/api/v1/history` 当前返回指定会话最近 50 条，没有游标参数；WebSocket 重连不会自动扫描用户全部会话并从 MySQL补齐。

因此准确口径是“提供短期、至少一次、可能重复的可恢复投递机制”，不能说绝对不丢或完整商业级多端同步。

## 群聊

Logic 校验发送者群成员身份、同步解析收件人、保存消息并将含 recipients 的任务写 Kafka。Transfer 使用同一 consumer group 消费，按 `message_id + recipient` 维护 `processing(owner, lease) -> done`；失败写 retry，超过次数写 DLQ，耐久后才提交原 Kafka 位点。
