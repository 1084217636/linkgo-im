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
7. 回放 WebSocket 写入失败时为什么要立即停止并断开连接？

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

消息写入 MySQL 时，当前 logic 还会在同一个 InnoDB 事务写入
`conversation_outbox`。它只负责“会话列表摘要最终一致”，不代表接收方已经 ACK；摘要处理失败最多重试 10 次，之后进入 `dead`，由运维检查 `last_error` 后修复或回放。

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

为什么从 `gateway_users` 开始扫描？每台 Gateway 只重推自己当前负责用户的本机 WebSocket，避免所有节点重复扫描和重复写连接。这种方案简单，但每秒都要先读本 Gateway 用户集合，再逐用户查询 pending，成本会随在线用户数增长；5 秒超时、3 次重试和 1 秒扫描只是演示默认值，不是经过生产 SLO 校准的结论。规模继续上升时可评估全局到期 ZSet、时间轮或延迟队列，把“逐用户轮询”改成“只取已经到期的任务”。

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

两个回放阶段都会把 `WriteBinary` 错误返回给 WebSocket Handler。第一次写失败就停止后续回放，Handler 随后关闭并清理连接。如果忽略错误继续循环，日志看起来像“补发完成”，实际后续消息也没有送达，还会对一条无效 socket 重复写入。

`WriteBinary` 默认还有 5 秒写超时，防止慢或失效客户端让回放长时间占用连接写锁。中断回放不会删除尚未 ACK 的 pending，客户端下次重连仍可再次尝试，因此客户端必须继续按 `message_id` 去重。

为什么 timeline 按会话共享，而不给每个用户再复制一份消息正文？共享的 `seq → message_id → payload` 可以减少按收件人复制短期消息体的成本，并能按会话 seq 补缺口。代价是这个 Key 不再天然属于某一个 uid：客户端提交的 `session_id` 必须先做资源授权，不能只验 JWT。

当前代码在初次 WebSocket Upgrade 前和心跳要求 timeline 补拉前都会检查：`c2c` 只允许会话两端，`group` 要求 MySQL 中仍是 `active` 成员；畸形 ID、非成员或数据库错误都失败关闭。不携带 `session_id` 的普通建连仍可以回放属于该 uid 的 pending。群权限查询增加了一次 MySQL 访问；未来若加缓存，必须同时处理退群后的快速失效。

### 当前自动恢复的边界

当前 `SyncOfflineMessages`：

- 不会遍历用户的所有会话；
- Redis timeline 不足或不可用时，会按 `seq > last_seq` 从 MySQL 分批回源；
- 只针对 URL/心跳提供的单个会话，不会扫描用户所有会话；
- 只补 URL 或心跳里提供的一个会话；
- 会先授权这个客户端提交的会话，不允许随意换 ID 读 timeline；
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

- 默认返回 50 条、最大 100 条，支持 `before_seq` 游标；
- 使用 `before_seq` 向前翻页，默认返回 50 条、最大 100 条，并返回 `next_before_seq/has_more`；
- 群历史会校验当前用户仍是 active 群成员；
- 它由页面切换会话时主动调用，不是 WebSocket 重连函数自动调用。

因此准确设计图是：

```text
Redis pending/timeline    负责约 7 天内的自动快速补偿
MySQL reconnect fallback  负责单会话缺口的长期回源
MySQL history API         负责手动读取永久历史 cursor 页面
```

未来要实现完整恢复，才应增加“登录返回每个会话的客户端游标 + 按 seq 分页回源 MySQL”。

## 9. 四种 ID 各自解决什么问题

### `client_msg_id`

客户端在发送前生成。同一发送者重试时必须保持不变。

Logic 先用 Redis `SETNX` 预占，再用 MySQL 唯一索引 `(from_uid, client_msg_id)` 兜底，降低重复入库风险。

这里要区分三种状态：

- Redis 中的值是 `pending`：前一个同 ID 请求还在处理，Logic 返回 `ErrClientMessageInFlight`。它不会把处理中误报为已完成。
- Redis 中是已完成记录：Logic 幂等短路返回，不再投递，所以不会为这次请求新产生副作用。
- Redis Key 已丢失/过期，但 MySQL 按发送者和 `client_msg_id` 找到旧消息：Logic 不信任客户端重试时新填的目标或正文，而是加载数据库标准记录，并对该记录的当前好友/群发送权限重新校验，通过后才恢复投递。

还有一个并发窗口：前置查询未看到记录，但 `INSERT` 时另一请求已经用唯一索引写入。`saveMessage` 会用 MySQL 中的胜出记录覆盖当前 frame；投递前必须对这个被替换后的真实 frame 再校验一次，不能沿用客户端原 frame 的权限结果。

为什么从 MySQL 找回旧消息后不能跳过验权直接重投？否则发送者被拉黑、移出群或禁言后，仍可以利用旧 ID 触发过期消息。代价是权限撤销后，已落库但未投递的旧消息也会被拒绝恢复。当前消息行没有持久的接收方逐设备投递状态，因此仍选择 fail-closed；会话摘要则由事务 Outbox 恢复，而不是让客户端任意触发旧消息重投。

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

这里必须把“队列受理”和“服务端成功”分开：

- 内部 `accepted` 只表示放入 Gateway 内存 shard，入队当下还没有最终结果。
- `queue_full`、`pool_closed`、`context_canceled` 会在入队失败时变成带原 `client_msg_id` 和 `trace_id` 的结构化 `SYSTEM` 拒绝帧。
- 任务已入队后，Worker 等 Logic 调用结束，再通过原 WebSocket 返回 `MESSAGE_ACCEPTED` 或 `MESSAGE_REJECTED`。`ErrClientMessageInFlight` 会成为可重试拒绝，网页复用同一个 `client_msg_id`。

`MESSAGE_ACCEPTED` 表示 Logic 正常完成本次处理或确认同 ID 的已完成请求，不表示接收方已 ACK。当前结果帧也没有返回最终 `message_id/seq`，网页仍会用历史查询核对服务端消息记录。

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
- 回放的第一次 WebSocket 写失败会立即返回并关闭无效连接。
- 队列满时向客户端返回可关联的过载错误。
- 消息事务内写入 `conversation_outbox`，worker 对会话摘要失败进行重试，毒事件超过 10 次进入 `dead`。

### 尚未实现

- 自动从 MySQL 按 seq 恢复所有会话。
- 带最终 `message_id/seq` 的发送方持久化回执；当前只有按 `client_msg_id` 关联的 Logic accepted/rejected 结果。
- 多设备独立 ACK/游标。
- 客户端持久化去重和完善的自动重连。
- 跨机房容灾与数学意义的零丢失。

仍需主动说明的故障窗口是：

```text
MySQL 消息和摘要 Outbox 已提交
→ Outbox worker 尚未处理，或 Redis pending/timeline 已过期
```

现在摘要事件不会因为 Logic 在消息提交后立刻崩溃而永久丢失；worker 会按状态重试。接收方投递仍受 Redis、Gateway、客户端 ACK 和重连游标影响，不能把当前系统描述成全链路无条件“至少一次”或零丢失。

## 12. 面试时怎样准确回答

可以这样说：

> 我的项目不把 Redis Pub/Sub 当可靠消息队列。Logic 先把消息和会话摘要 Outbox 写进同一个 MySQL 事务，再在 Redis 记录接收方 pending，并根据在线路由定向发布到目标 Gateway。客户端收到后按 message_id 返回 ACK，Gateway 才清理 pending；ACK 超时会有限重试。用户离线重连时，当前代码先回放 Redis pending，再按一个 session_id 和 last_seq 从 Redis timeline 补近期缺口，Redis 不足时按 `seq > last_seq` 回源 MySQL 分批补偿。Outbox worker 只保证摘要事件可恢复，不等于接收方已经送达；seq 也允许空洞。因此我会说它是边界清楚的可靠性原型，不会夸大成全链路零丢失或完整生产级至少一次。

## 本章代码阅读任务

| 顺序 | 打开位置 | 这次只看什么 |
| --- | --- | --- |
| 1 | `internal/delivery/redis.go` 的 `trackPendingScript`、`RedisDelivery.Deliver` | 按执行顺序写出 pending 三个 Key、GET route、PUBLISH、offline，确认 pending 在通知前 |
| 2 | `public/index.html` 的普通消息处理与 `ackMessage()`，再看 `internal/server/client.go` 的 ACK 分支 | 确认浏览器解码后发 `ack_message_id`，Gateway 用已认证 uid 调 `AckMessage` |
| 3 | `internal/server/ack.go` 的 `AckMessage`、`acknowledgeMessageScript`、`markReadFromAck` | 看 read seq 从 payload 取得；确认 pending、offline、ack_idx、ack_retry 的清理由一个 Lua 完成 |
| 4 | `internal/server/retry.go` 的 `StartPendingRetryLoop` | 找到 `gateway_users` 扫描、5 秒超时、重试计数、重新写 WebSocket 和超过上限后的 offline |
| 5 | `internal/server/sync.go` 的 `SyncOfflineMessages`、`SyncSessionMessagesAfterSeq`、`RememberSessionMessage` | 区分全用户 pending 回放与一个 session 的 timeline 补偿，并确认第一次写失败立即返回 |
| 6 | `internal/logic/handler.go` 的 `reserveClientMessage`、`PushMessage`、`saveMessage` | 对照 Redis pending/complete 状态、MySQL 唯一索引和数据库旧记录恢复 |
| 7 | `internal/server/client_error.go` 的 `pushProcessingDetail`、`writePushResult`，再看页面 `handleSystemMessage()` | 区分内部入队 accepted、Logic `MESSAGE_ACCEPTED/REJECTED` 和接收方 ACK |
| 8 | `cmd/gateway/internal/logic/historylogic.go` 的 `GetHistory` 与核心 `LogicHandler.GetHistory` | 确认历史 HTTP/gRPC 使用 `before_seq` cursor；重连则使用 `seq > last_seq` 的 MySQL fallback |

看到这个程度就停：你能画出正常 ACK、ACK 丢失、离线重连三张状态图，并能说出每一步保留或删除哪个 Key。暂时不必证明严格的分布式至少一次语义，也不必设计全链路 Outbox、设备游标和多会话自动恢复。

与本章直接对应的测试是：

```text
internal/server/client_test.go    入队拒绝帧与写失败
internal/server/manager_test.go   路由连接身份匹配与旧连接续期拦截
internal/server/sync_test.go      pending/timeline 回放遇到第一次写失败即停止
```

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
5. 当前重连如何组合 Redis 快速补偿和 MySQL 长期兜底？单次最多补多少条？
6. MySQL 历史接口的 cursor 语义和最大 limit 是什么？
7. `client_msg_id` 与 `message_id` 有什么区别？
8. 为什么不能对当前版本承诺“绝不丢消息”？
9. `accepted` 与发送端收到的拒绝帧分别证明什么？
10. 回放第一次写失败后为什么不继续写后续消息？

十个问题能不看文档讲清楚后，再进入第 12 章。

## 动手练习与闭卷检查参考答案

### 动手练习答案

1. 在线正常 ACK：Logic 在 PUBLISH 前创建 pending/ack_idx/ack_retry；B 收到并发 ACK；`AckMessage` 原子删除 pending、offline、payload 索引和 retry 计数。
2. B 已收到但 ACK 丢失：pending 保留；超时循环用同一 message_id/payload 重推；B 可能收到重复并再次 ACK，客户端按 message_id 去重，服务端收到 ACK 后清理。
3. B 离线：投递先留下 pending，再写 `offline_msg:B`；重连时先从 pending + ack_idx 回放，B ACK 后清理；若还带一个授权 session 和 last_seq，再从 timeline 补近期缺口。

Redis 整体数据丢失后，MySQL 仍有消息正文、message_id、client_msg_id、session_id 和 seq，也有关系数据。当前不会自动恢复 pending/retry/offline、在线 route、timeline payload 和 Redis 快速游标；带单会话 cursor 的 WebSocket 重连可以回源 MySQL，但不会自动扫描用户所有会话，也不会恢复 Redis Pub/Sub 中断期间的实时通知。

### 闭卷检查答案

1. PUBLISH 只报告 Redis 订阅者数量，不覆盖 Gateway 解析、socket 写入、客户端处理和 ACK。
2. `pending_ack` 保存 message_id 与投递时间；`ack_idx` 保存 message_id 到完整 payload；`ack_retry` 保存重试次数。
3. 客户端可能已经处理，但 ACK 在网络中丢失，服务端会重推；客户端按稳定 `message_id` 去重。
4. 不是。它只作离线标记；重连正文先从 `pending_ack` 找 ID，再从 `ack_idx` 取 payload。
5. URL 仍然携带一个会话 cursor，服务端先回放 pending 和 Redis timeline，再从 MySQL 按 `seq > last_seq` 分批兜底，保护上限为 1000 条；仍然不会一次同步用户所有会话。
6. 历史向前翻页使用 `seq < before_seq`，默认 50、最大 100，查询多一条判断 `has_more`；重连向后补偿使用 `seq > last_seq`，两者都是 cursor，不使用深 OFFSET。
7. `client_msg_id` 由发送端生成并在重试时复用，约束一次逻辑发送；`message_id` 由服务端生成，用于持久记录、接收去重和 ACK。
8. Outbox 能保证消息提交后的摘要事件可恢复，但 Redis 短期状态会过期或丢失，客户端也没有持久设备游标，因此接收方全链路仍有补偿窗口。
9. 内部入队 `accepted` 只证明任务进入 Gateway 内存；入队拒绝帧证明任务未进入队列，并告诉客户端是否可重试。后续 `MESSAGE_ACCEPTED/REJECTED` 才表示 Logic 调用结果，仍不表示 B 已 ACK。
10. socket 已经失效，继续循环只会制造更多失败并可能假装完成。立即返回会触发连接清理，未 ACK 的状态仍保留，客户端下次重连可再次尝试。

下一步：[12 Kafka 与群聊扩散](12_GROUP_CHAT_AND_KAFKA.md)

## 面试拷打：cursor 与 seq 空洞

当前 Redis `INCR` 先于 MySQL INSERT，INSERT 失败会留下 seq 空洞；并发消息也可能出现 seq=102 先提交、seq=101 后提交。因此系统只能承诺 seq 单调分配，不能承诺严格提交有序。分页和补偿必须查询真实存在的行，不能等待 101，也不能重新编号历史消息。`acked_seq` 只代表客户端确认收到，不代表用户阅读，更不能用 `last_seq - acked_seq` 冒充精确未读数。
