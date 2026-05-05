# IM 设计边界与面试说明

这份文档用于把 LinkGo-IM 从“组件列表”讲成“围绕 IM 业务问题做设计取舍”的项目。

## 核心业务问题

项目重点解决四个问题：

- 实时性：在线用户希望消息尽快到达目标 WebSocket 连接。
- 可靠性：在线推送失败、用户断线或 ACK 丢失时，消息不能直接丢。
- 顺序性：同一个单聊/群聊会话内，消息需要按递增序号展示。
- 扩展性：多个 Gateway 承载长连接，Logic 可多实例扩展，群聊扩散不能阻塞主链路。

## 服务分层

| 模块 | 职责 | 不做什么 |
| --- | --- | --- |
| Gateway | 登录入口、JWT 校验、WebSocket 长连接、心跳、ACK 接收、离线消息回放 | 不做复杂消息编排，不直接处理群聊扩散 |
| Logic | 消息校验、会话 ID/seq/message_id 生成、在线路由判断、单聊分发、历史消息查询 | 不持有 WebSocket 连接 |
| Transfer | 消费 Kafka 群聊任务，按群成员扩散，处理 retry/dead-letter | 不处理客户端连接 |
| Redis | 在线路由、Pub/Sub 实时通知、pending_ack、ack_idx、offline_msg、群成员缓存 | 不作为最终历史消息存储 |
| MySQL | 最终历史消息存储和历史查询 | 不承担在线路由和实时推送 |
| Kafka | 群聊异步扩散、削峰、失败重试和死信 | 不用于当前单聊直投链路 |

## 单聊跨 Gateway 链路

场景：用户 A 连接 Gateway-1，用户 B 连接 Gateway-7。

1. A 通过 WebSocket 把 Protobuf `WireMessage` 发给 Gateway-1。
2. Gateway-1 通过 Etcd 发现 Logic 实例，并通过 gRPC 调用 `PushMessage`。
3. Logic 校验发送方，生成 `session_id`、会话级递增 `seq`、`message_id` 和 `sent_at`。
4. Logic 把待确认消息写入 Redis `pending_ack:<uid>` 和 `ack_idx:<uid>`。
5. Logic 查询 Redis `route:B`，得到 B 当前所在的 Gateway-7。
6. Logic 通过 Redis Pub/Sub 向 `im_message_push:Gateway-7` 发布推送通知。
7. Gateway-7 收到 Pub/Sub 消息后，从本地连接表找到 B 的 WebSocket 并写入消息。
8. B 客户端收到消息后发送 `MsgType_ACK`，Gateway-7 清理 B 对应的 pending/offline/ack_idx 数据。

## ACK 边界

当前项目不要把 ACK 说成“已读确认”，更准确的说法是：

- 服务端接收确认：Gateway 调用 Logic 成功，只能说明服务端接收并进入分发流程。
- 接收方投递确认：接收方客户端收到消息后回传 `MsgType_ACK`，服务端据此清理 pending。
- 已读确认：当前未实现，不在项目能力范围内。

所以面试里可以这样说：

> 当前实现的是“接收方收到消息后的投递 ACK”，不是严格的 read_ack。服务端写 WebSocket 成功也不会立即删除 pending，只有收到客户端 ACK 才清理。

## 顺序性边界

当前保证的是会话级顺序，不保证全局顺序。

- 单聊会话 ID：`c2c:min(uid1,uid2):max(uid1,uid2)`
- 群聊会话 ID：`group:<group_id>`
- 每个会话通过 Redis Lua `INCR + PEXPIRE` 生成递增 `seq`
- 客户端可以按 `session_id + seq` 展示、去重和发现缺口
- 历史消息按 `session_id + seq` 查询和补齐

需要注意：当前 Lua 只负责生成会话级 `seq`，pending 写入发生在投递阶段，不要夸大成“seq 与 pending 完全原子写入”。

## Redis Pub/Sub 边界

Redis Pub/Sub 在本项目只承担在线用户的跨 Gateway 实时通知。

它的边界是：

- 适合在线实时推送
- 不适合作为可靠消息队列
- 订阅方断线、Gateway 重启或网络抖动时可能丢通知

项目里的补偿方式是：

- 消息先进入 `pending_ack` 和 `ack_idx`
- 接收方不在线时写入 `offline_msg`
- 在线推送失败或 ACK 未返回时，pending 不清理
- 用户重新上线后按 pending 顺序回放未确认消息，也可以通过历史消息接口按 `session_id + seq` 补齐

## Kafka 为什么用于群聊

单聊是一对一投递，主链路可以直接做在线路由和投递。

群聊是一对多扩散，如果 Logic 同步遍历大量成员并逐个推送，会拉长发送方请求耗时，也容易让主链路被大群消息拖住。因此项目使用 Kafka 把“发送消息”和“按群成员扩散”解耦：

1. Logic 生成消息并写入 Kafka 群聊任务。
2. Transfer 消费任务，遍历群成员并调用 RedisDelivery 投递。
3. 投递失败时写入 retry topic，多次失败后进入 dead-letter topic。

## 面试回答主线

可以用这句话收束：

> 框架提供的是 API/RPC 分层、服务启动、配置和日志规范；我真正设计的是多 Gateway 场景下的连接管理、跨节点消息路由、会话级 seq、ACK/pending 补偿、群聊异步扩散和压测验证。
