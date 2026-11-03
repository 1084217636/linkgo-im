# LinkGo：按《肖宾_西电_AI软件开发工程师》讲项目

本稿以 2026-11-03 的 `linkgo-collab-next` 源码和该 PDF 的四条项目要点为准。先练「两分钟开场」，然后沿一次真实用户操作讲完整链路。代码入口和测试证据放在每段末尾，供被追问时打开。历史 IM 讲稿包含旧阶段描述，不要与本稿混背。

## 1. 开场怎么说

### 两分钟版

> LinkGo 是我做的学习社区协作平台。用户注册后可以创建或加入多个学习社区，在社区里发资料帖、评论和点赞，也可以和成员私聊、群聊；管理员发布学习任务，成员提交内容，管理员审核后会产生可查询的通知。这个项目的核心不是一个聊天页面，而是把社区的持久业务和实时通信接起来。
>
> 服务端主要有 Gateway、Logic、Transfer。Gateway 提供 HTTP 接口并持有 WebSocket 连接，Logic 处理登录和消息业务，Transfer 消费 Kafka 做群消息扩散。MySQL 保存用户、社区、帖子、任务、消息和通知这些长期事实；Redis 保存在线路由和短期待确认投递状态；Etcd 发现 Logic 实例；MinIO 保存附件。业务变更与通知意向先在 MySQL 同一事务里提交，后台再投 Kafka；客户端收到实时消息后有业务 ACK，断线可以回放短期未确认消息并按会话查历史。
>
> 我重点处理了三类问题：一是多 Gateway 下找人和消息重复投递，二是任务提交、审核和帖子编辑的权限与并发，三是数据库成功但 Kafka 或在线推送失败时如何恢复。我通过 Docker 多用户操作、故障演练、浏览器流程和 CI 验证这些路径。它是本地多实例演示环境，没有宣称生产级多机房高可用。

如果只给 30 秒，讲第一段第一、二句，第三段第一句，然后停下。不要主动报不熟悉的吞吐数字。

### 十分钟版的叙述顺序

按一个人的完整操作讲，顺序为：**注册并进入社区 → 发帖及附件 → 群聊/私聊 → 布置并审核任务 → 通知 → 故障恢复与验证**。每走一步，再说明负责的服务和存储。这样比先背十几个中间件更容易让面试官听懂。

1. **注册/进入社区（约 1 分钟）**：用户提交用户名和密码。服务端用 bcrypt 保存哈希，登录后发 JWT。生产形态应经 TLS 入口；仓库提供本机 HTTPS/WSS 演示，HTTP 端口只用于本机调试。社区创建者是 owner；成员加入后在 `group_members` 中有 active 状态和角色。HTTP 请求带身份，服务端按用户和社区核对权限，不能靠前端隐藏按钮。
2. **内容沉淀（约 1.5 分钟）**：成员发布问答、笔记或资料帖，其他成员评论、点赞。帖子 `version` 用于编辑时拒绝旧版本覆盖；`request_id` 和请求内容指纹处理超时后的同请求重试。帖子、评论、点赞分别是 MySQL 表；附件的二进制存对象存储，MySQL 保存归属、元信息与存储键。下载时再次查当前群成员资格。
3. **实时沟通（约 2 分钟）**：A 连 Gateway-1，B 连 Gateway-3。A 经 WebSocket 发带 `client_msg_id` 的消息；Gateway 调 Logic，Logic 查好友/群成员关系，分配会话 `seq`，把消息与投递 Outbox 同事务写 MySQL。投递层先在 Redis 给 B 写 `pending_ack`、正文索引和重试状态，再查 `route:B`，向 Gateway-3 专属 Pub/Sub 频道通知。Gateway-3 从本机连接表找到 B，把二进制帧写 WebSocket；B 返回业务 ACK 才清待确认状态。`message_id` 标识同一条持久消息，重推可能重复，客户端按它去重。
4. **群聊（约 1 分钟）**：群消息先持久化，群投递任务由 Logic 的 Outbox 交 Kafka。Transfer 消费后按收件人逐个处理，成员级租约/完成状态使重复消费能够跳过已处理成员；失败写 retry topic，超过限制转 DLQ，可靠转移后才提交原 Kafka offset。Kafka offset 说明消费者处理进度，不等于成员客户端 ACK。
5. **学习任务（约 1.5 分钟）**：管理员建草稿并发布；普通成员提交文本/链接/附件；管理员按提交版本审核通过或退回，退回后成员可修改再次提交。`group_tasks` 是任务，`task_submissions` 是每个成员的一份当前提交，`task_events` 记录变更及幂等结果。操作在事务里锁任务与角色，按 `version` 拒绝并发旧请求，并写 `task_notification_outbox`。非群成员访问、普通成员审核等越权请求都由后端拒绝。
6. **持久通知（约 1 分钟）**：后台从任务通知事件投影到 `community_event_outbox`，按租约领取后写 `community.events.v1`。消费者用 event ID 回查可信 MySQL 记录，再写 `community_notifications`，其中 `(event_id,recipient_id)` 唯一；Redis 只发“有新通知”提示，页面再查 MySQL 收件箱。Kafka 停机时任务和通知意向已提交，恢复后 worker 重试。不能说通知和消息传输处于一个跨 MySQL/Kafka 的原子事务。
7. **工程验证（约 1 分钟）**：Docker 演练真实页面的多用户闭环、Kafka 停机恢复；Go 测试与 race detector 在 CI 跑；Prometheus 采指标，Grafana 展示，Alloy 把结构化日志送 Loki，消息 `trace_id` 帮助关联 Gateway、Logic、Transfer 日志。当前这是日志关联，并非完整 OpenTelemetry span tracing。最后说一条边界：中间件在本地演示仍是单节点，不从本地结果推导海量用户容量。

## 2. 白板图与四条简历的代码对应

```text
浏览器 ─ HTTPS/WSS ─ Gateway ─ gRPC/Etcd ─ Logic ─ MySQL（业务事实、消息、Outbox）
                         │                  │
                         │                  ├─ Redis（route、pending、近期回放）
                         │                  └─ Kafka（群投递任务）→ Transfer → Redis/目标 Gateway
                         └─ HTTP 社区/任务/附件接口 → MySQL / MinIO
业务事件：MySQL 同事务写业务表+Outbox → publisher → Kafka → consumer → MySQL 通知收件箱
```

| PDF 中的主张 | 打开哪些实现 | 追问时的关键限定 |
|---|---|---|
| Gateway/Logic/Transfer、Etcd/gRPC | `cmd/gateway/`、`cmd/logic/`、`cmd/transfer/`、`cmd/gateway/internal/svc/logicrouter.go` | Gateway→Logic 是 gRPC；Logic→目标 Gateway 的在线唤醒仍用 Redis 定向 Pub/Sub。部分社区 HTTP 接口在 Gateway 进程直接访问 MySQL。 |
| seq、幂等、ACK、断线补偿 | `internal/logic/handler.go`、`handler_store.go`、`handler_idempotency.go`、`internal/delivery/redis.go`、`internal/server/ack.go`、`sync.go` | 接收端 ACK 不等于已读；Redis pending 有 7 天 TTL，长期正文以 MySQL 为准。当前不是完整多设备同步。 |
| 社区、帖子、任务权限与并发 | `internal/community/service.go`、`internal/learningtask/store.go`、`submission.go`、`sql/20261102_communities.sql`、`sql/20261028_learning_tasks.sql` | 同一个 request ID 重试返旧结果；同 key 换内容返回冲突；version 处理旧编辑/审核。 |
| Outbox/Kafka/通知 | `internal/community/worker.go`、`internal/logic/delivery_outbox.go`、`internal/logic/task_notifications.go` | 至少一次处理，消费者靠唯一键/状态幂等；不是恰好一次。 |
| MinIO 与权限 | `internal/attachment/store.go`、`object_store.go` | 上传与下载都校验群归属/成员；对象存储与 MySQL 不是同一事务，失败清理是补偿动作。 |
| Docker、CI、监控日志 | `.github/workflows/`、`docker-compose*.yml`、`deploy/observability/`、`docs/evidence/resume-verification-20261102.md` | Docker/CI/可观测组件已验证的范围按证据文件说，不说生产 SLA。 |

## 3. 面试官从业务追问时怎么答

**Q：为什么叫社区平台，而不是普通 IM？**

> 我把社区设为权限和内容的共同边界。成员可以聊天，也可以发长期保留的帖子和资料；管理员布置任务，成员提交后形成审核与通知闭环。消息适合即时交流，帖子/任务适合后续查找和协作，两类数据的生命周期不一样，所以我没有简单把帖子当成一条群消息保存。

**Q：一个新成员能看到旧帖子、旧消息、旧附件吗？**

> 当前访问主要按 active 群成员校验。帖子和附件的可见性要结合群级权限；历史群消息还涉及加入时间和会话成员记录。这个项目不宣称完整的“入群前历史不可见”策略已覆盖所有路径，若作为产品要统一定义加入时间水位和离群后的历史访问规则，再给 HTTP 历史、附件和消息同步共用同一权限契约。

**Q：重复发布帖子、重复提交任务怎么办？**

> 客户端生成 `request_id`，服务端把 `(actor_id,request_id)` 和请求指纹、结果保存在 MySQL。重试同一个请求直接返回之前的结果，拿相同 ID 发不同内容返回 409。任务另有 `(task_id,user_id)` 唯一键，一人一份当前提交；退回后才允许带正确版本再次提交。请求幂等和业务唯一约束解决的是不同问题。

**Q：两名管理员同时审核一份提交呢？**

> 事务内锁任务、核对当前管理员角色，再锁提交行；客户端带 `expected_version`。先提交的审核把 `submitted` 改成 `approved` 或 `returned` 并加版本。后到的请求发现版本或状态变化，返回冲突，不会覆盖第一次审核。`task_events` 和通知意向与审核状态一起提交。

**Q：任务已审核但 Kafka 挂了，用户能看到什么？**

> 审核结果已经在 MySQL，所以打开任务详情能读到新状态。通知 Outbox 保持待处理，publisher 继续重试；Kafka 恢复后消费者将通知写入收件箱。在线提示可能晚到，因此页面不能只依赖 WebSocket 提示来判断任务状态。

**Q：Pub/Sub 是广播给所有 Gateway 吗？**

> 不是。Logic 从 Redis `route:<uid>` 读目标 Gateway，发布到那个 Gateway 专属的频道。目标 Gateway 再从自己的连接 map 查用户。Pub/Sub 只负责在线提示，不承担消息事实；订阅者不在时通知会错过，未 ACK 状态和 MySQL 历史用于恢复。

**Q：离线 7 天以后 Redis pending 过期，消息是不是丢了？**

> MySQL `messages` 仍有持久消息。已知会话可按客户端保留的会话游标查历史并补缺；页面的 IndexedDB 保存本地消息与同步进度。当前没有一套完整的服务端多设备收件箱发现协议，因此不能保证任意新设备一次登录自动得到所有历史会话的全部消息。这是我会明确说明的边界。

**Q：用户名和密码走 HTTP 是明文吗？JWT 能加密吗？**

> HTTP 本身不加密，JWT 签名也不加密请求体。仓库的本地安全演示用 HTTPS/WSS 入口，浏览器通过 HTTPS 登录；服务端用 bcrypt 存密码哈希，JWT 用来表示之后的登录身份。WebSocket 先通过 HTTPS 申请一次性、30 秒有效的 ticket，避免把长期 JWT 直接放在 URL。正式部署需要可信证书与 TLS 终止配置；直接访问本机 HTTP 调试端口不具有传输保密性。见 `scripts/start_secure_demo.sh`、`cmd/gateway/internal/middleware/wsticket.go`。

**Q：文件能放 MySQL 吗，为什么 MinIO？**

> 文件大、读写方式和帖子元数据不同。我把对象放 MinIO，MySQL 的 `attachments` 保留文件 ID、群 ID、上传人、大小、哈希和存储键。下载请求先查 active 成员及文件归属，再打开对象。上传限制 25 MiB 并检查文件类型。存储对象和数据库不能跨系统原子提交，数据库写失败时做对象删除补偿，仍需考虑删除失败的残留清理。

## 4. 数据表、Redis 与 Kafka：说出具体例子

以“管理员发任务，成员提交后被退回”为例：

```text
im_groups(group_id=c-7, owner_id=A)
group_members(c-7,A,owner,active) / (c-7,B,member,active)
group_tasks(task_id=t-9, group_id=c-7, status=published, version=2)
task_submissions(submission_id=s-4, task_id=t-9, user_id=B,
                 status=returned, attempt_no=1, version=2, review_note=需要补实验记录)
task_events(event_id=e-6, action=review, actor_uid=A, request_id=r-10, result_json=...)
task_notification_outbox(event_id=e-6, target_id=B, status=pending)
community_event_outbox(event_id=task-..., kind=task_review, status=pending/processing/done)
community_notifications(event_id=task-..., recipient_id=B, read_at=NULL)
```

这些是便于解释的示意 ID，不是数据库实测行。`task_submissions` 保留当前提交及审核状态，`task_events` 是行为审计与幂等结果，Outbox 是还没完成的外部通知工作，`community_notifications` 是用户可查的持久收件箱。它们不能混成一张“是否已读”表。通知消费后读收件箱；Redis 提示丢失时仍可查询它。

消息的 Redis 示例：`route:B` 是字符串，值含 Gateway 与连接 owner；`pending_ack:B` 是 ZSet，member 为 `message_id`、score 为投递时间；`ack_idx:B` 是 Hash，以 `message_id` 为 field、完整编码消息为 value；`ack_retry:B` Hash 记重试次数。一个 B 同时有多个会话的未 ACK 消息，因此 pending 按**用户**聚合，而不是每会话一条 key。`session_timeline:<session>` 按 seq 排近期消息；正文和权限的长期事实仍查 MySQL。

Kafka 至少要分清两条业务：群消息投递由 `cmd/transfer/main.go` 处理；社区持久通知用 `community.events.v1`，消费者组为 `community-notifications-v1`，异常 Kafka 记录可去 `community.events.dlq.v1`。不要把两条链混成“所有消息都由 Transfer 一种消费者处理”。

## 5. 技术选型与难点的回答

**为什么 MySQL 事务 + Outbox？** 数据库一旦答应业务成功，外部 Kafka/Redis 可能正好不可用。业务行和待办行同事务，后台扫描待办并重试，这样不会因进程在提交后崩溃就失去“还需要投递”这件事。代价是异步通知会延迟、可能重复，需要消费者幂等、重试上限和 dead 行排查。

**为什么 Redis 而不是全部查 MySQL？** 在线 route、ACK 超时和短期回放访问频繁，Redis 适合快查和 TTL；长期消息与权限仍在 MySQL。Redis 丢热状态要靠游标/历史恢复，不能说缓存与数据库天然一致。

**为什么 Kafka？** 群聊一条消息对多个成员，社区事件可能 fan-out 给多名成员，外部通知有积压与重试需求。Kafka 提供持久日志和消费者进度；但它不提供 MySQL 与 Kafka 跨系统事务，也不证明客户端已收到。

**遇到的具体困难怎么讲？** 选“审核已提交、Kafka 暂停”最清晰：先复现数据库状态已变但收件箱未更新；检查 Outbox 仍 pending，恢复 Kafka 后看 worker 发布、消费者写唯一收件箱行，重复消费后仍一条。这个故事展示了问题现象、数据库不变量、恢复和验证，来源是 `scripts/verify_communities.py --faults`，不要编造成生产事故。

## 6. 测试数字怎么报，没测什么

可报当前代码证据中的：`go test ./...` 通过；Docker 脚本覆盖注册、越权、并发编辑、任务状态流转和 Kafka 停机恢复；浏览器脚本覆盖页面上传/群聊/私聊/任务；CI 包含 Go 单测、race 与镜像构建；Prometheus/Loki 演练见 `docs/evidence/resume-verification-20261102.md`。本地 8 并发写 100 帖只验证小样本写入正确性及限流行为，**不能**据此推算平台峰值容量，也不必主动放进简历主讲。

没有完成的边界：单值用户路由不是同账号多端；本地单节点中间件不是跨机房容灾；Pub/Sub 不能代替持久投递；日志 TraceID 不是完整分布式追踪；旧页/旧客户端讲稿中的 2026-08/09 数字不能与这次社区业务吞吐混报。

## 7. 复习与演示

第一遍只背第 1 节两分钟稿和第 3 节前五问；第二遍对照白板图，能用自己的话解释 `group_tasks → task_events → Outbox → notification` 与 `messages → pending → Gateway → ACK`；第三遍打开 `internal/community/service.go`、`internal/learningtask/store.go`、`internal/community/worker.go`，分别找事务、权限、通知一处关键代码。启动和账号见 `scripts/start_community.sh`、`docs/evidence/resume-verification-20261102.md`；测试时先记录当前 commit，再跑 `go test ./...` 和相关演示脚本。

## 8. 高频深入追问：按真实边界回答

**ACK 丢了会怎样？** B 已经处理消息，但服务端还保留 `pending_ack`，超时可能重推同一个 `message_id`。客户端用这个稳定 ID 去重，再回 ACK。不能说传输绝不重复；也不要把 ACK 说成“用户已读”。

**`client_msg_id` 和 `message_id` 为什么不能只用一个？** 前者由发送端在第一次提交前生成，同一次意图重试必须不变，解决“入库了但响应丢了”的重发判定；后者是服务端持久消息身份，供投递、接收、ACK 与去重。`session_id+seq` 标识会话位置，seq 在 Redis 分配后数据库提交失败可有空洞，查询按真实存在的消息游标继续，不等待缺号。

**逻辑节点如何知道用户在哪台 Gateway？** Etcd 告诉 Gateway 可用的 Logic 服务地址；Redis `route:<uid>` 告诉投递端用户连接在哪台 Gateway。前者是服务发现，后者是用户连接路由。WebSocket 连接对象本身只在持有它的 Gateway 内存里。

**Gateway 崩溃、Redis route 未删除怎么办？** route 有 TTL 与连接 owner；新连接写新 owner，旧连接清理只能条件删除自己持有的 route。重连后新 Gateway 持有新 WebSocket；未 ACK 消息可先回放 Redis 短期 pending，已知会话再按 MySQL 历史游标补。稳定负载均衡入口负责把重连送到健康 Gateway；浏览器固定本机端口不等于已经实现自动切换生产入口。

**Outbox worker 抢同一任务会不会重复？** MySQL 条件领取/租约允许多实例抢占，到期后其他 worker 可接手；旧 worker 迟到更新用领取代数或 lease token 栅栏化。外部 Kafka/Redis 副作用仍可能在崩溃窗口重复，靠 event ID、message ID 和接收端唯一约束处理。租约防永久卡住，幂等防重复生效，两者都需要。

**为什么一个群聊成员失败要重试整条 Kafka 记录？** Transfer 当前同一条群任务串行处理收件人；每个成员有处理状态，已完成的成员在重试时跳过。失败任务可靠进入 retry topic 或 DLQ 后才提交原 offset。它保留了简单的恢复语义，但超级大群的单任务扇出吞吐会受串行限制，可以按成员范围拆小任务再扩分区。

**慢查询怎么排？** 先按具体 API 记录延迟和 SQL，结合 `EXPLAIN` 看是否命中索引。帖子动态按 `(group_id,status,pinned,created_at,post_id)` 做有序游标查询，通知按 `(recipient_id,id)` 翻页，任务按 `(group_id,status,created_at,task_id)` 查列表。索引能减少扫描，但会增加写入成本；不要在没跑生产负载时声称已完成全量 SQL 优化。

**内存泄漏/连接堆积怎么排？** 先区分真泄漏、连接本来就增加和消息队列积压。观察活跃 WebSocket、goroutine 数、堆内存、队列长度，结合 Go pprof 的 heap/goroutine 与容器内存；断开测试连接后观察是否回落。重点查连接关闭时是否从本机 map 移除、心跳/重试 goroutine 是否退出、Pub/Sub 订阅是否关闭。仓库有 `tools/leaklab` 和监控指标，但不能说已证明整个系统永无泄漏。

**CI 与监控到底解决什么？** CI 防止可复现的编译、单测、race 与镜像回归进入主线；监控指标告诉我错误率、延迟、连接数和 Kafka/通知积压是否异常；结构化日志按 `trace_id`/`event_id` 找一条具体业务从入口到异步消费者的轨迹。Grafana 是查看入口，Prometheus 存指标，Alloy/Loki 收集和查询日志；本项目没有宣称自动化生产发布或完整分布式 Trace 系统。
