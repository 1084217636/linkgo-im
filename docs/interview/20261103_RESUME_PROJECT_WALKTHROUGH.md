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

### 按真实操作串起来讲：先理解，再练十分钟口述

主线只有一条：**浏览器打开 HTTPS 页面 → 注册登录 → 创建或加入社区 → 上传资料并发帖 → 私聊/群聊 → 发布、提交、审核任务 → 收到通知 → 断线恢复与排障**。以下“浏览器”指仓库 `web/` 的社区页面。每一步可以用“输入、传输、校验、存储、返回、失败处理”六个词复述。

#### 1. 从浏览器打开页面到登录成功

1. 用户访问 `https://localhost:8443/community/`。浏览器先与本机 Nginx 建立 TCP 连接，再进行 TLS 握手：检查证书、协商加密参数，之后 HTTP 请求及响应就在加密连接中传输。本地脚本生成的是自签名证书，需要在浏览器中信任；正式对外部署必须换成受信任的证书。Nginx 提供页面，把 `/api/v1/` 反向代理到容器网络中的 Gateway。**这里保护的是浏览器到 Nginx 的外部传输；容器内 Nginx→Gateway 和 Gateway→Logic 目前没有声称全链路 TLS。**[Nginx 配置](../../deploy/community/nginx.conf)、[启动脚本](../../scripts/start_community.sh)
2. 点“注册”时，页面通过 HTTPS 发 `POST /api/v1/register`，请求体是 JSON 的用户名和密码。TLS 负责在网络上传输时加密；**不是浏览器先用 bcrypt 加密再发送**。Gateway 对 JSON、长度和请求频率做检查，`community.Service.Register` 校验用户名 3–32 字、密码 8–72 字节，用 bcrypt 加盐计算单向哈希，向 MySQL `users` 写 `user_id、username、password(哈希)、status`。用户名重复由数据库唯一约束挡住。数据库里没有可解密的明文密码。[注册逻辑](../../internal/community/service.go)
3. 页面接着通过 HTTPS 发 `POST /api/v1/login`。Gateway 通过 Etcd 发现可用 Logic，并用 gRPC 把登录请求交给 Logic。Logic 按用户名查 `users`，用 `bcrypt.CompareHashAndPassword` 比对用户输入和库里的哈希，并核对账号状态。成功后用服务器密钥做 HS256 签名，生成含 `user_id、签发时间、24 小时过期时间` 的 JWT；Gateway 把它作为 JSON 响应返回给浏览器。JWT 是**带签名的身份凭证，不是密码，也不负责加密内容**。页面把 token 放在当前 React 状态，后续 HTTP 请求放 `Authorization: Bearer <JWT>`；Gateway 验签、检查过期时间，从 token 取用户 ID 写入请求上下文。业务代码从上下文取身份，不能相信客户端提交的 `user_id`。[登录实现](../../internal/logic/handler_auth.go)、[JWT](../../internal/middleware/auth.go)、[页面请求](../../web/src/main.tsx)
4. 聊天需要长连接。页面先带 JWT 通过 HTTPS 请求 `POST /api/v1/ws-ticket`，Gateway 随机生成 32 字节一次性 ticket，只把其 SHA-256 摘要作为 Redis key 保存，值是用户 ID，TTL 30 秒。页面再发 `wss://localhost:8443/ws?ticket=...`；Nginx 转发 HTTP Upgrade，Gateway 原子 `GETDEL` 消费 ticket、检查浏览器 `Origin`，通过才返回 101 并建立 WebSocket。一个 ticket 不能重放，长期 JWT 也不必放在 WebSocket URL。Gateway 给这个 socket 生成 `connection_id`，保存在本机连接表，并把 `route:<uid>` 写到 Redis；之后页面每 20 秒发心跳。这里的 WSS 也是 TLS 加密的 WebSocket。[ticket](../../cmd/gateway/internal/middleware/wsticket.go)、[建连](../../cmd/gateway/internal/handler/websockethandler.go)、[前端连接](../../web/src/main.tsx)

**面试口述：**“用户到入口统一走 HTTPS/WSS。密码只在 TLS 内的登录请求里传到服务端，注册时数据库存 bcrypt 哈希，登录时比对哈希后签发 JWT。HTTP 接口验 JWT；WebSocket 先用 JWT 换 30 秒的一次性 ticket，再升级连接。进入具体社区时服务端继续查成员表，所以有 token 也不能访问任意社区。”

**容易追问：为什么还要 JWT，TLS 不是已经加密了吗？** TLS 保护传输中的字节，不能说明这次请求属于哪个登录用户；JWT 提供可校验的身份声明。bcrypt 解决数据库泄露时密码明文暴露问题。三者负责的环节不同。

#### 2. 创建或加入社区：哪些身份能做哪些事

用户登录后页面请求 `GET /api/v1/communities`，看到可发现的社区；点击创建会 `POST /api/v1/communities`。Gateway 验 JWT，取上下文中的用户 ID，再把请求交给社区业务服务。一次创建事务内写 `im_groups`（社区本体）、`community_profiles`（简介等）、`group_members`（创建者 role=`owner`、status=`active`）、`conversation_members`（群聊会话成员）。四项同时提交，避免有群却没有创建者。加入社区时先锁群记录，确认社区允许加入、用户未被移除，再写或恢复 active 成员；退出/移除会同步撤销会话成员资格，并让相关 Redis 成员缓存失效。[社区服务](../../internal/community/service.go)、[HTTP 路由](../../cmd/gateway/internal/handler/communityroutes.go)

后面的发帖、任务、附件、群聊都以 `group_id` 为权限边界：只要是 active 成员才能访问相应数据；公告、任务创建和审核还要检查 owner/admin 角色。页面隐藏管理员按钮仅是交互提示，后端在数据库查询时仍检查角色。举例：B 手改请求中的 `group_id` 指向未加入的 C 社区，服务端查不到 `(C,B,active)` 就拒绝；登录成功不代表拥有所有社区权限。

#### 3. 发帖、评论、点赞和附件：长期内容怎么存

成员在页面选问答/笔记/资料，填标题正文并点击发布。页面用 HTTPS JSON 调社区帖子接口，带随机 `request_id`。Gateway 验身份、解析有大小限制的请求体；业务层在 MySQL 事务内先锁操作者行，查询 `(actor_id, request_id)` 是否已经处理：同一请求和相同内容返回上次结果；同一 ID 换了内容返回冲突。随后查 active 群成员，写 `community_posts`，必要时校验附件归属并关联 `community_post_attachments`，再写社区事件 Outbox，最后提交事务。发帖成功返回 `post_id、version`。别人刷新动态时，按群和状态查询，最多取 20 条，使用游标继续翻页；评论在 `community_comments`，点赞在 `community_reactions`。编辑帖时必须带看到的 `expected_version`，事务锁帖并比较版本，成功加一；两个编辑者都拿版本 1，先提交者变 2，后一个收到冲突，需要刷新，防止静默覆盖。[帖子实现](../../internal/community/service.go)

附件不是把文件字节放进 MySQL。页面通过 HTTPS multipart 上传，Gateway 先核对本人是该群 active 成员，限制 25 MiB、文件名和可接受类型，读取内容计算 SHA-256；对象字节放 MinIO，MySQL `attachments` 保存附件 ID、群 ID、上传人、文件名、类型、大小、哈希和存储键。发帖只引用附件 ID，服务端还检查它确属该群且是本人上传。下载必须再次校验当前成员身份，再由存储键打开对象。对象存储与 MySQL 不能共用事务：如果对象已存而数据库登记失败，代码尝试删除对象作为补偿，失败仍可能留孤儿对象，不能宣称强一致。[附件实现](../../internal/attachment/store.go)

**面试口述：**“帖子是可检索的长期内容，保存在 MySQL；聊天消息是实时会话流。我的发帖接口用 `request_id` 防请求重放、用版本号防并发覆盖，附件字节交 MinIO，数据库仅存可校验的元数据与归属。读、写和下载都会查社区成员资格。”

#### 4. 私聊与群聊：消息从 A 的页面到 B 的页面

以 A 给 B 发私聊为例。页面先生成 `client_msg_id`，把待发送消息记到本机 IndexedDB，再将消息字段编码成 Protobuf 二进制帧，经已有 WSS 发给持有 A 连接的 Gateway。Gateway 从**已经认证的连接**取得 A 的身份，调用 Logic 的 gRPC 消息接口；Logic 验证私聊关系、检查 `client_msg_id` 重试，确定规范化的双方 `session_id`，从 Redis 给该会话分配 `seq`，形成持久 `message_id`。Logic 在 MySQL 同一事务写 `messages` 和 `delivery_outbox`。事务提交成功后给 A 返回“服务端已接受/入库”结果：它**不是 B 已收到**。如果 A 刚发完断线但数据库已提交，带原 `client_msg_id` 重试会查到同一条持久消息，不应生成两条。[消息入口](../../internal/logic/handler.go)、[保存](../../internal/logic/handler_store.go)

后台投递时，对接收人 B 先在 Redis 记短期待确认状态：`pending_ack:B` 是按投递时间排序的 ZSet，member 是 `message_id`；`ack_idx:B` Hash 用 message ID 找编码后的消息正文；`ack_retry:B` Hash 记重试次数。然后查 `route:B`，其值指出哪台 Gateway 和当前连接 owner。若 B 在 Gateway-3，Logic 向 **Gateway-3 专属** Redis Pub/Sub 频道发通知；其他 Gateway 不会为了这条消息逐个处理广播。Gateway-3 在本机用户连接表找到 B 的 socket，把二进制帧写入该 WebSocket；它不是调用浏览器的 HTTP 接口。B 的页面按 `message_id` 写 IndexedDB/更新 UI，再发业务 ACK。Gateway 收到 ACK 后清 Redis 待确认状态并更新确认进度；“收到”不等于用户点开“已读”。若 ACK 丢失，超时重推的还是同一 `message_id`，页面写入同一条记录并再 ACK。[Redis 投递](../../internal/delivery/redis.go)、[页面接收](../../web/src/main.tsx)

群聊的前半段类似，但 Logic 验证群成员并持久化收件人快照后，将**一条群消息扩散任务**通过 Outbox 交给 Kafka。`Transfer` 作为 Kafka 消费者取任务，当前对收件人逐个处理，给每位成员使用 Redis 租约与 `done` 状态；已完成成员在任务重试时跳过。投递失败写 retry topic，超过上限写 DLQ；成功处理或已可靠转移任务后才提交 Kafka offset。Kafka offset 是服务端消费进度，**不证明每个客户端已 ACK**。群成员的在线推送最终也复用上面的 pending、路由、WebSocket、业务 ACK 链路。[群扩散](../../cmd/transfer/main.go)

离线 B 没有可用 socket，Pub/Sub 无法替代持久存储。B 重连时 Gateway 自动回放 Redis 中该用户尚未 ACK 的短期消息；页面打开会话或 WSS 重连后还通过 HTTP 按会话查 MySQL 历史，页面按稳定 `message_id` 合并并按 `seq` 排序。Redis pending 有 TTL，MySQL `messages` 是长期事实；当前补偿依赖已知会话和历史查询，并不承诺全新设备自动扫完所有会话。新连接写新 `route:B`；旧连接退出时只在 Redis 值仍等于自己 owner 时删除，避免旧连接清理误删新路由。[重连同步](../../internal/server/sync.go)、[页面历史](../../web/src/main.tsx)

#### 5. 学习任务：管理员发布、成员提交、管理员审核

管理员在社区“学习任务”页点击创建，页面 HTTPS 调 `POST /groups/{group_id}/tasks`，后端先验证 owner/admin，再在 `group_tasks` 写一条 `draft` 任务。点击发布时请求携带 `request_id` 和当前 `expected_version`，服务端在事务内锁任务、复核角色和版本，改为 `published`、版本加一，并写 `task_events` 和 `task_notification_outbox`；提交后成员列表才可见该任务。任务状态是草稿→已发布→关闭，关闭前代码还会检查是否有待审核提交。[任务服务](../../internal/learningtask/store.go)

成员打开任务，后端会重新查该成员是不是本群 active 成员；成员提交文本、链接或附件时，后端核对任务已发布、附件是本人在本群上传。`task_submissions` 的 `(task_id,user_id)` 唯一约束表示每人一份当前提交：首次提交插入 `submitted、attempt_no=1、version=1`；管理员退回后，成员带当前版本再次提交，在原行更新内容、尝试次数和版本。管理员点通过或退回时带 `expected_version`，事务锁住任务和该提交，要求当前状态确为 `submitted`，改 `approved` 或 `returned`，并把审核人、理由、时间和待通知事件一起提交。两名管理员同时审核，后到者会看到状态/版本不符而收到冲突，不会覆盖先到者。管理员的“完成情况”接口按 active 普通成员统计未提交、待审核、退回和通过数量，数据直接来自 MySQL。[提交与审核](../../internal/learningtask/submission.go)、[页面任务操作](../../web/src/main.tsx)

这里要分清三种保护：`request_id` 解决网络超时后**同一个操作重复请求**；唯一键解决**一个成员不能产生两份当前提交**；`expected_version` 解决**两个人拿旧页面并发修改**。这三种不是一回事。

#### 6. 状态通知：审核成功后用户怎么知道

审核事务成功时，业务状态、`task_events` 和 `task_notification_outbox` 已经一起进入 MySQL；即使 Kafka 此时停机，审核结果本身也可在任务页查到。Logic 后台 goroutine 每秒扫描到期的社区 Outbox，事务内用 `FOR UPDATE SKIP LOCKED` 领取一条，设置 30 秒租约和 token，事务外发送到 Kafka `community.events.v1`。发送成功标 `done`；失败退避重试，达到上限标 `dead` 供排查。租约到期允许别的 worker 接手，因此可能重复发布。[通知 worker](../../internal/community/worker.go)

Logic 内的另一个 Kafka 消费循环属于消费组 `community-notifications-v1`。它取到事件 ID 后**重新从 MySQL 读可信事件**，按当前 active 成员投影到 `community_notifications`；`(event_id,recipient_id)` 唯一键使重复消费不会生成两条通知。MySQL 提交后才提交 Kafka offset。Redis Pub/Sub 只给在线页面发一条“通知有变化”的提示，不带正文；页面收到提示就调通知 HTTP 接口重新查 MySQL，另外每 10 秒轮询一次。B 离线时提示会错过，但收件箱行仍在；重新登录后可查询。[通知投影](../../internal/community/worker.go)、[页面收件箱](../../web/src/main.tsx)

**面试口述：**“审核结果与通知意向在一个 MySQL 事务里。后台领取待办、发 Kafka，消费者幂等写 MySQL 收件箱，Redis 只负责在线唤醒。这样 Kafka 挂了最多导致通知延迟，不会把已经通过的审核回滚；也不能说 MySQL 与 Kafka 构成一个跨系统事务。”

#### 7. 故障、测试与监控：软件分别干什么

Docker Compose 在本机启动 Nginx、3 台 Gateway、Logic、Transfer，以及 MySQL、Redis、Kafka、Etcd、MinIO 和观测组件。它让多进程链路可重复启动，**不是生产多机房部署**。`./scripts/start_community.sh` 会生成本地 TLS 证书、应用 SQL 迁移并启动服务。页面 `https://localhost:8443/community/` 是实际操作入口。[启动脚本](../../scripts/start_community.sh)

监控要按“程序产出数字 → 定期采集 → 展示/告警”理解。Go 代码在 `/metrics` 暴露指标，比如当前 WebSocket 数 `linkgo_ws_connections`，社区 HTTP 请求数与耗时、社区事件发布成功/重试/死亡数。**Prometheus** 每 5 秒主动抓 Gateway、Logic、Transfer 的 `/metrics`，把随时间变化的数字存下来；其 `up` 只能说明抓取成功。**Grafana** 查询 Prometheus 并展示曲线，自己不是采集器。Prometheus 规则连续发现 Transfer 抓取失败或社区 Outbox 重试，会把告警发给 **Alertmanager**；本地 Alertmanager 把告警送到 Gateway 的内部审计入口，不能说已经接入真实值班通知。[指标](../../internal/metrics/metrics.go)、[抓取](../../deploy/observability/prometheus.yml)、[告警规则](../../deploy/observability/rules/community.yml)

日志是另一条线。Gateway、Logic、Transfer 输出带时间、阶段、`trace_id`/`message_id`/`event_id` 的结构化日志；**Alloy** 发现本项目 Docker 容器并收集 stdout 日志，送到 **Loki**；**Grafana** 再查询 Loki。指标告诉我“什么时候开始失败、影响多大”，日志告诉我“一条具体任务在哪一步失败”。`trace_id` 是跨日志关联字段，当前没有 OpenTelemetry Span 全链路追踪。排障例子：收不到通知时先看任务状态和 Outbox 行，再看 `linkgo_community_events_total{operation="publish",result="retry"}` 是否增加，最后在 Loki 中按 event ID 查发布/消费日志，判断卡在 Kafka 发布还是收件箱投影。[Alloy](../../deploy/observability/alloy/config.alloy)、[告警演练](../../scripts/verify_community_observability.py)

项目确有运行过的验证：`go test ./...`；`verify_communities.py --faults` 验证权限、并发、任务流转和暂停 Kafka 后恢复；浏览器脚本真点注册、发帖、附件、聊天和审核；`verify_community_observability.py` 暂停 Transfer，观察 Prometheus 告警、Alertmanager 收到 firing 与 resolved，并查询 Loki 日志。CI 的 GitHub Actions 自动做 Go 单测、race 检测、构建、前端构建和配置检查；CI 是提交时自动检查，不是生产自动发布。100 帖/8 并发只是本地业务正确性与限流实验，不代表平台能支撑海量用户。[核验证据](../evidence/resume-verification-20261102.md)、[CI](../../.github/workflows/ci.yml)

**第一次自己看监控，按下面五步做：**

1. **先进入仓库**，再启动；如果当前在 `/home/xiaobin/myproject`，直接执行 `bash ./scripts/start_community.sh` 会找不到脚本。页面需要信任本地自签名证书。

   ```bash
   cd /home/xiaobin/myproject/linkgo-collab-next
   bash ./scripts/start_community.sh
   ```

   然后打开 `https://localhost:8443/community/`，注册两个账号、加入同一社区，发一条群消息并让接收方收到。想先跑自动化业务验收，可在**同一个仓库目录**运行下面这条命令；`SSL_CERT_FILE` 让 Python 信任启动脚本生成的本地证书，测试请求仍通过 HTTPS 加密入口。

   ```bash
   SSL_CERT_FILE=.local-tls/cert.pem python3 scripts/verify_communities.py --base https://localhost:8443
   ```
2. 打开 `http://localhost:9090/targets`。这里是 Prometheus 的采集目标列表；看 `linkgo-gateway`、`linkgo-logic`、`linkgo-transfer` 是否为 UP。UP 只证明 `/metrics` 能抓到，接着要做业务操作看数字变化。
3. 在 Prometheus 查询框输入 `sum(linkgo_ws_connections)`，连上第二个页面后观察当前 WebSocket 数变化。再查 `sum(rate(linkgo_community_requests_total[1m]))` 看最近一分钟社区 HTTP 请求速率。若一分钟内没请求，速率为 0 是正常的。Grafana `http://localhost:3000`（本地演示账号 `admin/linkgo`）的 Prometheus 数据源把这些查询画成图；看图时先核对时间范围。
4. 打开 Grafana 的 Explore，数据源选择 Loki。输入 `{project="linkgo"}` 看项目容器日志，再用具体 `event_id` 或 `message_id` 搜对应阶段。如果查不到，先确认 Alloy 容器在运行、Loki 数据源可用、刚才确有新业务流量；“没有日志”不能直接判断业务成功或失败。
5. 跑 `python3 scripts/verify_community_observability.py`。脚本会暂停本地 Transfer，等待 `up{job="linkgo-transfer"}` 触发告警，再恢复 Transfer，核对 Alertmanager 的 firing/resolved 审计和 Loki 中的相关日志。**这是受控故障演练，不是线上故障经验。**如果本机尚未启动 Docker 环境，先不要把历史证据写成自己这次亲手重跑的结果。

面试官若问“你用 Grafana 排查过什么”，就用上面这个实例说：先看采集目标与告警定位服务，接着看请求或事件指标判断时间和范围，再按 event ID 找日志确定是发布失败还是消费失败，恢复组件后重复业务烟测。**Prometheus 存数字、Grafana 看数字和日志、Alloy 搬日志、Loki 存并查日志、Alertmanager 管告警通知**，这是最需要记住的对应关系。

## 2. 白板图与四条简历的代码对应

```text
浏览器 ─ HTTPS/WSS ─ Nginx(TLS入口) ─ HTTP/WS ─ Gateway ─ gRPC/Etcd ─ Logic ─ MySQL（业务事实、消息、Outbox）
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

> 社区页面的用户入口是 `https://localhost:8443/community/`，浏览器的注册、登录和后续 HTTP 请求都走 TLS；WebSocket 走 WSS。密码到服务端后，注册时存 bcrypt 哈希，登录时比对哈希；JWT 是签名的身份凭证，不负责传输加密。建立 WebSocket 前先用 JWT 经 HTTPS 换取 30 秒一次性 ticket，避免长期 JWT 出现在 URL。项目当前由 Nginx 终止 TLS，容器内服务间调用尚未配置双向 TLS；对外部署还要换受信任证书。见 `scripts/start_community.sh`、`deploy/community/nginx.conf`、`cmd/gateway/internal/middleware/wsticket.go`。

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
