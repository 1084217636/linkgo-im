# 03 单聊消息与可靠性

## 1. 单聊主链路

```text
A 发送 WireMessage(client_msg_id)
-> Gateway 读取 WS 帧
-> 按 uid 分片有界队列
-> gRPC 调 Logic.PushMessage
-> 校验发送者和目标
-> client_msg_id 幂等
-> 生成 message_id / conversation_id / seq
-> MySQL 写 messages 并更新 conversation
-> RedisDelivery 记录 payload、timeline、pending
-> 根据 route 定向通知目标 Gateway
-> Gateway 推送给 B
-> B 返回 ACK
-> 清理 pending/offline/ack index
```

## 2. 四个 ID 不要混淆

### client_msg_id

客户端生成。客户端超时重试必须复用它。用于识别“同一次发送请求”。

### message_id

服务端确认的一条消息的全局业务 ID。投递、ACK、缓存引用都围绕它。

### conversation_id / session_id

表示会话。单聊需要对双方 ID 进行稳定归一化，避免 A-B 和 B-A 变成两个会话。

### seq

会话内递增顺序号。它用于排序和缺口补偿，不等于数据库自增主键。

## 3. 双层幂等

第一层：Redis `client_msg:<uid>:<client_msg_id>` 快速识别重复上行。

第二层：MySQL 唯一约束兜底，避免 Redis 失效或并发穿透后产生重复业务消息。

标准回答：

> Redis 提供快速幂等，MySQL 唯一索引提供最终约束。只用 Redis 会受缓存丢失影响，只用数据库会增加热点写压力。

## 4. 消息顺序

项目通过 Redis Lua 为会话生成递增 seq。seq 是接收方排序和 last_seq 补偿依据。

Gateway 的推送任务按 `hash(uid) % shardCount` 进入固定 shard：

- 同一用户落入同一队列，保持 FIFO。
- 不同用户落入不同 shard，可以并发。
- 队列有界，避免内存无限增长。

注意：项目保证的是服务端处理和会话序号上的顺序基础；真实网络到达、客户端展示仍要按 seq 处理，不能笼统宣称“任何场景严格全局有序”。

## 5. 背压

当有界队列满时，Gateway 返回结构化 `SERVER_BUSY + client_msg_id`，客户端进行指数退避和随机抖动后重试。

为什么不能只写日志：客户端不知道是否需要重试，会出现请求丢失或无脑重发。

为什么要 jitter：大量客户端同时固定间隔重试会形成惊群。

## 6. pending、offline、timeline

### pending_ack

所有进入投递但尚未收到客户端 ACK 的消息引用。

### offline_msg

发送时目标不在线或实时推送失败时的额外离线索引。

### ack_idx

message_id 到消息 payload/索引信息的映射，便于重试定位。

### ack_retry

记录 ACK 超时重试次数，防止无限重试。

### session_timeline

按会话 seq 保存 message_id，用于客户端带 `last_seq` 重连时补齐缺口。

## 7. ACK 语义

> 当前 ACK 表示接收方客户端确认收到消息，不表示用户阅读。服务端写 WebSocket 成功后不能立刻删除 pending，必须等客户端 ACK。

### ACK 丢失

消息可能已经到客户端，但 ACK 丢了。服务端会重发，因此客户端也需要按 message_id 幂等展示。

这是典型 at-least-once 投递：允许重复，通过幂等消除重复影响。

## 8. 重连补偿

客户端重连后：

1. 恢复 Redis route。
2. 回放 pending ACK 消息。
3. 如果携带 session_id 和 last_seq，再从 timeline 补 `seq > last_seq`。
4. MySQL 历史接口作为最终回源。

为什么 pending 和 timeline 都要有：pending 面向未确认消息，timeline 面向会话序号缺口，两者解决的问题不同。

## 9. “不丢消息”应该怎么说

错误：系统保证绝不丢消息。

正确：

> 项目通过 MySQL 最终历史、Redis pending/offline/timeline、客户端 ACK 和幂等重试降低弱网及断线漏收风险，提供可恢复的至少一次投递链路。极端情况下仍需结合持久化、备份和跨机房方案，不能承诺数学意义零丢失。

## 10. 重点故障题

### Redis 写 pending 成功，实时通知失败

pending 保留，并写 offline；用户重连后补偿。

### 推送成功，ACK 前 Gateway 崩溃

pending 尚未清理；客户端重连后可能收到重复消息，用 message_id 幂等。

### Logic 落库成功，返回前超时

客户端复用 client_msg_id 重试，Redis/数据库幂等返回既有结果，而不是再写一条。

### 队列满

返回 SERVER_BUSY，客户端退避重试；Prometheus 记录 queue_full 指标并可告警。

## 11. 代码锚点

- `cmd/gateway/internal/handler/websockethandler.go`
- `internal/server/pool.go`
- `internal/server/ack.go`
- `internal/server/sync.go`
- `internal/delivery/redis.go`
- `internal/logic/handler.go`
- `internal/logic/conversation.go`

## 12. 闭卷题

1. client_msg_id 和 message_id 有什么区别？
2. 为什么需要 Redis 和 MySQL 双层幂等？
3. seq 解决什么问题？
4. UID 分片队列如何兼顾顺序和并发？
5. 为什么队列必须有界？
6. ACK 丢失会怎样？
7. pending 和 offline 有何区别？
8. last_seq 如何补偿？
9. 为什么这是 at-least-once？
10. 面试中如何准确表述“不丢消息”？
