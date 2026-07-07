# SQL

`sql/` 目录存放数据库初始化脚本，负责把项目需要的基础表结构和测试数据准备好。

## 当前脚本

- `init.sql`：创建 `users` 和 `messages` 表，并插入测试账号。
- `20260518_conversations.sql`：旧库补齐会话列表相关字段和表。
- `20260611_v1_message_idempotency.sql`：旧库补齐 `client_msg_id` 和发送幂等唯一索引。
- `20260611_contacts_groups.sql`：旧库补齐好友申请、好友关系、群组和群成员表。
- `20260703_red_packets.sql`：旧库补齐红包主表和领取记录表。
- `20260705_ai_summary.sql`：旧库补齐 AI 群聊总结记录表。
- `20260707_ai_call_logs.sql`：旧库补齐 AI provider 调用审计日志表。

## 关键表设计

- `users`：登录账号与用户 ID 映射。
- `messages`：保存 `message_id / client_msg_id / conversation_id / session_id / seq / from_uid / to_id / to_type / create_time`，支撑历史消息查询、会话顺序展示和发送幂等。
- `friend_requests / friend_relations`：保存好友申请和双向好友关系，支撑单聊权限校验。
- `im_groups / group_members`：保存群组和群成员关系，支撑群聊权限校验和扩散成员来源。
- `red_packets / red_packet_claims`：保存红包主状态和领取记录，使用红包主行锁和 `red_packet_id + user_id` 唯一索引防止并发超卖与重复领取。
- `ai_summary_records`：保存 AI 总结、待办、风险、触发人和覆盖消息 seq 范围，支撑群聊总结审计与回放。
- `ai_call_logs`：保存 provider、调用耗时、输入消息数、状态和失败原因，支撑 AI 调用审计和性能优化。
