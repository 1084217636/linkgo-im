# SQL

`sql/` 目录存放数据库初始化脚本，负责把项目需要的基础表结构和测试数据准备好。

## 当前脚本

- `init.sql`：创建 `users` 和 `messages` 表，并插入测试账号。

## 关键表设计

- `users`：登录账号与用户 ID 映射。
- `messages`：保存 `message_id / session_id / seq / from_uid / to_id / to_type / create_time`，支撑历史消息查询和会话顺序展示。
