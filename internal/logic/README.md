# Internal Logic

`internal/logic` 实现 Logic 服务的核心业务，是整个 IM 的“大脑”。

## 当前实现

- 校验并归一化客户端消息。
- 通过 Lua 脚本为会话分配单调递增 `seq`。
- 统一生成 `message_id` 和 `session_id`。
- 单聊直接投递目标用户，群聊转成 Kafka 异步扩散任务。
- 调用 RedisDelivery 维护在线投递、待 ACK 和离线补偿。
- 异步落库到 MySQL。

## 为什么这层重要

简历里的“消息顺序一致性”“跨节点投递”“Kafka 削峰”“历史记录查询”都在这一层真正实现。
