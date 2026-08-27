# 04 跨 Gateway 单聊与消息可靠性

## 1. A 和 B 在不同 Gateway，单聊消息怎样传递？

### 面试官问

A 连在服务器 1，B 连在服务器 3，Logic 又是一个集群，请完整讲一条消息。

### 30 秒回答

A 通过 WebSocket 把 Protobuf 消息发到 Gateway-1。Gateway 按 A 的 uid 把任务放入有界分片队列，再通过 Etcd 发现的 gRPC client 调用任意健康 Logic。Logic 使用认证 uid 校验发送者和好友权限，处理 `client_msg_id` 幂等，分配会话 `seq`，把消息和会话摘要 Outbox 写入 MySQL。随后它先在 Redis 登记 B 的 pending，查询 `route:B`，向 Gateway-3 专属频道发布完整 payload。Gateway-3 校验 routeValue，找到本机 B 的连接并写 WebSocket，B 收到后按 `message_id` 返回 ACK。

### 详细回答

入口消息带 `client_msg_id`、目标用户和正文。Gateway 解码以后，不直接相信消息里的 `from`，而是把 JWT 已认证 uid 单独传给 Logic。队列把网络读循环和慢业务处理隔开。

Logic 为单聊构造稳定的 `c2c` 会话 ID，检查双方当前是否允许单聊。它先处理幂等，再用 Redis Lua 为该会话分配序号，生成服务端 `message_id`，同步插入 MySQL。只有消息事实存在以后，才进入在线投递。

RedisDelivery 先写 `pending_ack:B`、`ack_idx:B` 和 `ack_retry:B`。然后读取 `route:B=gateway-3|connection-x`，把 payload 包进 `PushEnvelope`，发布到 `im_message_push:gateway-3`。

Gateway-3 订阅自己的频道。它检查 envelope 的 routeValue 是否对应自己的 Gateway 和当前连接 ID，再 Base64 解码 Protobuf payload，通过带写锁和 deadline 的 `WriteBinary` 写给 B。B 页面解码、展示并回 ACK，Gateway 才清理 pending。

### 结合 LinkGo

主路径依次落在 `internal/server/client.go`、`internal/server/pool.go`、`internal/logic/handler.go`、`internal/delivery/redis.go` 和 `internal/server/manager.go`。同一 Gateway 的 A、B 当前也走这条 Redis 统一路径，没有本地直推快路径。

### 为什么这样选，替代方案是什么

Gateway-1 不能访问 Gateway-3 的 Go map。全量广播给每个 Gateway 简单但浪费资源。共享 route 加定向 Pub/Sub 能快速找到目标实例。当前单聊不经过 Kafka，因为它只需要定向投递一个接收者，增加 Kafka 会延长路径并扩大依赖面。

### 边界与追问

Redis `PUBLISH` 不是持久队列。真正历史在 MySQL，短期补偿依赖 pending 和 ACK。当前没有多设备独立投递状态，也不能承诺商业级零丢失。

## 2. 四种 ID 各自解决什么问题？

### 面试官问

`client_msg_id`、`message_id`、`session_id` 和 `seq` 为什么不能只留一个？

### 30 秒回答

`client_msg_id` 由发送客户端生成，用于同一次发送重试幂等。`message_id` 由服务端生成，用于接收方去重和 ACK。`session_id` 标识消息属于哪个单聊或群聊。`seq` 是该会话内单调递增序号，用于排序和发现缺口。四者对应发送请求、服务端消息、会话归属和会话顺序，语义不同。

### 详细回答

客户端在不知道服务端是否已经处理成功时，只能用自己提前生成的 ID 重试。Logic 使用 `(from_uid, client_msg_id)` 找到同一发送请求。若 Redis 幂等 Key 过期，MySQL 唯一索引仍能阻止重复业务消息。

服务端接受消息后生成 `message_id`。接收方收到重复投递时可以按它只渲染一次，ACK 也明确确认这条服务端消息。

`session_id` 把双方单聊或一个群的消息归到同一条时间线。`seq` 由 Redis Lua 对会话计数。网络可能乱序或重试，客户端可以通过 seq 排序并发现缺口。

### 结合 LinkGo

当前 `message_id` 大致由 `session_id + seq` 组成。Redis Lua 分配 seq，MySQL 在 `(from_uid, client_msg_id)` 上有唯一约束。重试恢复旧记录时，Logic 使用 MySQL 中的目标和正文，不信任客户端用同一 ID 改写的新内容。

### 为什么这样选，替代方案是什么

只用 UUID 可以唯一标识消息，却不能自然表达同会话顺序。只用 seq 在不同会话会冲突，也不能让客户端在发送前拥有重试键。Snowflake 一类全局 ID 可用于 message_id，但仍不能替代 client_msg_id 和会话 seq。

### 边界与追问

单调递增不等于连续无空洞。Redis 分配 seq 后，MySQL INSERT 可能失败，数字会跳过。系统应依赖顺序大小和缺口检测，不能假设每个 seq 都必有消息。

## 3. 怎样保证同一会话消息有序？

### 面试官问

你说同一个用户 FIFO，但 A、B 同时发送怎么办？

### 30 秒回答

Gateway 的 uid 分片队列只保证同一发送用户在单进程入口的处理顺序。真正的会话总顺序由 Logic 对 `session_id` 使用 Redis Lua 原子递增生成 `seq`。A、B 同时发送时，谁先成功执行会话 INCR 谁获得更小 seq，客户端最终按 seq 排序。网络到达顺序不作为最终顺序。

### 详细回答

有序要先定义范围。TCP 只保证一条连接上的字节顺序，不保证两个 Gateway 的并发请求顺序。Gateway 的 worker pool 解决同一发送者因为多 worker 并发造成的重排，但 A 和 B 在不同连接上仍会并发。

Logic 使用会话级 Redis Key 做原子递增。Lua 还可以在 Key 丢失时结合 MySQL 最大 seq 继续初始化。每条持久消息都带 seq，历史查询按 seq 排序，重连也用 `last_seq` 找缺口。

### 结合 LinkGo

`pushShardIndex(uid)` 使用 FNV 哈希选择 shard。同一 uid 进入同一 channel，由单 worker 消费。会话序号则在核心 Logic 的 Redis Lua 路径生成，MySQL `messages` 保存它。

### 为什么这样选，替代方案是什么

可以让每个会话固定到一个 Kafka partition，通过分区顺序处理，但单聊也要引入 Kafka。可以在 MySQL 行锁中更新会话序号，事务更统一，但热点会话会增加数据库锁竞争。Redis INCR 轻量，适合当前设计，代价是 Redis 与 MySQL 之间没有事务。

### 边界与追问

当前路由不是按 session 固定到某个 Logic，顺序依赖共享 Redis，而不是某个 Logic 内存。若 Redis seq 已增加但落库失败，会出现空洞，这是允许的边界。

## 4. ACK 确认的到底是什么？

### 面试官问

WebSocket 写成功，为什么还需要 ACK？ACK 等于用户已读吗？

### 30 秒回答

WebSocket 写成功只证明服务端把数据交给本机连接，不证明页面完成解码。LinkGo 的 ACK 由客户端处理普通消息后发送，确认的是“这个客户端程序处理到该 `message_id`”。它不是用户肉眼已读，也不是单独的已读回执。服务端收到 ACK 后才清理 Redis pending，并推进确认进度。

### 详细回答

一条消息有多个成功点：MySQL 提交、Redis 发布、Gateway 写 socket、客户端处理、用户阅读。每一层不能代替下一层。

Logic 投递前记录 pending。客户端返回的 ACK 帧携带 `ack_message_id`。Gateway 从连接绑定身份得到 uid，不允许客户端随意替其他用户 ACK。服务端 Lua 一次清理 pending、offline、payload index 和 retry 计数，并根据 payload 中的会话 seq 推进确认状态。

如果客户端收到了消息但 ACK 丢失，服务端会重推同一个 `message_id`。因此可靠补偿天然可能产生重复，客户端必须去重。

### 结合 LinkGo

网页当前用内存 `renderedMessageIds` 避免同一页面重复渲染。Gateway 的 `StartClientLoopWithDB` 在 `MsgType_ACK` 分支调用 `AckMessageWithDB`。默认 ACK 超时 5 秒、最多重试 3 次、扫描间隔 1 秒，这些是演示参数。

### 为什么这样选，替代方案是什么

只依赖 TCP 确认无法知道应用是否处理。可以进一步拆成 delivered 和 read 两种回执，更接近商业 IM，但会增加协议、每设备游标和数据库状态。当前项目只实现应用收到确认，重点是讲清可靠性层次。

### 边界与追问

网页刷新后内存去重集合会消失，当前不是持久化客户端去重。ACK 状态也不是多设备独立游标，不能描述成微信级已读体系。

## 5. 用户离线再上线，消息怎样恢复？

### 面试官问

用户很多时，难道把所有离线消息一直放 Redis？

### 30 秒回答

不会把永久历史只放 Redis。消息正文先写 MySQL，Redis 只保存约 7 天的 pending、payload 和会话 timeline，用于快速补偿。重连时先回放该用户未 ACK 的 pending；如果客户端带一个 `session_id + last_seq`，再补这个会话的近期缺口，Redis 不足时按 `seq > last_seq` 从 MySQL 分批回源。当前不会自动遍历用户全部会话，也没有多设备持久游标。

### 详细回答

离线时如果没有 `route:<uid>`，RedisDelivery 保留 pending 和 payload，并把 message_id 放进 `offline_msg:<uid>`。`offline_msg` 是标记，不是正文来源。永久正文已经在 MySQL。

重新建连后，`SyncOfflineMessagesWithDB` 先遍历 `pending_ack:<uid>`，从 `ack_idx:<uid>` 取完整编码 payload 并写 WebSocket。随后对于客户端明确提交且已授权的一个会话，查询 `session_timeline:<session_id>` 中大于 last_seq 的消息。Redis 缺口不足时可以从 MySQL 按 seq 向后补。

第一次回放写失败就停止并关闭连接。尚未 ACK 的状态仍在，下次重连继续尝试。继续对失效 socket 写没有意义，还会产生假成功日志。

### 结合 LinkGo

连接 URL 和心跳都可以携带一个 session id 和 last seq。服务端先校验 c2c 参与者或 active 群成员，防止用户修改 session id 越权读取共享 timeline。历史 HTTP 接口还支持 `before_seq` 向前翻页，这是用户主动看历史，不等于重连自动扫描。

### 为什么这样选，替代方案是什么

Redis 快，适合短期在线补偿；MySQL 容量和持久性更适合长期历史。完整方案可以为每个用户设备持久化每会话游标，登录后批量返回缺口，再从 MySQL 分页同步。当前没有继续做，是因为设备模型、游标提交和全会话扫描会显著扩大项目范围。

### 边界与追问

登录返回的 `last_seq` 是会话最新序号，不是某设备可靠 ACK 游标。准确口径是短期、可能重复的可恢复投递原型，不是上线后自动完整同步所有历史。

## 6. MySQL 成功但 Redis 失败，会发生什么？

### 面试官问

数据库落库后进程崩了，发送方又看到失败，会不会重复消息？

### 30 秒回答

这是跨存储故障窗口。消息可能已经在 MySQL，但 pending 或在线发布没有完成，发送方可能收到失败或超时。客户端必须复用同一个 `client_msg_id` 重试。Logic 先查 Redis 幂等状态，Redis 丢失时再用 MySQL `(from_uid, client_msg_id)` 唯一约束和旧记录恢复，避免再插一条。当前没有 MySQL 与 Redis 的分布式事务，所以我不会说这一窗口完全自动闭环。

### 详细回答

第一次请求可能经历：MySQL 事务提交成功，随后 Redis 不可用，Logic 返回错误。发送客户端不知道服务端在哪一步失败，只能重试。若它生成新的 client_msg_id，服务端会把它当新业务消息。复用原 ID，Logic 能查询已经存在的记录。

恢复旧消息时，Logic 不采用重试请求里新带的 target 和 body，而是以数据库记录为准，并重新检查当前好友或群发送权限。这样用户被拉黑或移出群以后，不能利用旧 ID 任意重放。

消息与会话摘要 Outbox 在同一个 MySQL 事务里，Outbox worker 能补摘要更新。但接收方逐设备投递状态没有完整持久 Outbox，仍依赖 Redis pending、客户端游标和主动历史查询。

### 结合 LinkGo

Redis 幂等预占区分 processing 和 complete。MySQL 唯一索引处理 Redis Key 过期后的并发插入。队列 worker 最终向发送方返回 `MESSAGE_ACCEPTED` 或 `MESSAGE_REJECTED`，但 accepted 仍不等于接收方 ACK。

### 为什么这样选，替代方案是什么

MySQL 和 Redis 上两阶段提交成本高、可用性也会受协调者影响。常见演进是把要执行的投递事件与消息一起写本地 Outbox，再由 worker 可靠投递。当前已经把会话摘要放入 Outbox，尚未把每个接收设备投递完全建模。

### 边界与追问

不要声称严格 exactly-once。工程上通常依赖至少一次尝试加幂等效果。当前系统也不能无条件声称全链路至少一次，因为接收方持久投递状态仍有边界。

## 7. 为什么群聊经过 Kafka，单聊不经过？

### 面试官问

你说 Kafka 防止群聊堵死，完整含义是什么？

### 30 秒回答

单聊只有一个主要接收者，Logic 可以直接定向投递。群聊一条消息可能对应很多成员，如果 Logic 在同步 RPC 里逐个推送，群越大，请求耗时越长，也会占住 Logic worker。LinkGo 在校验、分配 seq 和落库后，把包含 recipients 的任务写入 Kafka，由 Transfer 独立消费和逐成员投递。这样发送入口和扇出执行解耦，Transfer 也能独立扩容。

### 详细回答

Kafka 在这里提供持久异步缓冲和消费组协调。Logic 是生产者，Transfer 是消费者。同一 consumer group 会分配分区，记录处理进度。Transfer 对每个 `message_id + recipient` 使用 `processing(owner, lease) -> done` 状态，降低重复消费造成的重复副作用。

某个成员投递失败时，Transfer 把失败任务写 retry topic；超过次数进入 DLQ。只有原任务已处理，或者失败任务已经耐久转移到 retry/DLQ 后，才提交原 Kafka 位点。

### 结合 LinkGo

Logic 在发送时同步解析群成员并把完整 recipients 放进事件。Transfer 不在消费时重新决定原始接收者集合。两用户群测试是合法的最小群聊，它验证生产、消费和一个接收者投递，不证明大群性能和多个成员部分失败。

### 为什么这样选，替代方案是什么

可以用进程内队列，但 Logic 崩溃会丢任务，也不能让 Transfer 独立扩容。Redis Streams 也能持久消费，组件更少；Kafka 在消费组、分区和积压重放上更适合展示群扇出。代价是 Kafka 运维、分区规划、消费延迟和重复消费处理更复杂。

### 边界与追问

Kafka 不会让群聊自动无限扩展。吞吐受分区数、Transfer 副本、单成员 Redis 投递和下游速度限制。当前仓库没有大群生产压测证据，不能报未经验证的吞吐数字。

