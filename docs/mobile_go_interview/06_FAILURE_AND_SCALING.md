# 06 故障处理、扩容与一致性边界

## 1. Gateway 挂了会怎样？

### 面试官问

用户连接的 Gateway 进程突然退出，消息和连接怎么恢复？

### 30 秒回答

Gateway 挂掉后，本机 WebSocket 一定断开，连接不能迁移。入口停止把新连接发给故障实例，客户端按退避策略重连到健康 Gateway。新连接执行 `ClaimRoute` 覆盖 Redis 旧 route，未 ACK 消息从 pending 回放，并可按一个会话的 last seq 补缺口。旧 route 即使来不及清理，也会因 TTL 过期。

### 详细回答

Gateway 的消息历史不在本机内存，所以进程退出不会删除 MySQL 记录。但本机 map、尚未处理的内存队列任务和 TCP 连接会消失。客户端如果没有收到最终处理结果，应复用原 client_msg_id 重试。

正常退出会执行条件清理。异常退出没有 defer，Redis route 的 TTL 是最后防线。新连接的 routeValue 包含新的 Gateway 和 ConnectionID，可以立即覆盖旧值。旧 Gateway 若只是网络隔离后恢复，它的心跳必须比较所有权，不能把旧路由写回来。

### 结合 LinkGo

Kubernetes Service 只把新握手发给 ready Pod。网页检测断线后重连。`RefreshRouteIfMatch` 和 `ClearRouteIfMatch` 都使用 Lua 比较完整 routeValue，`ClientManager.Remove` 使用 CompareAndDelete。

### 为什么这样选，替代方案是什么

迁移 TCP 和 Go 对象不现实。连接服务通常选择外置可恢复状态加客户端重连。可以增加服务端 draining 通知，让计划下线更平滑，但节点突然掉电仍只能重连。

### 边界与追问

当前只实现一个当前 route，不是多设备恢复。进入 Gateway 内存队列但尚未落库的任务可能丢失，因此不能说 Gateway 故障完全无感。

## 2. Logic 挂了会怎样？

### 面试官问

Logic 在消息处理一半时崩溃，另一个实例能接着执行吗？

### 30 秒回答

正在执行的 RPC 不会自动无损转移，Gateway 会得到错误、超时或没有最终结果。后续重试可以通过 Etcd 列表落到其他健康 Logic，并复用 client_msg_id。若崩溃发生在 MySQL 前，消息没有持久化；若发生在 MySQL 后，唯一索引和旧记录恢复避免重复插入。服务发现解决后续选址，不解决请求中途事务接管。

### 详细回答

要按故障点分析，而不是只说“有多个副本所以没问题”。校验前崩溃几乎没有业务副作用。seq 已分配、MySQL 未写时可能留下序号空洞。MySQL 已提交、Redis 未投递时，历史存在但实时投递可能未完成。Redis pending 已写、发布前崩溃时，重连还能看到 pending。

Kubernetes 会补回 Logic 副本，Etcd 租约最终移除旧地址。Gateway 的 readiness 还会做 gRPC Health Check，但这些机制都不能接管栈里已经执行到一半的函数。

### 结合 LinkGo

客户端看到 `MESSAGE_REJECTED` 的可重试错误或连接异常后，应复用同一 client_msg_id。Logic 用 Redis 状态和 MySQL `uk_sender_client_msg` 判断重复，恢复时以数据库消息为准并重新验权。

### 为什么这样选，替代方案是什么

把请求全过程写成持久状态机能更强地恢复，但实现成本高。当前对消息事实使用数据库幂等，对会话摘要使用 Outbox，对接收投递使用 Redis pending，已经覆盖秋招项目最值得解释的窗口。

### 边界与追问

当前没有完整的接收方投递 Outbox，所以不能说 MySQL 后每个故障点都会由后台自动补齐。准确回答必须指出需要客户端重试或历史回源的窗口。

## 3. Redis 挂了，为什么不能只让消息继续写 MySQL？

### 面试官问

Redis 只是缓存，挂了以后是不是可以降级？

### 30 秒回答

在 LinkGo 里 Redis 不只是缓存，它还保存在线路由、seq、pending、ACK 状态、短期回放和限流。没有 Redis 时继续返回发送成功，会造成实时投递和确认状态不完整。因此相关服务 readiness 会失败，系统应明确拒绝依赖 Redis 的新操作。MySQL 历史仍在，但恢复后要重新验证单聊、ACK 和重连链路。

### 详细回答

降级要看丢掉的能力是不是业务正确性的一部分。只缺热点缓存时，可以回源数据库。LinkGo 的 route 决定目标 Gateway，seq 决定会话顺序，pending 决定短期恢复。它们同时不可用时，盲目继续处理会让发送端得到假成功。

已经提交到 MySQL 的消息不会因为 Redis 单实例停止而自动消失。服务恢复后，历史查询仍能读到，但 Redis 短期状态是否保留取决于部署持久性和故障方式。

### 结合 LinkGo

本地故障脚本会停止 Redis，观察 Gateway、Logic、Transfer 的 readyz，再重启依赖并运行消息 smoke。这个演练证明应用能发现单 Redis 故障和恢复，不证明 Redis 集群自动切换。

### 为什么这样选，替代方案是什么

可以把 seq 和接收方投递状态都持久化到 MySQL，Redis 只做缓存，这会减少跨存储事实，却增加数据库写热点。也可以使用 Redis HA 降低单节点不可用时间。当前项目保留 Redis 核心角色，并选择 fail-closed，不制造假成功。

### 边界与追问

production 需要 Sentinel、Cluster 或托管 HA，并测试故障切换。当前代码只连接一个稳定入口，没有完成这些平台能力。

## 4. MySQL 挂了会怎样？

### 面试官问

数据库不可用时，能不能先把聊天消息放 Redis，稍后再写？

### 30 秒回答

当前设计不这样降级。MySQL 是消息最终事实，持久化失败就不能向发送方报告业务成功。Gateway 和 Logic readiness 会受影响，新登录、关系校验、消息写入和历史查询会失败。客户端保留 client_msg_id，恢复后重试。先写 Redis 再异步落 MySQL 需要持久队列、幂等落库和积压保护，当前没有实现，不能临时假装存在。

### 详细回答

Redis 可以配置持久化，但当前 Key 设计和生命周期是在线路由与短期补偿，不是完整消息日志。若 MySQL 不可用还接受消息，需要定义接收成功的持久边界，处理 Redis 重启、重复落库、顺序和积压。这已经是一套不同架构。

当前同步落库让发送结果语义更清楚：MySQL 事务成功后才进入投递。恢复后先确认连接池能 Ping，再跑登录、单聊和历史查询，不能只看数据库进程启动。

### 结合 LinkGo

MySQL 保存 users、messages、relationships、group_members、conversations 和 outbox。群发送也要先持久化，Kafka 不替代消息事实表。

### 为什么这样选，替代方案是什么

高吞吐系统可以先写持久日志再异步物化数据库，但需要重新定义查询和一致性。秋招项目优先保证链路可解释，所以同步 MySQL 是合理取舍。

### 边界与追问

当前没有 MySQL 主从自动切换或应用层读写分离。生产需要稳定 DSN 背后的高可用、备份和恢复演练。

## 5. Kafka 或 Transfer 挂了会怎样？

### 面试官问

群聊消费者全挂了，消息是否丢失？

### 30 秒回答

Kafka 挂了时，Logic 不能把群任务成功写入 broker，因此不能声称群投递完成。Transfer 挂了但 Kafka 正常时，已写入且未超过 retention 的记录仍在；恢复或其他同组 Transfer 会从未提交 offset 继续消费。重复记录由成员级 lease 和 done 状态控制，失败任务进入 retry 或 DLQ。单聊主链路不依赖 Kafka。

### 详细回答

要区分 broker 与 consumer。Kafka broker 不可用会影响 producer 写入和 consumer 读取。Transfer 进程不可用只造成消费积压。恢复后的消费位置由 group offset 决定。

若 Transfer 投递全部成员后、提交 offset 前崩溃，Kafka 会重新交付。recipient 状态使已 done 成员跳过。若 RedisDelivery 已返回、done 尚未写，可能再次投递，所以客户端仍需去重。

### 结合 LinkGo

Logic readiness 会检查 Kafka，Transfer readiness 检查 Redis 和 Kafka。消费使用 `FetchMessage -> 处理或耐久转移 -> CommitMessages`，retry/DLQ 写失败时不提交原记录。

### 为什么这样选，替代方案是什么

Kafka 将群扩散从 Logic 请求线程拆出，允许消费者积压后恢复。代价是最终投递延迟、重复消费和运维复杂度。若群很小且系统规模有限，同步或进程内队列更简单。

### 边界与追问

Kafka retention 到期仍未消费，数据可能被删除。当前没有验证生产多副本 broker、跨可用区容灾和 DLQ 人工回放平台。

## 6. 十台 Gateway 满了，怎样扩容和缩容？

### 面试官问

怎么把原来的连接分到第十一台？缩容又怎么做？

### 30 秒回答

扩容时先启动 Gateway-11，健康和 readiness 通过后，入口负载均衡把新握手分给它，旧连接不会自动搬迁。若要下线旧节点，先让 readiness 失败或从入口摘流，停止接收新连接，留出 draining 时间，再让客户端断开重连。新连接写入新 route，旧连接的条件续期和条件删除不能覆盖它。

### 详细回答

连接容量与请求吞吐不同。Gateway 连接很多但 CPU 不一定高，所以只按 CPU 扩容可能太晚。更合理的指标还包括当前连接数、分片队列深度、拒绝率、网络带宽和写延迟。

Kubernetes Deployment 增加 replicas 会创建 Pod，Service 在 readiness 成功后把它加入端点。缩容会终止选中的 Pod，其连接必然断开。PDB 和滚动策略可以限制计划内同时不可用数量，不能阻止机器突然故障。

### 结合 LinkGo

当前 HPA 只使用 CPU，这是可渲染示例，不是经过压测的连接型扩缩方案。Gateway 有 5 秒 preStop 窗口和客户端自动重连，但没有长时间精细 draining 控制面。

### 为什么这样选，替代方案是什么

手工扩容简单但响应慢。HPA 自动化更强，却需要正确指标和启动预热。当前先提供 CPU 示例，同时在面试中明确下一步应接 Prometheus 连接数和队列指标，而不是夸大现状。

### 边界与追问

`replicas: 3` 是三个 Pod，不一定三台机器。已有 WebSocket 无损迁移没有实现。扩容能提高的容量也受 Redis、MySQL、网络和 LB 限制，不会线性无限增长。

## 7. 你怎样理解一致性、可用性和降级？

### 面试官问

项目遇到依赖故障时，为什么有些接口降级，有些直接失败？

### 30 秒回答

我先判断失败的子步骤是否影响业务事实。登录认证成功但会话摘要查询失败时，可以先返回 token，让客户端后续刷新，这是功能降级。消息 MySQL 持久化或 Redis 路由与 pending 失败时，继续返回发送成功会产生假成功，所以应失败并让客户端幂等重试。降级不是“捕获错误继续跑”，而是明确哪些不变量仍然成立。

### 详细回答

系统无法同时把所有依赖故障都隐藏。设计时要列出每一步成功能证明什么，以及失败后剩下哪些状态。只读摘要可以稍后恢复，认证本身仍有效。消息落库失败则没有长期事实，不能降级成成功。Redis 失败时即使 MySQL 已有记录，接收方实时投递状态不完整，也不能把对方 ACK 说成已经完成。

健康检查同样要分层。liveness 主要判断进程是否需要重启，readiness 判断是否应该接新流量。短暂依赖抖动不应让所有 Pod 被 liveness 同时杀死，但 readiness 应避免继续扩大错误。

### 结合 LinkGo

Gateway、Logic、Transfer 有不同依赖的 readyz。故障脚本会先看 readyz 失败与恢复，再运行登录、单聊、离线、ACK 和群聊 smoke。只看 HTTP 200 的 healthz 不足以证明业务恢复。

### 为什么这样选，替代方案是什么

全部 fail-open 可用性表面高，却可能破坏数据；全部 fail-closed 又会让非核心功能拖死主流程。按业务不变量分类是更实际的取舍。

### 边界与追问

当前项目没有定义正式 SLO、错误预算和生产降级开关。面试中可以讲设计判断和本地故障证据，不能包装成公司线上容灾经验。

