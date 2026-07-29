# 11 ACK、重试与离线恢复：消息为什么可能重复，也可能暂时收不到

## 本章前置

你已经学过：WebSocket 长连接、Redis 在线路由、跨 Gateway 单聊，以及 `message_id`、`client_msg_id`、`session_id`、`seq` 这些字段。

本章不会假设你已经懂“可靠消息”。我们先从最普通的失败开始。

## 本章目标

学完后，你必须能回答：

1. Redis `PUBLISH` 成功为什么不等于用户已经收到？
2. ACK 在 LinkGo 中确认的到底是什么？
3. 服务端没有等到 ACK 时怎样重试？
4. 用户离线后，当前代码怎样自动回放消息？
5. MySQL 历史与 Redis 自动补偿分别负责什么？
6. 当前实现在哪些故障窗口里仍可能需要客户端主动重试？

## 1. 先区分四个不同的“成功”

假设 A 给 B 发消息，B 连接在另一台 Gateway：

```text
MySQL 写入成功
≠ Redis 发布成功
≠ B 所在 Gateway 写 WebSocket 成功
≠ B 的客户端处理成功
```

每一步只能证明自己的工作完成了。

- MySQL 成功：服务端有一条持久化历史。
- Redis `PUBLISH` 成功：当时至少有订阅者收到通知的机会。
- Gateway 写 WebSocket 成功：数据交给了本机网络连接；不代表页面已经处理。
- 客户端 ACK：页面解码消息后，主动告诉 Gateway“我收到了这条消息”。

因此，不能把 Redis Pub/Sub 当成最终送达证明。

## 2. LinkGo 中 ACK 的含义

ACK 是 acknowledgement，意思是确认。

当前协议中，普通消息和 ACK 都使用 `api.WireMessage`。ACK 帧的重要字段是：

```text
msg_type       = ACK
ack_message_id = 被确认消息的 message_id
```

网页收到普通消息后会执行：

```text
decodeWireMessage
→ 渲染或更新会话
→ ackMessage
→ WebSocket 发回 ACK
```

服务端收到 ACK 后会删除 Redis 中对应的待确认状态。

必须使用准确口径：

> 当前 ACK 表示“该客户端程序已经收到并处理到这条消息”，不是用户肉眼已经阅读，也不是业务已读回执。

项目没有单独实现“送达回执”和“已读回执”两套协议。

## 3. 为什么投递前要先记录 pending

Logic 调用 `RedisDelivery.Deliver` 时，第一步不是直接 `PUBLISH`，而是先记录待确认消息。

涉及三个 Redis 结构：

```text
pending_ack:<uid>  ZSET  message_id -> 首次/最近投递时间
ack_idx:<uid>      HASH  message_id -> Base64(Protobuf 消息体)
ack_retry:<uid>    HASH  message_id -> 已重试次数
```

当前这些结构的过期时间是 7 天。它们是短期补偿状态，不是永久历史。

写入由一段 Redis Lua 脚本完成。Lua 脚本让三类更新作为一次 Redis 原子执行完成，避免只写入一半：

```text
ZADD pending_ack
HSETNX ack_idx
HSETNX ack_retry
EXPIRE 三个 key
```

然后 Logic 才查询 `route:<uid>`，决定在线推送还是标记离线。

这样做的原因是：如果先推送、后记录 pending，中间进程崩溃，就可能既没有 ACK，也没有可回放状态。

## 4. 在线投递的真实顺序

假设 B 的路由是 `gateway-3|connection-xyz`：

```text
Logic
→ 先写 pending_ack / ack_idx / ack_retry
→ GET route:B
→ 得到 Gateway-3
→ PUBLISH im_message_push:Gateway-3
→ Gateway-3 的 Redis 订阅协程收到 PushEnvelope
→ 在本机 ClientManager 查找 B 的 WebSocket
→ WriteBinary
→ B 页面解码并回 ACK
→ Gateway-3 删除 B 的 pending、payload 索引和 retry 计数
```

Pub/Sub 中传递的 `PushEnvelope` 包含目标用户、消息 ID、会话、seq、路由版本，以及 Base64 编码后的 Protobuf 消息体。

每台 Gateway 只订阅自己的频道：

```text
im_message_push:<gateway_id>
```

它不是让所有 Gateway 都接收所有用户消息。

## 5. ACK 超时怎样重试

每个 Gateway 启动一个定时循环：

```text
StartPendingRetryLoop
```

默认参数是：

```text
ACK 超时       5 秒
最大重试次数   3 次
扫描间隔       1 秒
```

循环先从 `gateway_users:<gateway_id>` 找出当前属于本 Gateway 的用户，再检查这些用户的 `pending_ack`。

超过 ACK 时间仍未确认时：

```text
HINCRBY ack_retry
→ 从 ack_idx 取回消息体
→ 再写一次本机 WebSocket
→ 更新 pending_ack 的时间分数
```

超过最大次数后，代码会把消息 ID 写入 `offline_msg:<uid>`，并把 pending 的下一次检查时间推迟一分钟。它不会无限快速重试，因为那会在客户端故障时拖垮服务器。

### 重试为什么会产生重复消息

可能发生：

```text
B 已经收到消息
→ B 发出的 ACK 在网络中丢失
→ 服务端认为没有收到
→ 服务端再次推送同一个 message_id
```

因此客户端需要按 `message_id` 去重。网页当前使用内存中的 `renderedMessageIds` 避免同一页面重复渲染；刷新页面后这份内存会消失，所以它还不是完整商业客户端的持久去重实现。

## 6. 用户不在线时当前代码做什么

如果 `route:B` 不存在，或者 Redis 频道没有订阅者，`Deliver` 会：

```text
保留 pending_ack 和 ack_idx
→ ZADD offline_msg:B message_id
→ 本次实时投递结束
```

注意两个容易混淆的事实：

1. `offline_msg:<uid>` 只保存 `message_id`，不复制完整正文。
2. 当前重连代码实际首先读取的是 `pending_ack`；`offline_msg` 主要用于离线标记和 ACK 时清理，不是当前回放正文的直接来源。

完整正文短期保存在 `ack_idx`，永久聊天历史保存在 MySQL `messages` 表。

## 7. 重连时当前代码究竟怎样恢复

客户端重新建立 WebSocket 时可以携带：

```text
/ws?token=...&session_id=...&last_seq=...
```

Gateway 的 `SyncOfflineMessages` 当前按两个步骤执行。

### 第一步：回放该用户全部 pending

```text
ZRANGE pending_ack:<uid>
→ HGET ack_idx:<uid> message_id
→ Base64 解码
→ WriteBinary
```

这些消息再次收到 ACK 后才从 pending 中删除。

### 第二步：补一个会话的近期 seq 缺口

发送成功的消息还会保存一份共享的近期索引：

```text
session_timeline:<session_id>  ZSET  seq -> message_id
message_payload:<message_id>   STRING  Base64(Protobuf)
```

如果连接参数中有一个 `session_id + last_seq`，Gateway 查询：

```text
seq > last_seq
```

当前一次最多读取 200 条，并跳过刚刚已经从 pending 回放的相同消息。这两个结构也只保留约 7 天。

### 当前自动恢复没有做什么

当前 `SyncOfflineMessages`：

- 不会遍历用户的所有会话；
- 不会在 Redis 缺失时自动查询 MySQL；
- 不会从 MySQL 按 `after_seq` 分页；
- 只补 URL 里提供的一个会话；
- 依赖客户端正确维护并提交 `last_seq`。

当前网页的初始 `last_seq` 来自登录返回的 `conversation.last_seq`，它是会话最新序号，不是这台设备已 ACK 的可靠游标；之后只在页面内存中更新，刷新会丢失。所以现有 timeline 更接近演示性近期补偿，不能说已经实现可靠的多设备游标同步。

所以不能在面试中说“用户一上线，服务端会自动从 MySQL 把所有会话缺失消息完整推送”。那是合理的下一版设计，但不是当前代码事实。

## 8. MySQL 历史接口与自动重连不是一回事

MySQL 中的 `messages` 是最终历史来源。当前查询入口是：

```text
GET /api/v1/history?target_id=<用户或群>
```

Logic 根据当前用户和目标构造会话 ID，然后执行：

```sql
SELECT ...
FROM messages
WHERE session_id = ?
ORDER BY seq DESC
LIMIT 50;
```

查询结果会反转成从旧到新的顺序返回。

当前接口的边界：

- 只返回最近 50 条；
- 没有 `cursor`、`before_seq` 或 `after_seq` 参数；
- 群历史会校验当前用户仍是 active 群成员；
- 它由页面切换会话时主动调用，不是 WebSocket 重连函数自动调用。

因此准确设计图是：

```text
Redis pending/timeline   负责约 7 天内的自动快速补偿
MySQL history API        负责手动读取最近 50 条永久历史
```

未来要实现完整恢复，才应增加“登录返回每个会话的客户端游标 + 按 seq 分页回源 MySQL”。

## 9. 四种 ID 各自解决什么问题

### `client_msg_id`

客户端在发送前生成。同一发送者重试时必须保持不变。

Logic 先用 Redis `SETNX` 预占，再用 MySQL 唯一索引 `(from_uid, client_msg_id)` 兜底，降低重复入库风险。

### `message_id`

服务端接受消息后生成，当前形式大致是：

```text
<session_id>-<seq>
```

接收方用它 ACK 和去重。

### `seq`

Redis Lua `INCR` 为一个会话分配单调递增序号。Redis 中序列 key 丢失时，Logic 会先查询 MySQL 的最大 seq，再继续递增。

`seq` 用于发现缺口和排序，不代表网络一定按该顺序到达。

### `trace_id`

用于把 Gateway、Logic、Transfer 的日志串起来。它不是业务幂等键。

## 10. Gateway 队列满时，发送端看到什么

Gateway 不会让每个 WebSocket 读循环直接无限创建 goroutine。它按发送者 uid 哈希到固定 shard，每个 shard 有一个有界队列。

队列满时，Gateway 通过原 WebSocket 返回 `SYSTEM` 错误帧：

```json
{
  "type": "error",
  "code": "SERVER_BUSY",
  "retryable": true,
  "retry_after_ms": 250
}
```

响应会带回原 `client_msg_id`。网页最多重试 5 次，使用指数退避和随机抖动，并复用同一个幂等 ID。

这里也有边界：Gateway 将消息成功放入内部队列后，并不会给发送方返回一条正式的“服务端已持久化 ACK”。后台 gRPC 或 Logic 后续失败目前主要记录日志，网页通常通过历史查询观察最终结果。

## 11. 当前可靠性边界：必须主动说清楚

### 已实现

- MySQL 保存消息历史。
- `client_msg_id` 的 Redis 预占与 MySQL 唯一索引。
- 会话 `seq`。
- 投递前记录 pending。
- 客户端 ACK 后清理。
- ACK 超时有限重试。
- 重连回放 Redis pending。
- 单个会话的 Redis `last_seq` 近期补偿。
- 队列满时向客户端返回可关联的过载错误。

### 尚未实现

- MySQL 事务提交与投递事件之间的 Outbox。
- 自动从 MySQL 按 seq 恢复所有会话。
- 历史消息游标分页。
- 发送方“已落库”服务端确认帧。
- 多设备独立 ACK/游标。
- 客户端持久化去重和完善的自动重连。
- 跨机房容灾与数学意义的零丢失。

最重要的故障窗口是：

```text
MySQL 已提交
→ Logic 在记录/发布投递事件前崩溃
```

因为当前没有 Outbox，只有客户端再次用同一个 `client_msg_id` 重试，Logic 才会从 MySQL 找到原消息并重新投递。不能把当前系统描述成无条件“至少一次”。

## 12. 面试时怎样准确回答

可以这样说：

> 我的项目不把 Redis Pub/Sub 当可靠消息队列。Logic 先把消息写入 MySQL，并在 Redis 记录接收方 pending，再根据在线路由定向发布到目标 Gateway。客户端收到后按 message_id 返回 ACK，Gateway 才清理 pending；ACK 超时会有限重试。用户离线重连时，当前代码先回放 Redis pending，再按一个 session_id 和 last_seq 从 Redis timeline 补近期缺口。MySQL 是永久历史来源，但当前历史接口只查最近 50 条，尚未接入重连时按游标分页回源；MySQL 提交到投递之间也还缺 Outbox。因此我会说它实现了可演示的 ACK 和短期补偿机制，不会夸大成零丢失或完整生产级至少一次。

## 代码锚点

按下面顺序阅读：

1. `internal/delivery/redis.go`：投递前怎样记录 pending，怎样查 route 和 Pub/Sub。
2. `internal/server/ack.go`：ACK 怎样清理状态。
3. `internal/server/retry.go`：超时重试怎样扫描。
4. `internal/server/sync.go`：重连实际读取什么。
5. `internal/server/client_error.go`：队列拒绝帧。
6. `internal/logic/handler.go`：幂等、seq、持久化与投递的先后顺序。
7. `cmd/gateway/internal/logic/historylogic.go`：MySQL 历史入口。
8. `public/index.html`：搜索 `ackMessage`、`scheduleMessageRetry`、`last_seq`。

## 动手练习

画出三张图：

1. B 在线并正常 ACK。
2. B 收到消息但 ACK 丢失。
3. B 离线，稍后重新连接。

每张图都标出 `pending_ack` 在什么时候产生、什么时候删除。

然后分别回答：Redis 整体数据丢失后，哪些信息还能从 MySQL 找到，哪些当前不会自动恢复？

## 闭卷检查

1. `PUBLISH` 返回订阅者数量为什么不能证明 B 已收到？
2. `pending_ack`、`ack_idx`、`ack_retry` 分别保存什么？
3. ACK 丢失为什么导致重复投递？客户端靠什么去重？
4. `offline_msg` 是否是当前重连正文的直接来源？
5. 当前重连能自动补几个会话？数据来自 Redis 还是 MySQL？
6. MySQL 历史接口当前有什么分页限制？
7. `client_msg_id` 与 `message_id` 有什么区别？
8. 为什么不能对当前版本承诺“绝不丢消息”？

八个问题能不看文档讲清楚后，再进入第 12 章。

下一步：[12 Kafka 与群聊扩散](12_GROUP_CHAT_AND_KAFKA.md)
