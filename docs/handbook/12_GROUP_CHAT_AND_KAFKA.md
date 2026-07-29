# 12 Kafka 与群聊扩散：一条群消息怎样送给很多人

## 本章前置

你已经理解单聊完整链路、Redis 在线路由、pending ACK 和客户端重连补偿。

本章第一次引入 Kafka。开始前只需要知道：A 发一条群消息，系统需要把它分别投递给多个群成员。

## 本章目标

学完后，你必须能回答：

1. 群聊为什么不能简单复制单聊代码？
2. Kafka 在 LinkGo 中保存的是什么，而不是什么？
3. Logic 和 Transfer 分别完成哪一半工作？
4. 为什么消费成功后才提交 offset？
5. retry topic、DLQ 和收件人幂等分别解决什么问题？
6. 当前实现面对真正超大群还有哪些瓶颈？

## 1. 群聊首先是一个“扇出”问题

单聊通常只有一个接收者：

```text
A 的一条消息 → B
```

群聊有多个接收者：

```text
A 的一条消息 → B、C、D、E……
```

把一个输入变成多个输出，叫 fan-out，中文常说扇出或扩散。

如果一个群有 5,000 人，而 Logic 在一次 gRPC 请求中依次向 4,999 人推送，那么发送请求会被最慢的成员拖住，Logic 的工作时间和群人数一起增长。

因此 LinkGo 把“接收并保存群消息”和“逐个投递群成员”拆开。

## 2. Kafka 先用快递分拣中心理解

Kafka 可以先理解为一个可持久保留、按顺序追加的事件日志系统。

生产者把任务写进去：

```text
Logic → Kafka
```

消费者之后读取并处理：

```text
Kafka → Transfer
```

这叫异步：Logic 不需要在同一个函数里完成所有成员的 WebSocket 推送。

但异步不等于自动正确。我们仍要处理重复消费、失败重试、顺序和积压。

本章先使用两个最小业务概念：`active 群成员` 表示用户当前仍在群中；`recipients` 是这条群消息要投递到的收件人 ID 列表。好友申请、群角色和成员表会在第 13 章系统学习，这里只关注它们怎样影响 Kafka 任务。

## 3. LinkGo 的群聊完整路径

假设 A 连接 Gateway-1，并向群 G100 发送消息：

```text
A 浏览器
→ Gateway-1 WebSocket
→ Gateway-1 的发送队列
→ gRPC 调 Logic.PushMessage
→ Logic 校验 A 是 active 群成员且未处于禁言期
→ Redis Lua 分配 group:G100 的 seq
→ MySQL messages 只写一条群消息
→ MySQL 查询当前 active 群成员，排除 A
→ 把消息和 recipients 列表写入 Kafka
→ Logic 返回本次内部调用

Kafka
→ Transfer 消费任务
→ 逐个 recipient 调 RedisDelivery.Deliver
→ 查询每个用户的 route
→ 定向通知其所在 Gateway，或保留离线 pending
→ 客户端收到后 ACK
```

群消息正文在 MySQL 只保存一行，而不是给每个成员复制一行正文。

## 4. Kafka 任务里实际有什么

Logic 生产的 JSON 任务结构是：

```go
type groupDispatchJob struct {
    Frame      *api.WireMessage
    Recipients []string
    Attempt    int
}
```

- `Frame`：已经有 `message_id/session_id/seq` 的群消息。
- `Recipients`：发送时从 MySQL 查出的接收者快照。
- `Attempt`：这份任务已经重试多少次。

Kafka message 的 key 使用 `frame.SessionId`，即群会话 ID。Kafka Hash Balancer 会让相同 key 通常进入同一 partition，从而保留同一 partition 内的写入顺序基础。

Producer 当前使用：

```text
RequiredAcks = RequireOne
```

它表示 leader broker 确认后写入返回，不等于所有副本都已确认。个人本地 Compose 甚至只有一个 Kafka broker。

## 5. Logic 到底解耦了什么

当前 Logic 已经把最重的“逐成员在线路由查询、pending 记录和 WebSocket 投递”交给 Transfer。

但要诚实看到：Logic 仍然会同步执行：

```text
SELECT active group_members
→ 把完整 recipients 数组放进 Kafka 消息
```

所以当前发送成本仍会受到群成员数量影响，只是从 N 次网络投递降成一次成员查询和一次较大的 Kafka 写入。

准确口径是：

> Kafka 解耦了逐成员投递，而不是让 Logic 的群聊发送成本完全与群规模无关。

## 6. Transfer 为什么使用 consumer group

多个 Transfer 使用同一个：

```text
KAFKA_CONSUMER_GROUP=transfer-group
```

Kafka 会把 partition 分给组内消费者。同一条 Kafka 记录在一个 consumer group 中只由一个消费者实例负责。

增加 Transfer 实例能否增加吞吐，还取决于 topic 的 partition 数：

- 只有 1 个 partition 时，同一个 group 通常只有 1 个活跃消费者处理主 topic。
- 有多个 partition 时，多个 Transfer 才能并行处理不同 partition。
- 同一群使用同一个 key，通常仍集中到一个 partition，以换取该群的顺序基础。

仓库配置没有负责生产环境 topic 分区规划，不能只说“加 Transfer 就一定线性扩容”。

## 7. 为什么使用 FetchMessage 和手动提交

Kafka 的 offset 可以理解为消费者读到的位置。

如果读取一条记录后立刻提交 offset，再开始投递，可能发生：

```text
offset 已提交
→ Transfer 崩溃
→ 成员还没收到
→ 重启后从更后面继续
```

这条任务就被跳过了。

当前 Transfer 使用：

```text
FetchMessage
→ 处理投递，或者成功写入 retry/DLQ
→ CommitMessages
```

只有下列结果之一完成后，才提交原消息：

1. 所有 recipients 已成功处理或被幂等状态跳过；
2. 失败任务已经成功写入 retry topic；
3. 无法解析或达到上限的任务已经成功写入 DLQ。

如果 retry/DLQ 写入也失败，当前记录不提交，消费循环每 250ms 退避后继续尝试。

## 8. 为什么会重复消费

另一个故障窗口是：

```text
Transfer 已经投递完成
→ 还没提交 offset 就崩溃
→ 重启后再次 Fetch 同一条记录
```

Kafka 的常见消费语义允许这种重复。因此业务不能只依赖 offset，还需要收件人级幂等。

## 9. 收件人级 lease 幂等

LinkGo 为每个“消息 + 接收者”生成 Redis key：

```text
group_delivery:<message_id>:<recipient>
```

状态机是：

```text
不存在
→ processing:<owner>，带 1 分钟 lease
→ done，保留 7 天
```

这里：

- `owner` 是当前处理者生成的唯一标识；
- `lease` 是临时租约，避免一个 Worker 崩溃后永远卡在 processing；
- `done` 表示该收件人已经完成投递处理，重复任务可以跳过。

三个 Lua 脚本分别负责 claim、complete 和 release：

- key 不存在：当前 owner 获得处理权；
- 另一个 owner 正在处理：返回 busy；
- 已是 done：直接跳过；
- 当前 owner 投递失败：只有它能删除自己的 processing；
- owner 崩溃：lease 到期后其他 Worker 可以重新 claim。

这个状态解决的是“不要因为 Kafka 重复记录而无控制地重复处理同一收件人”。Transfer 自己不写 WebSocket：它调用 `RedisDelivery` 登记 pending，并完成定向 Pub/Sub 或离线标记。若 `RedisDelivery` 已返回、但写 `done` 前进程崩溃，租约到期后会再次执行投递处理，所以最终客户端仍应按 `message_id` 去重。`done` 只代表这个收件人的服务端投递处理完成，不代表目标 Gateway 已写成功，更不代表客户端收到或 ACK。

## 10. retry topic 怎样工作

只要某一个 recipient 的普通投递返回错误，Transfer 就停止本轮后续成员处理，把整个 job 的 `Attempt` 加一，并写入：

```text
group_message_retry
```

retry 消费者再次遍历完整 recipients 列表：

- 已经 `done` 的成员快速跳过；
- 失败成员重新处理；
- 之前尚未处理到的后续成员继续处理。

配置中的 `maxAttempts=3` 表示最多把失败任务写入 retry topic 3 次；连同最初主 topic 的一次处理，最多可经历 4 轮失败，第 4 轮才进入：

```text
group_message_dlq
```

DLQ 是 dead-letter queue，死信队列。它保存自动流程无法继续处理的记录，等待人工排查或离线补偿。

当前 retry topic 没有实现分钟级或指数级延迟队列；它是另一条立即消费的 topic。

有一个特殊分支：如果收件人的 lease 正被另一个 Worker 持有，Transfer 不会把它当普通投递失败写入 retry topic，而是保持原 Kafka 记录未提交，并在当前消费循环中退避后重试，等待 lease 完成或过期。

## 11. 逐成员投递仍复用单聊机制

Transfer 并不直接持有 WebSocket。它为每个 recipient 调用同一个 `RedisDelivery`：

```text
记录 pending
→ 查询 route
→ Redis Pub/Sub 通知目标 Gateway
→ 目标 Gateway 写 WebSocket
```

所以群聊与单聊共享：

- 在线路由；
- 跨 Gateway 通知；
- pending ACK；
- ACK 超时重试；
- Redis 重连回放。

Kafka 只替换了“怎样安排多接收者投递任务”，没有替换最后一跳。

## 12. 顺序应该怎样准确描述

项目具有几层顺序基础：

1. Logic 用 Redis Lua 为群会话分配单调 `seq`；
2. Kafka 使用相同 `session_id` 作为 key；
3. 同一 Kafka partition 内记录有顺序；
4. 客户端和历史记录可以按 `seq` 排序。

但不能承诺所有成员实时到达绝对有序：

- 某条消息进入 retry 后，下一条主 topic 消息可能先投递；
- Redis Pub/Sub、网络重连和 ACK 重试都可能造成迟到或重复；
- 当前网页没有实现完整的乱序缓冲窗口，只会显示实际到达或历史查询顺序。

因此标准说法是：

> 服务端为会话生成可比较的 seq，并尽量保留 Kafka 分区顺序；实时网络到达仍可能乱序，客户端需要按 seq 检测缺口、排序和补偿。

## 13. 典型故障推演

### Logic 写 MySQL 成功，但写 Kafka 失败

当前 `PushMessage` 返回内部错误并释放 Redis 的 `client_msg_id` 预占，但 Gateway 后台 Worker 只记录错误，没有正式发送失败确认帧。

如果客户端再次使用相同 `client_msg_id` 发送，Logic 会从 MySQL 找回原消息并再次尝试 Kafka 分发。

当前没有 MySQL Outbox 自动恢复这个故障窗口。

### Kafka 写成功，Logic 随后崩溃

Transfer 仍可能消费并投递消息，因为任务已在 Kafka。可是 Logic 后面的会话缓存/元信息异步更新可能尚未完成，这些更新与消息写库不是同一事务。

### Transfer 投递一半后崩溃

offset 未提交，消息会重新消费。已经完成的 recipient 由 `done` 跳过，其余成员继续处理。

### Redis 不可用

Transfer 无法 claim recipient，也无法记录 pending 或查在线路由，因此不提交当前 Kafka 记录，恢复后继续。积压会留在 Kafka。

### retry 和 DLQ 都不可用

原 Kafka offset 不提交，当前循环持续退避重试，不假装任务成功。

## 14. 当前群聊实现的设计缺口

### 已实现

- 群成员发送权限校验。
- 群消息 MySQL 单行持久化。
- 群会话 seq。
- Kafka 异步逐成员投递。
- 手动提交 offset。
- retry 和 DLQ。
- 收件人 processing/lease/done 幂等状态。
- Transfer 可运行多个副本并使用 consumer group。

### 尚未实现或尚未验证充分

- MySQL 消息事务与 Kafka 事件之间的 Outbox。
- 超大群成员分片、分页或分批任务；当前 recipients 全部装入一个 job。
- 生产 topic 的 partition、副本和容量规划。
- 有延迟策略的 retry topic。
- 群成员变更与发送时快照的完整语义；当前任务携带发送时 recipients，之后退群的人仍可能收到重试消息。
- 客户端完整乱序缓冲与缺口自动回源。
- 真实多 broker Kafka 故障演练和大群端到端压测证据。

## 15. 面试时怎样准确回答

可以这样说：

> 单聊只有一个接收者，群聊会产生 fan-out。我的 Logic 先校验群成员、分配会话 seq，并在 MySQL 只保存一行群消息；然后查询当前 active 成员，把 WireMessage 和 recipients 快照写入 Kafka。Transfer 使用 consumer group 异步逐成员调用与单聊相同的 RedisDelivery。消费采用 FetchMessage 加手动 Commit，只有全部处理成功，或者 retry/DLQ 写成功后才提交。为处理投递完成但 offset 未提交导致的重复消费，我用 message_id + recipient 的 Redis Lua 状态实现 processing owner、lease 和 done。当前仍会在 Logic 同步加载完整成员列表，也缺少 MySQL Outbox 和超大群分片，所以我会说它完成了可靠异步扩散原型，而不是已经解决百万群生产问题。

## 代码锚点

按顺序阅读：

1. `internal/logic/handler.go`：`resolveRecipients` 和 `deliverPersistedMessage`。
2. `cmd/logic/internal/svc/kafka_dispatcher.go`：Kafka job 与 producer 写入。
3. `cmd/transfer/main.go`：Fetch、处理、retry/DLQ、Commit 顺序。
4. `cmd/transfer/recipient_lease.go`：收件人状态机和 Lua。
5. `internal/delivery/redis.go`：Transfer 最后怎样复用单聊投递。
6. `cmd/transfer/main_test.go`：提交顺序和 lease 测试实际覆盖什么。

## 动手练习

假设 G100 除发送者外有 B、C、D：

1. B 投递成功，C 失败，D 尚未处理。
2. job 进入 retry。
3. retry 时 B、C、D 分别会发生什么？
4. retry 投递 C 后、标记 done 前进程崩溃，会有什么重复风险？

画出每个 recipient 的 Redis 状态变化。

## 闭卷检查

1. fan-out 是什么？
2. 当前 Kafka job 包含哪些字段？
3. Logic 是否已经完全摆脱 O(N) 群成员成本？
4. 为什么要先处理、后提交 offset？
5. retry topic 与 DLQ 有什么区别？
6. processing lease 为什么不能只写一个永久 processing？
7. 增加 Transfer 副本为什么不一定增加吞吐？
8. 当前能否承诺所有成员实时严格按 seq 到达？
9. MySQL 写成功但 Kafka 写失败怎样恢复？当前缺少什么？

九个问题能闭卷讲清楚后，再进入第 13 章。

下一步：[13 好友、群组与会话](13_RELATIONSHIPS_AND_CONVERSATIONS.md)
