# 04 群聊、Kafka 与 Transfer

## 1. 为什么群聊要异步

如果 Logic 同步遍历 1000 个群成员并逐个投递：

- 请求耗时随成员数增长。
- 一个慢成员拖慢整个请求。
- Logic 被扩散工作占满，影响单聊和登录。
- 失败恢复困难。

所以链路拆成：

```text
Logic 校验和落库
-> Kafka 写群聊任务
-> 快速结束核心处理
-> Transfer 异步消费
-> 按成员投递
```

## 2. Kafka 在项目中的角色

- 解耦生产与消费。
- 削峰：短时大量群消息先进入日志。
- 可重放：消费者通过位点知道处理进度。
- 横向扩展：同 consumer group 可增加 Transfer 实例。

Kafka 不负责：最终聊天历史、WebSocket 连接、永久解决所有重复问题。

## 3. 核心消费流程

```text
FetchMessage
-> 解码任务
-> 查询群成员
-> 对每个收件人申请 lease 幂等
-> 投递
-> 失败收件人写 retry topic
-> 超过策略或不可恢复写 DLQ
-> 所有处理结果已经耐久化
-> CommitMessages
```

## 4. 为什么不用 ReadMessage 自动提交

自动提交可能在业务真正处理完成前推进位点。如果 Transfer 此时崩溃，Kafka 认为消息已经消费，但成员尚未投递。

项目改用 FetchMessage + CommitMessages：业务投递或 retry/DLQ 写成功后才提交位点。

## 5. 经典宕机题

### 投递完成但提交位点前崩溃

重启后 Kafka 会再次投递同一任务。成员级幂等发现已经 done，跳过重复副作用，然后重新提交位点。

### 部分成员成功、部分失败

成功成员标记 done；失败成员写 retry。原消息只有在失败信息也已耐久保存后才提交。

### retry topic 写失败

不能提交原位点，否则失败成员任务会丢失。消费者保留原消息等待再次处理。

## 6. 成员级 lease 幂等

状态：

```text
absent
-> processing(owner, lease_until)
-> done
```

- absent：从未处理。
- processing：某个 worker 正在处理，并带租约期限。
- done：完成，重复消费直接跳过。
- worker 崩溃后 lease 过期，其他 worker 可以接管。

为什么不能只用 SETNX 永久锁：持锁 worker 崩溃后任务可能永远无法恢复。

为什么幂等粒度是 message + recipient：同一群消息给不同成员的结果可以不同，不能只标记整条群消息完成。

## 7. retry 和 DLQ

retry topic：暂时性失败，稍后再尝试。

DLQ（死信队列）：达到重试边界或数据不可处理，需要人工排查和补偿。

DLQ 不是垃圾桶。必须保留原任务、错误原因、尝试次数等信息，才能回放。

## 8. 分区和顺序

Kafka 只保证单分区内顺序。项目使用稳定 key/Hash 让相关任务落到确定分区，但群聊最终展示仍以会话 seq 为准。

不能说 Kafka 保证全局严格顺序。

## 9. Consumer Group

同一个 consumer group 内，一个分区同一时间只由一个消费者实例处理。增加 Transfer 实例可以并行消费不同分区，但实例数超过分区数后多余实例不会获得分区。

## 10. 监控指标

当前代码关注：

- fetch/read error。
- decode/marshal error。
- handle error。
- retry/DLQ write success/error。
- commit success/error。

Kafka lag 需要 Kafka exporter 或管理工具。当前仓库没有部署 exporter，因此不能声称已有完整 lag 监控。

## 11. 面试标准回答

### 为什么选 Kafka？

> 群聊 fanout 是高吞吐、可异步、允许重试的任务，Kafka 能把它从 Logic 主链路解耦，并通过分区、consumer group 和位点提供并行消费及恢复能力。代价是最终一致、重复消费和运维复杂度，所以我又补了手动提交、成员级幂等、retry 和 DLQ。

### Kafka 能保证消息只消费一次吗？

> 业务上不能只依赖 Kafka 声称 exactly-once。项目按至少一次处理设计，重复消费通过 message+recipient 幂等状态消除重复投递副作用。

## 12. 代码锚点

- `cmd/logic/internal/svc/kafka_dispatcher.go`
- `cmd/transfer/main.go`
- `internal/delivery/redis.go`
- `internal/metrics/metrics.go`

## 13. 闭卷题

1. 为什么群聊不用同步 for 循环？
2. Kafka 在项目中解决哪三个问题？
3. FetchMessage 和自动提交的差异？
4. 投递成功但提交前宕机如何恢复？
5. 为什么需要成员级幂等？
6. lease 比永久 SETNX 好在哪里？
7. retry 和 DLQ 的区别？
8. consumer 数超过分区数会怎样？
9. Kafka 是否保证全局顺序？
10. 当前为什么不能声称已有 lag 告警？
