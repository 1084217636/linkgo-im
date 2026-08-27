# 05 MySQL、Redis、Kafka 与 Etcd

## 1. 为什么 MySQL 是消息事实来源？

### 面试官问

IM 消息为什么存 MySQL？它和 Redis 的职责怎样区分？

### 30 秒回答

MySQL 保存需要长期存在并参与约束的数据，包括用户、好友、群成员、消息历史和会话摘要。它提供事务、唯一索引和持久化查询。Redis 保存在线路由、会话计数器和约 7 天的补偿状态，强调低延迟，不替代永久历史。LinkGo 的基本原则是先把消息事实写 MySQL，再做 Redis 在线投递。

### 详细回答

消息历史需要按会话和 seq 查询，也要在服务重启后存在。MySQL 的 InnoDB 事务可以把消息和会话摘要 Outbox 一起提交。唯一索引还承担业务正确性：`message_id` 唯一，`(from_uid, client_msg_id)` 防发送重试重复，`(conversation_id, seq)` 防会话位置冲突。

历史查询使用 `session_id` 和 seq 索引，按 `before_seq` 向前分页。相较 offset 分页，游标不会随着前面新增数据持续漂移，也不用扫描并丢弃大量行。

### 结合 LinkGo

`messages` 表有 `uk_message_id`、`uk_sender_client_msg`、`uk_conversation_seq` 和 `idx_session_seq`。消息事务还写 `conversation_outbox`，worker 再更新会话摘要。好友与群权限来自 MySQL 关系表，不来自 Redis 在线状态。

### 为什么这样选，替代方案是什么

MongoDB 对文档结构灵活，按会话分片和海量消息归档也有应用场景。但当前数据关系明确，需要唯一约束、事务和 SQL 查询，MySQL 足够。为了简历多一个数据库强行加 MongoDB，会增加一致性和学习成本，没有解决当前真实问题。

### 边界与追问

当前应用连接一个稳定 MySQL DSN，没有在 Go 代码里实现读写分离、分库分表或主库选举。真实大规模历史可以按会话分片、冷热分层或使用专用存储，但那是容量和访问模式达到瓶颈后的演进。

## 2. 事务和唯一索引分别保证什么？

### 面试官问

有了事务为什么还需要唯一索引？有了唯一索引又为什么需要幂等代码？

### 30 秒回答

事务保证一组数据库操作要么一起提交，要么一起回滚。唯一索引保证即使有并发请求，数据库也不会接受两个冲突事实。应用层幂等可以提前识别重复并返回合理结果，唯一索引则是最后防线。LinkGo 把消息和会话摘要 Outbox 放同一事务，用 `(from_uid, client_msg_id)` 唯一索引兜底并发重试。

### 详细回答

两个 Logic 可能同时收到同一 client_msg_id。它们都可能在前置查询时看不到记录，随后同时 INSERT。如果只有“先查再写”，会发生检查与写入之间的竞争。唯一索引由数据库在写入时裁决，只允许一个成功。

事务解决另一类问题：若消息已写入，但用于恢复会话摘要的 Outbox 没写，Logic 立刻崩溃，摘要更新可能永久丢失。把两者放一笔事务，提交后 worker 总能看到 Outbox；回滚时两者都不存在。

### 结合 LinkGo

Logic 先做 Redis 快速预占，命中处理中状态时返回可重试错误；Redis 丢失或并发窗口下，MySQL 唯一索引决定胜出记录。若 INSERT 撞唯一约束，代码加载数据库中的标准消息继续判断，而不是把重复键简单当系统故障。

### 为什么这样选，替代方案是什么

只依赖分布式锁会增加租约和锁失效问题，而且锁服务失败时仍需要数据库约束。只依赖唯一索引正确但每次重复都打到数据库。Redis 快速路径加 MySQL 最终约束兼顾性能和正确性，代价是状态机更复杂。

### 边界与追问

MySQL 事务只覆盖 MySQL，不能把 Redis 和 Kafka 自动纳入同一原子提交。跨系统要用幂等、Outbox、重试和明确的失败语义组合处理。

## 3. Redis 在 LinkGo 里承担哪些职责？

### 面试官问

不要只说缓存，请按数据结构讲 Redis。

### 30 秒回答

Redis 在项目里有四类职责。String 保存 `route:<uid>`、消息 payload 和 Gateway 活跃标记；Hash 保存 ACK payload 索引、重试次数和会话进度；ZSet 保存 pending、offline 和按 seq 排序的 timeline；Set 保存某 Gateway 负责的用户。它还用 Lua 完成条件续期、比较删除、ACK 清理和会话 seq 分配，用 Pub/Sub 做在线 Gateway 定向通知。

### 详细回答

选择数据结构要从访问方式出发。按 uid 查当前位置是单键查找，用 String。按时间或 seq 取一个范围，需要有序集合 ZSet。一个用户下按 message_id 查 payload 和 retry 次数，适合 Hash。按 Gateway 枚举当前用户且不关心顺序，适合 Set。

Lua 用于把多个相关 Redis 命令变成一次原子执行。例如旧连接续 route 时，必须先比较当前值仍等于自己的 routeValue，再同时延长相关 Key。若在客户端分两次 GET 和 EXPIRE，中间可能被新连接覆盖，旧连接仍错误续期。

### 结合 LinkGo

主要 Key 包括 `route:<uid>`、`gateway_users:<gatewayID>`、`pending_ack:<uid>`、`ack_idx:<uid>`、`ack_retry:<uid>`、`offline_msg:<uid>`、`session_timeline:<sessionID>` 和 `message_payload:<messageID>`。短期可靠性 Key 大约保留 7 天。

### 为什么这样选，替代方案是什么

这些在线状态也可以存 MySQL，但心跳续期、计数和频繁 ACK 清理会给关系数据库带来高频写。Redis 更适合短生命周期状态。代价是内存昂贵、数据可能过期或故障，业务必须有 MySQL 历史兜底并承认补偿边界。

### 边界与追问

当前应用使用单个 Redis 稳定地址，不原生实现 Sentinel 或 Cluster 客户端。多 Key Lua 在 Redis Cluster 还要考虑 Key 是否同 slot，不能把单节点脚本直接说成可无修改横向分片。

## 4. Redis Pub/Sub 为什么适合在线通知，又不适合可靠队列？

### 面试官问

微信单聊会不会也用 Redis Pub/Sub？你的做法合理吗？

### 30 秒回答

公开资料无法证明微信内部具体实现，我不会把 LinkGo 类比成微信。Redis Pub/Sub 的特点是低延迟、实现简单，适合拿到在线路由后唤醒目标 Gateway。它不保存历史，订阅者断开期间的事件不会等它恢复，所以不能单独承担可靠消息。LinkGo 用 MySQL 保存消息，用 pending 和 ACK 做短期补偿，Pub/Sub 只负责实时通知。

### 详细回答

发布者向频道发送后，Redis 把事件交给当时在线的订阅连接。`PUBLISH` 返回订阅者数量，只能说明当时有多少订阅客户端，不代表 Gateway 成功解析、写 socket 或客户端 ACK。

每个 Gateway 只订阅 `im_message_push:<gatewayID>`。Logic 查到 B 的 route 后只发目标频道，不广播给所有节点。事件中携带完整 payload，所以目标 Gateway 不必再查 MySQL就能在线写出。

### 结合 LinkGo

投递顺序是先记录 B 的 pending，再 GET route，再 PUBLISH。没有 route、订阅者为零或写 socket 失败时会标记 offline。历史和待确认状态让“通知丢失”不等于“消息事实消失”。

### 为什么这样选，替代方案是什么

可以为单聊也使用 Kafka 或 Redis Streams，获得持久消费记录，但每个在线用户的定向消费与低延迟路径更复杂。也可以使用专门的路由服务和内部 RPC 直调 Gateway。当前项目规模下 Redis 已经承担路由，用定向 Pub/Sub 能减少组件。

### 边界与追问

当前方案是可靠性原型，不是严格送达保证。大规模系统可能采用专门推送总线、分区日志或 Gateway 间 RPC，技术选择要看连接规模、延迟、消息保留和运维能力。

## 5. Kafka 的 partition、consumer group 和 offset 是什么？

### 面试官问

群聊里的 Kafka 是怎样并行和恢复的？

### 30 秒回答

Topic 是消息类别，partition 是其中有序追加的分片，offset 是记录在分区中的位置。相同 consumer group 内，一个 partition 同时主要由一个消费者负责，因此增加 Transfer 是否有效取决于分区数。LinkGo 用群 session id 做 message key，使同群消息通常进入同一 partition，换取分区内顺序基础。消费者用 `FetchMessage` 处理完成或耐久转入 retry/DLQ 后再手动提交 offset。

### 详细回答

Producer 把群任务追加到 topic。Hash balancer 根据 key 选择 partition。同一 partition 内 Kafka 保留写入顺序，但不同 partition 没有全局顺序。

Consumer group 让多个 Transfer 分担 partition。若主 topic 只有一个 partition，增加很多同组实例也只有一个主要实例在处理它。某消费者退出时，Kafka 重新分配 partition。

如果读到记录立即提交 offset，再投递成员，中途崩溃会跳过未完成消息。LinkGo 先 `FetchMessage`，完成成员处理或确保失败任务进入 retry/DLQ 后才 `CommitMessages`。如果提交前崩溃，记录会重复消费，因此还需要成员级幂等。

### 结合 LinkGo

群 job 带完整 recipients、消息和 Attempt。某成员失败后，整个 job 增加 Attempt 写 retry topic，已 done 的成员下轮快速跳过。无法解析或超过重试上限的任务进入 DLQ。

### 为什么这样选，替代方案是什么

Kafka 适合持久积压、消费组和重放。RabbitMQ 更偏灵活路由与队列语义，Redis Streams 组件较少。当前项目选择 Kafka 是为了群扩散解耦和消费恢复，代价是分区、重复消费和 broker 运维都要理解。

### 边界与追问

当前 retry 是立即消费，不是指数延迟队列。持续故障时重试可能很快耗尽。仓库也没有 DLQ 管理页面和自动修复回放工具。

## 6. Lease 是什么？它和 Kafka 幂等有什么关系？

### 面试官问

为什么不用一个永久的 `processing` 标记？

### 30 秒回答

Lease 是有期限的处理所有权。Transfer 对 `message_id + recipient` 原子 claim，写成 `processing:<owner>` 并设置一分钟过期。完成后改为 `done`，失败时只有 owner 能 release。若 Worker 崩溃，lease 到期后别的 Worker 可以重新 claim，避免任务永远卡在 processing。

### 详细回答

Kafka 可能在提交 offset 前重复交付记录。整条 job 里有多个 recipient，单靠 Kafka offset 不知道哪些成员已经完成，所以需要成员级状态。

状态机是：Key 不存在时当前 owner 获取处理权；已经 done 时直接跳过；其他 owner 的 processing 尚未过期时等待；当前 owner 成功后完成；当前 owner 失败后释放。Lua 保证 claim、complete 和 release 的比较修改原子。

Lease 处理的是 Worker 崩溃导致锁永久占用的问题。它不是客户端送达证明。RedisDelivery 返回后、写 done 前崩溃，lease 到期可能再次投递，客户端仍要按 message_id 去重。

### 结合 LinkGo

幂等 Key 形式是 `group_delivery:<message_id>:<recipient>`。processing lease 当前是一分钟，done 保留约 7 天。这些是演示默认值，不是生产调优结果。

### 为什么这样选，替代方案是什么

数据库唯一表也能保存每成员状态，持久性更强，但群成员写放大会增加 MySQL 压力。Redis 状态快且实现简单，代价是过期和故障会导致重新处理。若业务副作用不可重复，仍需要下游操作自身幂等。

### 边界与追问

Lease 时间过短会让仍在处理的 Worker 被误判失效，过长会拖慢崩溃接管。严格方案需要 heartbeat 延长 lease，当前项目的成员投递较短，没有把它扩展成通用任务平台。

## 7. Etcd 和 Redis 的关系是什么？

### 面试官问

既然已经有 Redis，为什么还要 Etcd？

### 30 秒回答

两者在项目里管理不同状态。Etcd 保存 Logic 实例注册和租约，强调一致的服务发现；Redis 保存高频业务临时状态和在线推送。可以用 Redis 自己实现注册中心，但要自行处理租约、观察、并发更新和客户端发现协议。LinkGo 使用 go-zero 已支持的 Etcd 服务发现，职责更清楚。

### 详细回答

Etcd 是一致性 Key-Value 存储，常用于配置、选举和服务发现。Logic 的注册记录随租约存活，Gateway 观察变化获得健康地址列表。Redis 虽然也有 TTL 和 Pub/Sub，但项目使用它处理用户路由、ACK 和消息通知，故障语义不同。

把所有东西塞一个组件可以减少依赖数量，却会把“Logic 实例目录”和“用户在线路由”混在一起，也失去 go-zero 现成 resolver 的能力。

### 结合 LinkGo

Etcd Key 是 `services/logic`，不保存聊天正文、好友或群成员。Redis `route:<uid>` 保存的是用户连接位置，不是 Logic 服务地址。Gateway 获取 Logic 列表后，仍由 gRPC balancer 选择一次调用的实例。

### 为什么这样选，替代方案是什么

Kubernetes 原生 Service/DNS、Consul 或云服务发现都能替代 Etcd。若完全运行在 K8s 且接受 Service 层负载均衡，可以减少 Etcd。当前项目保留 Etcd 是为了展示独立服务注册和客户端负载均衡，代价是本地环境多一个组件。

### 边界与追问

当前仓库本地运行单 Etcd 容器，不能说验证了 Etcd quorum 高可用。Etcd 故障不会直接删除 MySQL 消息，但会影响新 Logic 地址发现。

## 8. 多服务器部署时 Redis 和 MySQL 应该放哪里？

### 面试官问

Gateway 和 Logic 分布在多台服务器，数据库也要每台装一份吗？

### 30 秒回答

应用实例不能各用一套互不相通的数据库，否则用户路由、消息和关系会分裂。Gateway、Logic、Transfer 应连接共享的稳定 Redis 和 MySQL 入口。入口背后可以是托管高可用、代理加主从或独立集群。应用只依赖稳定地址，故障切换由数据库平台负责。当前仓库只提供外部地址配置，没有实现这些集群本身。

### 详细回答

多应用服务器解决计算和连接容量，不等于把数据随便复制。MySQL 高可用通常还要复制、选主、代理、备份和恢复。Redis 高可用要考虑 Sentinel、Cluster 或托管服务，并处理数据持久化与分片约束。

应用侧要使用连接池、超时和健康检查。写请求必须到可写主入口，读写分离还要接受从库延迟。Redis Cluster 会影响多 Key Lua 和事务，因为相关 Key 可能不在同一 slot。

### 结合 LinkGo

production K8s overlay 把 Redis、MySQL、Kafka、Etcd 写成外部稳定地址占位符，不随 Gateway 滚动发布。Compose 中的单节点依赖只用于一台开发机复现。

### 为什么这样选，替代方案是什么

把数据库也作为普通无状态 Deployment 部署看起来方便，但 Pod 或 Node 故障会同时影响数据。真实有状态服务要用 StatefulSet、持久卷、复制协议或成熟 Operator。当前项目不自行实现，是因为这属于专门的平台运维领域，面试中应讲设计思路和边界，不伪造落地经验。

### 边界与追问

“连接稳定入口”不等于已经高可用。当前应用代码没有 MySQL 应用层读写分离，也没有 Redis Cluster 原生分片验证。生产 overlay 中的 `*.example.internal` 是占位地址，不能原样部署。

