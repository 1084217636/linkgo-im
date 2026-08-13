# LinkGo IM 项目 45 分钟逐字讲稿

这是一份可以直接开口练习的讲稿，不是知识点目录。正文中的“我说”就是你在面试时可以说的话。“面试官可能打断”表示对方很可能在这里追问，后面给出了可以直接回答的版本。

第一次练习时不要追求背完。先把前 15 分钟讲顺，再逐步加入可靠性、故障和测试。面试官通常不会让候选人连续讲 45 分钟，准备长稿的意义是，无论他从哪里打断，你都有下一层内容可讲。

## 一张不依赖 Mermaid 的白板图

面试时先画下面这张简图。画到哪里讲到哪里，不要一次写满。

```text
客户端 A                                                       客户端 B
 HTTP + WebSocket                                               WebSocket + ACK
       │                                                              ▲
       ▼                                                              │
  Gateway-1  ───────────── gRPC ─────────────▶ Logic 集群             │
  A 的本机连接                                      │                  │
       │                                             ├── MySQL         │
       │                                             │   消息事实、关系、会话
       │                                             │
       │                                             ├── Redis         │
       │                                             │   route、seq、pending
       │                                             │       │          │
       │                                             │       ▼          │
       │                                             │   Gateway-3 ─────┘
       │                                             │   B 的本机连接
       │                                             │
       │                                             └── Kafka
       │                                                  │
       │                                                  ▼
       └──────────────────────── 群聊 ─────────────── Transfer
                                                          │
                                                          └── 逐成员写 Redis 投递
```

这张图的 Mermaid 源文件在 [LINKGO_IM_ARCHITECTURE.mmd](LINKGO_IM_ARCHITECTURE.mmd)。GitHub 通常能渲染 Mermaid，但不同 Markdown 阅读器支持不一致，所以正文以纯文本图为准。

## 第一段：两分钟开场

我说：

> 我主要介绍 LinkGo。这是我用 Go 做的一个实时 IM 系统。它有一个浏览器调试客户端，服务端拆成 Gateway、Logic 和 Transfer。Gateway 管 HTTP、WebSocket 和在线连接；Logic 管好友、群成员、消息持久化、幂等和序号；Transfer 专门消费 Kafka，完成群聊的逐成员投递。
>
> 我做这个项目时最关心的不是聊天页面能不能发出一行字，而是把多实例场景想清楚。比如用户 A 连在 Gateway-1，用户 B 连在另一台服务器的 Gateway-3，A 发出的消息怎样找到 B；B 临时离线或者 ACK 丢失，消息怎样补回来；群里有很多人时，为什么不能让 Gateway 同步循环推送；Redis、Logic、Kafka 或 Gateway 出故障后，系统应该拒绝什么、保留什么、恢复后又验证什么。
>
> 当前系统用 MySQL 保存长期事实，用 Redis 保存在线路由、待确认消息和近期回放数据，用 Kafka 承接群聊异步任务。单聊是先落消息事实，再建立接收方 pending，最后通过 Redis 定向通知目标 Gateway。群聊由 Logic 生成一次任务，Transfer 手动消费，失败成员进入 retry 或 DLQ，处理完成后才提交 offset。客户端收到消息后再 ACK，服务端才推进这个用户的 acked_seq。
>
> 我也给项目补了 health、readiness、Prometheus 指标、容器部署和故障演练。这里我要先说明边界：浏览器端是功能完整的调试客户端，不是商业级桌面应用；Redis、MySQL、Kafka 在本地演练里还是单实例；Kubernetes 部分是应用部署清单和静态验证，不是我拥有一个真实生产集群。这些边界我会和已经完成的代码分开讲。

说完先停。面试官如果让你继续，就从“为什么需要三个服务”开始。如果他直接问单聊链路，跳到第四段。

## 第二段：先把客户端和服务端的关系讲明白

我说：

> 这个项目实际使用时，客户端就是用户手里的程序。现在仓库提供的是一个原生 HTML、CSS、JavaScript 页面，浏览器打开它就能作为客户端使用，不需要每台电脑安装我单独打包的 exe。它可以登录、建立 WebSocket、显示好友和群、收发消息、查询历史、发送 ACK、断线重连，也包含红包和 AI 的演示入口。
>
> 如果以后做成真实产品，可以把同一套协议封装进 Windows、macOS、Android 或 iOS 客户端。客户端外形会变，但它与服务端的通信方式不会因此改变：登录、好友、群管理和历史查询走 HTTP；实时消息和 ACK 走 WebSocket。
>
> 我把 HTTP 和 WebSocket 分开，是因为它们解决的问题不同。登录或查询历史是一问一答，HTTP 很合适。实时聊天要求服务端在 B 没有发起新请求时也能主动把 A 的消息推给 B，所以要维持一条双向长连接。WebSocket 本质上从 HTTP Upgrade 开始。Gateway 校验请求后返回 101 Switching Protocols，连接升级成功，后续双方在同一条 TCP 连接上持续交换二进制帧。

这里面试官可能问：“有很多客户端吗？”

你答：

> 从代码实现看，目前只有一种浏览器调试客户端，不是同时维护桌面端、移动端和 Web 端三套代码。从系统运行看，可以同时有很多浏览器实例，每个实例都代表一个用户连接。多客户端数量和客户端种类是两个概念，我不会把一个网页说成已经做了全平台客户端。

面试官可能再问：“这个前端是不是太简单？”

你答：

> UI 确实简单，因为项目重点是服务端消息可靠性。这个页面的价值是能完成协议级黑盒验收，包括登录、WebSocket 建连、消息结果帧、ACK、离线重连和历史查询。若岗位偏前端，它不够；若岗位是 Go 后端，我会重点展示自动化客户端和故障脚本，而不是用页面精美程度证明后端能力。

## 第三段：登录、JWT 和中间件到底怎么走

我说：

> 用户先调用 `POST /api/v1/login`，请求体只有用户名和密码。Gateway 的 `LoginHandler` 解析参数，然后通过 gRPC 把登录请求交给 Logic。Logic 从 MySQL 查询用户，用 bcrypt 比较密码哈希。验证成功以后，Logic 调用 `GenerateToken` 生成 HS256 JWT，Claims 里有 user_id、签发时间和过期时间，当前有效期是 24 小时。最后 Gateway 把 token、user_id 和会话摘要返回给浏览器。
>
> 前端 `login()` 里的 `state.token = payload.token` 只是把服务端返回的 JWT 保存到页面内存，它不是生成 JWT。之后 `apiFetch` 会自动加 `Authorization: Bearer <token>`。建立 WebSocket 时，浏览器端把 token 放到连接 URL 的查询参数里。Gateway 收到后解析签名和过期时间，取得真正的 user_id。
>
> 登录接口本身不能要求用户先有 JWT，否则用户永远无法第一次登录。所以 `/login` 只挂限流中间件。好友、群、历史等受保护接口会先经过 AuthMiddleware，再经过 RateLimitMiddleware，最后才进业务 Handler。认证中间件把解析出的 user_id 放进 Context，业务代码使用这个身份，不相信请求体里随便传来的用户 ID。

如果面试官指着 `NewRateLimitMiddleware(...).Handle` 问为什么没有传 `next`，你说：

> `.Handle` 不是现在就执行请求，而是把一个方法值交给 go-zero。它的类型相当于 `func(next http.HandlerFunc) http.HandlerFunc`。go-zero 注册路由时，会把真正的 LoginHandler 当成 next 传进去。一次请求到达后，限流器先判断是否允许；允许就调用 `next(w, r)`；不允许直接返回 429，后面的登录 Handler 不执行。

如果继续问令牌桶，你说：

> 当前令牌桶是项目内实现。每个 key 有剩余 token 和上次补充时间。请求到来时，根据经过的时间乘补充速率，把 token 补到容量上限；如果至少有一个 token，就扣一个并放行。为了避免所有用户争一把锁，我把 key 哈希到 32 个 shard，每个 shard 有自己的 map 和 mutex。长期不用的 bucket 会按 TTL 清理。它适合单进程自我保护，但多 Gateway 的全局限额要放到统一网关，或者用 Redis Lua 做原子扣减。

## 第四段：先讲为什么要拆 Gateway、Logic、Transfer

我说：

> Gateway 最适合做连接层工作。它维持 WebSocket，本机保存用户到连接对象的映射，处理心跳、帧解析、ACK 和下行写操作。连接对象不能随便搬到别的进程，因为 WebSocket 背后是一条已经建立的 TCP 连接。
>
> Logic 处理可以共享的业务规则。它不关心用户连在哪个 Gateway，而是检查好友或群成员权限，处理 client_msg_id 幂等，分配会话 seq，把消息写入 MySQL，然后调用投递层。多个 Gateway 可以通过 gRPC 调用多个 Logic 实例。服务发现使用 Etcd，连接选择有 p2c_ewma 策略，不是把 Logic 地址写死成一台服务器。
>
> Transfer 只负责群消息任务。群聊的一个发送请求可能对应很多接收人。如果 Logic 在一次 RPC 里等所有成员全部推完，请求耗时会随着成员数增长，某个慢依赖还会占住业务 goroutine。Kafka 把这部分工作变成可积压任务，Transfer 可以独立重启和扩容。

面试官可能问：“为什么单聊不用 Kafka？”

你答：

> 当前单聊追求较短实时路径，一条消息只对应一个目标用户，所以我使用 MySQL 事实、Redis pending 和定向 Pub/Sub。群聊是一条消息对应一批成员，扇出成本更高，更需要异步削峰。两条链路不是谁更高级，而是负载形态不同。若将来单聊吞吐或跨机房可靠性要求更高，也可以引入持久消息日志，但会多出分区、位点、重复消费和延迟成本。

## 第五段：跨服务器单聊完整逐字讲解

这部分最重要。固定使用“A 在 Gateway-1，B 在 Gateway-3”的例子。

我说：

> 先看 B 上线。B 完成 JWT 校验并升级 WebSocket 后，Gateway-3 创建一个 ClientConn，把它放进本机 ClientManager。这个 map 只能被 Gateway-3 自己访问。Gateway-3 同时在 Redis 写一条 B 的 route，route 里不仅有 gateway ID，还有连接 owner 信息并带 TTL。心跳会刷新 TTL。这样共享系统能知道 B 当前在哪个 Gateway，但最终写 WebSocket 的动作仍由真正持有连接的 Gateway-3 完成。
>
> 现在 A 在 Gateway-1 发一条单聊。客户端消息里至少带接收方、会话信息、正文和 client_msg_id。Gateway-1 读到二进制帧，解析 protobuf，先把任务放进按 uid 分片的有界队列，再由 worker 调 Logic 的 `PushMessage`。
>
> Logic 不会直接相信客户端。它先确认发送人身份和好友关系，再检查 client_msg_id 是否已经处理过。没有处理过才继续分配 message_id 和会话 seq。消息正文写入 MySQL，数据库里的 messages 行是长期事实。会话摘要通过 conversation_outbox 做最终一致更新。
>
> 进入投递阶段以后，Logic 调用 RedisDelivery。这里的顺序很重要。它先把完整编码消息写入 B 的 pending_ack、ack_idx 和 retry 相关结构，再读取 B 的 route。假如 route 指向 Gateway-3，就把一个 PushEnvelope 发布到 Gateway-3 专属的 Redis channel。Gateway-3 订阅自己的 channel，收到后会检查 envelope 中的 gateway 和 route owner 是否仍匹配，然后在本机 map 里找到 B 的 ClientConn，把消息写入 B 的 WebSocket。
>
> B 收到消息后，用 message_id 返回 ACK。Gateway-3 根据 message_id 定位待确认消息，通过 Lua 一次性清理 pending、ack index、offline 标记和 retry index，然后把 B 在这个会话里的 acked_seq 单调推进。到这一步，服务端才能说客户端已经确认收到。

### 为什么必须先 pending 后 publish

我说：

> 如果顺序反过来，Logic 先 Publish，B 的网络很快，可能在 pending 建立之前就收到并返回 ACK。这个 ACK 去 Redis 清理时找不到记录，随后 Logic 又把 pending 写进去，结果一条已经确认的消息反而留在未确认集合里，之后会重复发送。先建立 pending，再发通知，就把这个竞态关掉了。

### Redis Pub/Sub 是否合适

我说：

> Pub/Sub 在这里不是消息数据库，只是一条低延迟通知通道。它有一个明显缺点：订阅者离线时消息不会替它保存。因此我不依赖 Pub/Sub 单独保证消息可靠。发布之前已有 pending，正文也在 MySQL。Publish 没有订阅者、route 无效或者 B 离线时，系统留下 offline 标记；B 重连后读取 pending，并用 Redis timeline 和 MySQL cursor 补缺口。
>
> 所以更准确的表述是：Redis Pub/Sub 负责在线用户的跨 Gateway 唤醒，MySQL、pending、ACK 和重连机制负责可靠性。不能说“用了 Pub/Sub，所以消息不会丢”。

### route 为什么不能永久保存

我说：

> Gateway 可能突然断电，来不及主动删除 route。永久 route 会让系统一直把消息发给已经不存在的 Gateway。因此 route 有 TTL，心跳只刷新当前连接 owner 对应的 route。B 重连到 Gateway-2 后，新连接会占有新 route；旧 Gateway 即使稍后执行清理，也只能按 owner 匹配删除，不能误删新连接的路由。这是 route fence 的作用。

## 第六段：四个 ID 和三个游标必须分清

我说：

> 这条消息链路里最容易混淆的是 client_msg_id、message_id、session_id 和 seq。
>
> client_msg_id 由客户端为一次发送意图生成。A 发送以后网络断了，它不知道服务端到底有没有处理，因此重试时必须沿用同一个 client_msg_id。Logic 才能识别这是同一次发送，并返回之前的结果。
>
> message_id 是服务端接受消息后生成的稳定标识。投递、ACK、Redis pending 和日志追踪都围绕它进行。客户端看到两次相同 message_id，就知道那是 at-least-once 重投，不应该展示两条。
>
> session_id 标识会话。A 和 B 的单聊消息属于同一个 session；群消息属于对应群会话。seq 是消息在这个 session 里的递增位置，用来排序、分页和找重连缺口。

### seq 为什么用 Lua

我说：

> 多个 Logic 实例可能同时给同一会话分配序号。如果代码先 GET，再在 Go 中加一，最后 SET，两个实例可能都读到 10，然后都写 11。Lua 在 Redis 内部把读取、加一和写回作为一次原子执行，所以不会分出两个相同 seq。
>
> 这里也有边界。Redis 分到 seq=11 后，MySQL 事务可能失败，那么序号 11 没有真实消息，下一条可能是 12。项目保证 seq 单调且不重复，不承诺完全没有空洞。客户端按真实存在的消息排序，不能因为缺了 11 就永远等待。

### last_msg、last_seq、acked_seq 和 read_seq

我说：

> last_msg 是会话列表要展示的最新一条消息摘要。last_seq 是这个会话已经产生的最新消息位置。acked_seq 是某个会话成员的客户端确认收到哪里。read_seq 是这个成员真正阅读到哪里。
>
> 假设会话已经到 seq=10，B 离线，那么会话的 last_seq 是 10，B 的 acked_seq 可能还是 7。B 上线收到了 8、9、10，但没有打开聊天窗口，这时 acked_seq 可以到 10，read_seq 仍然是 7。因此 last_msg 和 acked_seq 根本不是同一个维度，一个是内容摘要，一个是成员接收进度。
>
> 当前项目已经维护 acked_seq，但没有把它包装成完整的多端已读系统。真实多设备场景还要按 device 保存 cursor，再汇总用户级已读状态。

## 第七段：顺序、背压和重复消息怎么处理

我说：

> Gateway 收消息后不能无限创建 goroutine。流量突然升高时，内存和 goroutine 会先失控。因此当前使用有界队列。队列按 `hash(uid) % shardCount` 分片，同一 uid 始终进入同一个 shard，shard 内按 FIFO 消费，不同用户可以并行。
>
> 队列满时不能只打一行日志，否则客户端会以为请求已经进入系统。Gateway 会返回明确的 SERVER_BUSY，并带回原 client_msg_id。客户端之后用同一个 client_msg_id 重试，Logic 幂等层负责避免重复入库。
>
> uid 分片保证的是一个 Gateway 进程内同一用户的入口顺序。它不等于跨 Gateway、跨 Logic 的绝对执行顺序。最终展示顺序仍由 session seq 决定。
>
> 下行消息采用 at-least-once。ACK 超时后 pending retry 可能再次发送，所以服务端和客户端都必须容忍重复。Exactly-once 不是在文档里写一个词就能获得，它要求业务事实、投递和客户端状态跨故障形成更强事务。当前项目选择允许重复，再用 message_id 去重，这个取舍更现实。

## 第八段：离线以后上线究竟怎么补消息

这是以前最容易回答含糊的地方，要完整说完。

我说：

> B 离线时，A 的消息仍然先写 MySQL。RedisDelivery 也先建立 B 的 pending。随后发现没有有效 route，或者 Publish 没有订阅者，就写 offline 标记。Redis 里的内容有 TTL，它不是永久消息仓库，MySQL messages 才是长期历史。
>
> B 重新建立 WebSocket 时会带 session_id 和它已知的 last_seq。服务端第一步回放 B 的 pending_ack，因为这些消息已经明确进入投递流程但还没有 ACK。第二步读取这个 session 的 Redis timeline，找 `seq > last_seq` 的近期消息。第三步在 Redis 不可用或者热数据不完整时，按同样的 cursor 从 MySQL 分批读取真实消息。回放时按 message_id 去重，写 WebSocket 失败就停止，不能假装后面的消息已经发完。B 收到以后重新 ACK，服务端再清理 pending。
>
> 这比“用户上线时扫描 Redis 里所有消息”更明确，也避免每次登录全表扫描 MySQL。客户端应该按会话携带 cursor，服务端只补这个 cursor 之后的消息。当前单次 MySQL 回放有批量和总量上限，避免一个极老客户端一次连接拖垮 Gateway。

面试官可能问：“刷新网页后 cursor 不就没了吗？”

你答：

> 当前网页的 cursor 主要保存在页面内存，登录返回会话摘要后重新初始化，所以它是演示客户端的限制。生产客户端应把每个账号、设备、会话的 cursor 持久化到本地数据库，并由服务端维护设备级游标。服务端的 MySQL 历史和 pending 兜底已经具备，但多设备 cursor 模型还没有完整实现。

## 第九段：群聊为什么用 Kafka，具体怎样消费

我说：

> 群聊和单聊最大的区别是扇出。一条群消息需要送给多个成员。如果 Gateway 自己循环成员，群越大，占用连接层 goroutine 越久。某次 Redis 抖动还会把整个发送请求卡住。Kafka 的作用不是“防止程序死锁”，而是把耗时的逐成员投递从同步请求中拆出来，让任务能够积压、重试和独立扩容。
>
> 当前流程是：Gateway 把群消息交给 Logic；Logic 校验发送人是不是群成员，生成消息和 seq，查询当前 recipients，构造 group dispatch job 写入 Kafka。这里我要承认，Logic 目前仍同步解析 recipients，因此发送前还有 O(N) 成本，只是实际逐成员投递已经从请求路径移到 Transfer。
>
> Transfer 使用 FetchMessage，不会一读到消息就自动提交。它先反序列化任务，把群消息记入 timeline，然后逐个 recipient 投递。每个成员有 processing owner 和 lease。没有处理过的成员可以 claim；处理成功后标 done；某个 Transfer 崩溃后，lease 到期，另一个实例可以接手。已经 done 的成员在重复消费时跳过。
>
> 如果成员投递失败，任务增加 attempt 并写 retry topic。超过最大尝试次数后写 DLQ。只有本次任务已经投递成功，或者失败信息已经可靠进入 retry 或 DLQ，Transfer 才 CommitMessages。提交前崩溃，Kafka 会重新给出任务；提交后崩溃，前面的处理结果已经有可靠去向。

面试官可能问：“失败一个成员，其他成员怎么办？”

你答：

> 当前实现按 recipients 顺序处理，遇到失败成员会停止本轮并把任务写 retry。之前已经 done 的成员有幂等状态，下一次消费会跳过，所以不会从业务上重复完成；失败成员和后续成员继续处理。这不是最高吞吐模型，但恢复语义容易解释和验证。

面试官可能问：“Transfer 扩容就一定更快吗？”

你答：

> 不一定。消费者并行度受 Kafka 分区数限制，某个超大群任务内部当前还是顺序投递。扩容 Transfer 主要提升不同分区和不同任务的并发。超级大群可以进一步改成分片 fan-out 任务，或者采用共享消息加成员拉取模型，但当前没有实现，所以只作为演进方向。

## 第十段：MySQL、Redis、Kafka 分别承担什么

我说：

> 我不是按“这个技术常见”来选择数据库，而是按数据寿命和访问方式分工。
>
> MySQL 保存需要长期查询和约束的事实，包括 users、friendships、groups、group_members、messages、conversations、conversation_members、红包记录和 AI 记录。好友关系、群成员权限和消息历史需要事务或唯一约束，适合关系数据库。
>
> Redis 保存变化快、访问频繁、允许通过长期事实恢复的状态，包括在线 route、会话 seq、pending ACK、ack index、retry index、offline 标记和近期 timeline。它让在线路径不需要每次都扫描 MySQL，但 Redis 不是唯一事实源。
>
> Kafka 保存需要异步消费的群投递任务。它通过分区和 offset 表达消费进度，Transfer 可以在失败后重新处理。Kafka 不适合拿来直接替代用户、好友和历史查询表。

如果面试官问 MongoDB，你说：

> MongoDB 适合文档结构经常变化、希望把复杂消息体整体保存、水平扩展文档读写的场景。当前项目的核心查询围绕明确的关系、成员权限、会话序号和唯一约束，MySQL 已经能覆盖，而且消息正文 protobuf 也可以作为字段保存。增加 MongoDB 会带来新的部署、一致性、索引和备份成本，却没有解决当前最主要的问题，所以我没有为了技术栈数量加入它。只有在消息类型高度动态、文档查询成为主要负载，并且压测证明现有模型出现明确瓶颈时，我才会评估迁移或冷热分层。

## 第十一段：Outbox 解决什么，不解决什么

我说：

> 写一条消息时，messages 表和会话列表摘要不是完全相同的事实。如果正文已经提交，但更新 conversations 的 last_msg 失败，用户刷新会话列表可能暂时看不到最新摘要。
>
> 当前消息事务同时写 conversation_outbox。后台 worker 读取 outbox，再把会话摘要更新到 MySQL 和 Redis，失败可以重试。这样正文提交后，摘要最终能够追上。
>
> Outbox 不代表消息已经送达，也不代表 MySQL 和 Redis 发生了分布式事务。它只把“正文已提交以后还需要更新摘要”这件事变成可重试事件。送达仍由 pending 和 ACK 判断。

## 第十二段：故障先行，逐个服务怎么坏、怎么恢复

我说：

> 我处理故障时先区分 liveness 和 readiness。liveness 只回答进程是否还活着，不能因为 Redis 短暂抖动就疯狂重启。readiness 回答实例现在是否适合接收新流量。依赖异常时先让 readiness 失败，把实例从新流量中摘除，再重启或替换，恢复后必须跑业务 smoke，不能只看容器状态变成 Running。

### Gateway 挂了

> Gateway 挂掉后，原 WebSocket 不可能平移到新机器，TCP 连接已经断了。客户端指数退避重连，负载均衡把新连接分到其他 Gateway。新 Gateway ClaimRoute，旧 route 由 owner fence 和 TTL 清理。未 ACK 消息仍在 Redis pending，重连时回放。

### Logic 挂了

> Gateway 调 Logic 会失败。Gateway 的 readiness 不只检查端口，还调用 gRPC Health Check。服务发现和连接池可以选择其他健康 Logic。客户端没有收到接受结果时，保持 client_msg_id 重试。若第一次请求其实已经写入 MySQL，幂等查询会返回原 message_id，不再插入第二条。

### Redis 挂了

> Redis 承担 route、pending 和实时通知。此时继续返回“发送成功”会产生无法解释的送达状态，所以当前路径选择 fail-closed。Logic 的依赖感知 health 会报告 NOT_SERVING，Gateway readiness 随之失败。Redis 恢复后重新建立连接和 route，再跑登录、单聊、ACK、离线回放。

### Kafka 挂了

> 新群聊任务写不进去时，Logic 不能假装群任务已经可靠接受。Transfer 读写或提交失败时会重试 Kafka 操作，不提前提交 offset。Kafka 恢复后，未提交任务再次消费，成员 lease 和 done 状态承担重复。

### Transfer 挂了

> Gateway 和 Logic 仍可提供不依赖 Transfer 的接口，但群任务会在 Kafka 积压。Transfer 恢复后从 consumer group offset 继续。监控重点是 Kafka lag、retry、DLQ 和消费错误，而不是只看进程是否存在。

### MySQL 挂了

> 权限校验、登录和消息事实无法可靠写入，所以相关业务应该失败，不能只把消息留在 Redis 后返回成功。Logic readiness 检查 MySQL。恢复后验证登录、关系权限、消息入库和历史查询。

## 第十三段：Kubernetes 解决什么，不解决什么

我说：

> Kubernetes 管理的是应用实例生命周期。Deployment 可以维持副本数、滚动替换；readiness 决定 Pod 是否进入 Service Endpoint；liveness 处理进程卡死；preStop 和 termination grace period 给 Gateway 一点清理连接和 route 的时间；PDB 限制维护时同时中断的实例数量；HPA 可以根据指标增加副本。
>
> 新用户连接可以被负载均衡分配给新增 Gateway，但已经存在的 WebSocket 不会为了均衡而迁移。如果十台 Gateway 都快满了，先扩容并让新连接进入新 Pod。是否主动让旧客户端分批重连，需要额外的连接排空策略，不能粗暴杀掉全部旧连接。
>
> 当前仓库的 K8s 清单能渲染和静态检查，但没有证明我部署了 Redis Cluster、MySQL 主从或 Kafka 多副本。应用使用的是这些中间件的稳定连接入口。真实公司环境中，选主、复制、备份和故障切换一般由中间件平台或托管服务负责，应用还要配合超时、重连、幂等和 readiness。

## 第十四段：测试到底测试了什么

我说：

> 我的测试分四层。第一层是单元和组件测试，覆盖 JWT、令牌桶、消息幂等、队列背压、Redis route owner、ACK Lua、离线回放、Transfer lease 和提交边界。第二层是 `go test -race ./...`，用带 GCC 的 Go 环境检查并发读写。第三层是 `tools/core_im_demo` 黑盒客户端，它不调用内部函数，而是像真实用户一样登录、建立 WebSocket、发单聊、收消息、发 ACK、检查 pending 清理，再验证离线重连、AI 回复、Kafka 群聊和 MySQL 持久化。第四层是故障注入脚本，在完整 Compose 中实际停止 Redis、Logic、Kafka、Transfer、MySQL 和一个 Gateway，再恢复并重新运行 smoke。
>
> 验收故障不能只看 docker ps。脚本先看故障期间 readyz 是否正确失败，再看恢复以后 readyz 是否回来，最后执行真实登录、单聊、ACK、离线和群聊链路。这样才能区分“进程启动了”和“系统业务恢复了”。
>
> 我不会声称当前已经证明十万连接或固定 P99，因为仓库没有一份对应环境、负载模型和原始数据都完整的压测报告。现有证据能证明功能、并发安全边界和单实例依赖故障恢复。大规模容量需要单独制定连接数、消息大小、在线率、群大小和硬件规格后再压测。

面试官可能问：“前端页面点几下算测试吗？”

你答：

> 页面手测只能做探索和演示，不能作为主要回归证据。真正可重复的是 Go 测试、race、自动化 WebSocket 黑盒客户端和故障脚本。页面的作用是让人直观看到协议行为，不替代自动化断言。

## 第十五段：安全设计怎么讲

我说：

> 密码不明文保存，新数据使用 bcrypt。JWT 解决“你是谁”，资源权限解决“你能不能访问这个会话”，两者不能混为一谈。历史查询会检查当前用户是不是会话成员，群消息会检查发送人是不是群成员，单聊会检查好友关系。
>
> 浏览器发起 WebSocket 时还要校验 Origin。JWT 有效只能证明请求带着合法身份，不能阻止另一个恶意网页诱导用户浏览器连接，因此 Origin 白名单仍然需要。
>
> 登录按 IP 限流，认证以后按用户限流。当前是每 Gateway 本地令牌桶，因此它是实例保护，不是全局精确配额。JWT Secret、数据库密码和 AI Key 在生产清单中通过 Secret 注入，日志不应该输出完整 Token 和密码。

## 第十六段：红包和 AI 只讲两分钟

我说：

> 红包和 AI 是在 IM 主链路上的业务扩展。红包领取使用 MySQL 事务和行锁检查剩余状态，唯一约束阻止同一用户重复领取。它现在是等额账本演示，没有钱包扣款、退款、资金流水和对账，所以我不会把它说成支付系统。
>
> AI Bot 被当成特殊好友。用户消息进入 Logic 后调用 provider，默认可以使用 mock，知识库是轻量文本召回，回复再走普通消息投递和 ACK。当前 AI 任务是进程内 goroutine，不是持久任务队列，因此外部模型长时间超时和进程重启后的任务恢复还没有生产化。这部分只证明 IM 可以接业务扩展，不是项目主卖点。

## 第十七段：面试官连续追问模拟

### 追问一：“投递成功，但 Kafka offset 提交前宕机，会不会重复？”

我答：

> 会重新消费，这是预期行为。Transfer 不是靠“不重复消费”保证正确，而是靠每个 message_id 和 recipient 的状态。投递前先 claim processing lease，完成后标 done。重新消费看到 done 就跳过；看到未过期 processing 会等待；lease 过期后可以接管。因此整体是 at-least-once 加业务幂等。

### 追问二：“消息写 MySQL 成功，Redis pending 失败怎么办？”

我答：

> 这时不能返回已可靠送达。MySQL 中已经有消息事实，客户端使用同一 client_msg_id 重试时会命中原消息，服务端再尝试建立投递状态。长期还可以把投递动作也做成可靠 outbox，由后台任务补偿。当前会话摘要已有 outbox，但消息投递不是完整的事务消息系统，所以我要把这个作为边界说清楚。

### 追问三：“B 收到消息后 ACK 丢了怎么办？”

我答：

> pending 仍然存在，重试 worker 或重连回放会再次发送。B 按 message_id 去重，再次 ACK。服务端 ACK Lua 清理是幂等的，acked_seq 只做单调推进，不会因为旧 ACK 把游标倒退。

### 追问四：“两个 Gateway 同时认为自己持有 B 怎么办？”

我答：

> 当前 route 是带 owner 的单值模型，新连接 ClaimRoute 后成为当前 owner。通知 envelope 也带 routeValue，Gateway 写本机连接前再次核对，旧 owner 的刷新和删除不能覆盖新 owner。它解决单连接替换竞态，但不是完整同账号多设备。多设备需要 route 变成 user 到多个 device connection 的集合，并分别维护 ACK cursor。

### 追问五：“为什么 Redis 数据过期不会把离线消息弄丢？”

我答：

> Redis pending 和 timeline 用于近期快速恢复，长期正文已在 MySQL。Redis 数据过期以后不能再靠快速索引回放，但可以根据 session_id 和 last_seq 从 MySQL 查询真实消息。前提是客户端或服务端保存可靠 cursor，这也是生产多设备同步要继续完善的地方。

### 追问六：“怎么扩容？”

我答：

> Gateway 扩容后，新连接经负载均衡进入新实例，现有连接保持原位。Logic 是无连接业务实例，通过 Etcd 服务发现和 gRPC 连接池扩容。Transfer 加消费者实例，但并行度受 Kafka 分区数约束。MySQL、Redis 和 Kafka 不是简单多启动几个应用容器，分别需要主从或集群拓扑、稳定入口、数据复制和故障切换，这部分由基础设施方案负责，应用侧保证幂等、重连和降级。

### 追问七：“你这个项目最值得讲的取舍是什么？”

我答：

> 我认为是把实时通知和可靠事实分开。Redis Pub/Sub 快，但会丢通知，所以只用来找到在线 Gateway；消息正文放 MySQL，未确认状态放 pending，客户端用 ACK 闭环。群聊的高扇出又是另一种负载，所以交给 Kafka 和 Transfer。这个拆分让我能明确回答每个组件挂掉以后还剩下什么，而不是把“用了中间件”当成可靠性结论。

## 第十八段：最后一分钟收束

我说：

> 总结一下，LinkGo 的核心是一条可以解释故障的消息链路。A 和 B 在不同 Gateway 时，通过 Redis route 找到目标节点；单聊在实时 Pub/Sub 之前先建立 pending，并用 MySQL 保存长期消息；客户端 ACK 后才推进成员 acked_seq；离线重连先补 pending，再用 timeline 和 MySQL cursor 补缺口；群聊通过 Kafka 和 Transfer 把成员扇出移出同步请求，并用手动提交、retry、DLQ 和 recipient lease 承受重复消费。
>
> 我已经用单元测试、race、自动化 WebSocket 客户端和 Compose 故障注入验证这些路径。当前还没有完整多设备模型、中间件生产 HA 和超级大群分片，我能区分这些演进方案和已经完成的代码。这个项目让我真正掌握的不是某一个 API，而是怎样定义消息接受、投递、确认和故障恢复的边界。

到这里停止。不要自己再追加一串技术名词，等待面试官追问。

## 附录一：代码阅读定位

| 你要证明的内容 | 代码位置 | 至少看到什么程度 |
| --- | --- | --- |
| 路由和中间件顺序 | `cmd/gateway/internal/handler/routes.go` | 能指出公共登录路由与认证路由怎样组合 |
| 登录转发 | `cmd/gateway/internal/handler/loginhandler.go` | 能说出 HTTP 参数怎样进入 Logic gRPC |
| JWT | `internal/middleware/auth.go` | 能说出 GenerateToken、ParseToken、Claims 和过期时间 |
| REST 认证 | `cmd/gateway/internal/middleware/authmiddleware.go` | 能解释 next 和 Context user_id |
| 令牌桶 | `internal/middleware/ratelimit.go` | 能解释补 token、扣 token、shard mutex 和清理 |
| WebSocket 连接 | `internal/server/client.go` | 能找到读循环、心跳、ACK 和结果帧 |
| 本机连接和 route | `internal/server/manager.go` | 能区分本机 map 与 Redis route |
| uid 分片队列 | `internal/server/pool.go` | 能解释有界、FIFO、不同 uid 并行和满载返回 |
| Redis 投递 | `internal/delivery/redis.go` | 能说明 pending 先于 Publish、定向 channel 和离线 fallback |
| ACK | `internal/server/ack.go` | 能说明 Lua 原子清理和 acked_seq 单调推进 |
| 重连回放 | `internal/server/sync.go` | 能按 pending、timeline、MySQL cursor 顺序讲 |
| Logic 消息入口 | `internal/logic/handler.go` | 能找到权限、client_msg_id、seq、消息事务和投递 |
| 会话与 Outbox | `internal/logic/conversation.go` | 能区分消息事实和会话摘要最终一致 |
| Kafka 消费 | `cmd/transfer/main.go` | 能讲 Fetch、recipient lease、retry、DLQ 和 Commit |
| Logic 健康 | `cmd/logic/health.go` | 能区分 HTTP health 和依赖感知 gRPC Health |
| Gateway 检查 Logic | `cmd/gateway/internal/svc/logicrouter.go` | 能说明连接状态与 gRPC Health Check |
| 自动化业务客户端 | `tools/core_im_demo/main.go` | 能指出登录、WebSocket、ACK、离线和群聊断言 |
| 故障演练 | `scripts/fault_injection.sh` | 能说出每个故障的停止、摘流量、恢复和 smoke |

## 附录二：怎样背这份稿

第一天只练第一到第五段。目标不是逐字一致，而是不看稿画出客户端、两个 Gateway、Logic、Redis 和 MySQL，并完整讲完跨节点单聊。

第二天练第六到第十一段。每个名词都用“它解决了哪一种失败”解释。如果只能背定义，回到代码位置看读写的 key、表和字段。

第三天练故障和测试。随机停止一个组件，按照“怎样发现、怎样摘流量、什么状态还在、怎样恢复、用什么业务验证”五步回答。

第四天让别人从第十七段随机追问。每个回答控制在一分钟，结构固定为：先给结论，再讲当前实现，然后讲失败恢复，最后说边界。

你不需要一字不差地背，但下面五条不能说错：

1. Pub/Sub 是在线通知，不是可靠存储。
2. ACK 表示客户端收到，不等于用户已读。
3. seq 保证单调且不重复，允许事务失败造成空洞。
4. Gateway 故障后连接要重建，不会迁移现有 TCP。
5. 本地故障演练不等于中间件生产集群已经实现。
