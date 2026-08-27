# 08 项目介绍、技术选型与高频追问

## 1. 怎样用两分钟专业地介绍 LinkGo？

### 面试官问

请介绍一下你最主要的 Go 项目，重点说清架构和你解决的问题。

### 30 秒回答

LinkGo 是我用 Go 和 go-zero 实现的 Client-Server 即时通信系统，核心服务按 Gateway、Logic、Transfer 分层。Gateway 管理 HTTP、WebSocket 和本机长连接，Logic 负责鉴权、关系校验、发送幂等、会话序号和 MySQL 持久化，Transfer 通过 Kafka 异步完成群消息逐成员投递。项目重点不是聊天页面，而是多 Gateway 场景中的用户路由、ACK 与离线补偿、群聊高扇出、故障恢复和有界背压。

### 详细回答

我做这个项目时，先把场景定义成 A 和 B 可能连接不同 Gateway，而 Logic 也以集群方式提供服务。每台 Gateway 只持有本机 WebSocket，Redis 保存 `uid -> gatewayID|connectionID` 的共享在线路由。Gateway 通过 Etcd 发现 Logic，gRPC 客户端使用 `p2c_ewma` 在可用连接中选择实例。

单聊发送时，Gateway 按发送者 uid 放入有界分片队列，Logic 使用已认证 uid 校验好友关系，通过 `client_msg_id` 和数据库唯一索引处理重试幂等，用 Redis Lua 分配会话 `seq`，再把消息写入 MySQL。消息事实存在后，投递层先为接收者登记 pending，再查 Redis route，向目标 Gateway 的专属 Pub/Sub 频道发布。目标 Gateway 找到本机连接并写 WebSocket，客户端按 `message_id` 返回 ACK 后才清 pending。

群聊的负载形态不同。一条消息会扇出到多个接收者，所以 Logic 落库并取得成员后，把任务写 Kafka，Transfer 再逐成员投递。消费者采用手动提交，成员处理有 lease、retry 和 DLQ。这样发送请求不必同步等待所有成员，但当前 Logic 仍会同步读取完整成员列表，我不会说发送成本与群规模完全无关。

我还给项目加入了 readiness、Prometheus 指标、容器和 Kubernetes 清单，以及单元测试、race、协议黑盒 demo 和本地故障脚本。边界也很明确：当前前端是浏览器调试客户端，数据库和消息中间件的本地环境是单节点，Kubernetes 是可渲染部署设计和 CI 校验，不是实际生产集群经历。

### 结合 LinkGo

介绍时记住六个代码入口即可：

- 连接接入：`cmd/gateway/internal/handler/websockethandler.go`
- 连接读循环与队列：`internal/server/client.go`、`internal/server/pool.go`
- 核心消息业务：`internal/logic/handler.go::PushMessage`
- Redis 路由与投递：`internal/server/route.go`、`internal/delivery/redis.go`
- 群聊消费：`cmd/transfer/main.go`、`cmd/transfer/recipient_lease.go`
- 协议契约：`api/protocol.proto`

### 为什么这样选，替代方案是什么

项目可以写成一个单体，开发和部署都会更简单。我拆出三类进程，是因为连接接入、核心业务和群聊扇出的资源模型不同。Gateway 受长连接数和网络写影响，Logic 受业务 RPC、MySQL 和 Redis 延迟影响，Transfer 受 Kafka 积压和群成员数量影响。拆开后能分别扩容和隔离故障，代价是多了 gRPC、服务发现和跨进程失败窗口。

### 边界与追问

不要把它称为经历真实生产流量的商业 IM。更准确的定位是：面向多节点部署设计，并在本地多进程、真实协议和故障脚本中验证核心链路的 Go IM 项目。

## 2. 技术选型时怎样体现自己的判断？

### 面试官问

你用了很多中间件，怎样证明不是为了堆技术栈？

### 30 秒回答

我的原则是先说业务约束，再比较最直接方案的缺点，最后说明当前选择解决了什么和新增了什么成本。Redis 用于高频短期状态和在线通知，MySQL 保存长期事实，Kafka只承担群聊高扇出任务，Etcd只做 Logic 服务发现。这些职责不重叠，也没有因为消息量大就额外加入 MongoDB。

### 详细回答

选型不能只回答“性能高”。以四个组件为例。

MySQL 负责用户、关系、消息和会话事实，因为这些数据需要事务、唯一约束和按会话查询。Redis 负责在线 route、短期 pending、ACK 索引和 seq，因为这些操作频繁、生命周期短，而且需要 Lua 原子比较修改。Redis Pub/Sub 只做目标 Gateway 的低延迟通知，不能替代消息历史。

Kafka 出现在群聊，不是因为所有消息都必须上消息队列。单聊只有一个主要接收者，直接定向路径更短。群聊会同步放大逐成员投递时间，Kafka 让这部分形成可积压、可重试、可由 Transfer 独立扩容的任务。

Etcd 管理的是 Logic 实例列表，Redis route 管理的是用户连接位置。一个是服务发现，一个是业务路由。即使它们都能保存 Key，也不能因此说职责相同。

MongoDB 没有加入，是因为当前消息结构稳定，MySQL 已经满足历史、关系、事务和索引需求。多一个数据库会增加双写、一致性、备份和学习成本。只有当消息规模、冷热分层或文档结构变化真的出现瓶颈时，才应该用数据重新评估。

### 结合 LinkGo

面试时可以使用固定回答顺序：

```text
业务约束是什么
→ 最直接方案会出现什么问题
→ 当前技术解决哪一段
→ 为什么没有选择最近的替代方案
→ 新方案带来什么故障和运维成本
```

例如 Kafka 的准确理由是隔离群聊高扇出，不是“防止 Go 程序死锁”；Redis Pub/Sub 的准确职责是在线通知，不是可靠保存消息。

### 为什么这样选，替代方案是什么

单聊通知可以换成 Gateway 间内部 RPC，群任务可以换成 Redis Streams 或 RabbitMQ，Logic 服务发现也可以依赖 Kubernetes Service。没有一种方案在所有维度更好。选择要围绕延迟、持久性、扇出、运维能力和团队已有基础设施做取舍。

### 边界与追问

项目当前确实依赖较多组件，所以面试时必须能逐个说出“去掉它会怎样”。如果某个组件只剩框架名，讲不出数据结构、失败语义和替代方案，就不应把它放在简历核心句中。

## 3. 简历项目描述怎样写得专业而不过度？

### 面试官问

给出可以直接放简历的项目描述，并说明每句话的证据。

### 30 秒回答

简历先写 Client-Server 与三层服务架构，再写单聊可靠性和跨 Gateway 路由，最后写 Kafka 群聊与测试证据。专业感来自职责、数据语义和故障边界，不来自堆框架名。我不会写生产级、消息绝不丢、WebSocket 无损迁移或真实线上 QPS。

### 详细回答

项目名称可以写：

```text
LinkGo 分布式即时通信系统
```

项目简介可以写：

```text
基于 Go/go-zero 实现面向多节点部署的 Client-Server IM 系统，客户端通过 HTTP 完成登录与关系操作，通过 WebSocket 维持实时双向连接；服务端按 Gateway 接入层、Logic 业务层和 Transfer 异步投递层拆分。
```

正文建议保留三条：

```text
设计 Gateway、Logic、Transfer 分层架构，使用 gRPC 与 Etcd 服务发现连接接入层和业务层，并以 Redis 在线路由及定向 Pub/Sub 完成多 Gateway 场景下的实时投递。

围绕消息可靠性实现 client_msg_id 幂等、Redis Lua 会话序号、MySQL 消息持久化、pending ACK 与单会话重连补偿；Gateway 使用按 UID 分片的有界队列维护同发送者入口顺序并提供背压。

使用 Kafka 解耦群消息与逐成员扩散，Transfer 通过手动提交 offset、成员级 lease、retry 和 DLQ 处理失败与重复消费；使用单元测试、race、协议黑盒 demo 及本地故障脚本验证关键路径。
```

如果版面只能放两条，就合并架构和可靠性，保留群聊与验证。`p2c_ewma`、具体 Redis Key、Lua 条件删除等内容留给追问，不要在三行简历中塞满所有实现细节。

### 结合 LinkGo

第一条对应 Gateway、Logic、Transfer 三个入口和 `logicrouter.go`。第二条对应 `pool.go`、核心 `PushMessage`、`redis.go`、ACK 与 sync 代码。第三条对应 Kafka dispatcher、Transfer consumer、测试和 CI workflow。

写完每一句都要能回答四件事：对应哪个文件，解决什么失败，测试覆盖到哪里，当前还有什么缺口。

### 为什么这样选，替代方案是什么

另一种简历写法是列 Go、Redis、MySQL、Kafka、K8s 十几个名词。这种写法占空间，却没有说明你做了什么。以“设计了什么机制、解决什么问题、用什么证据验证”为主语，更容易把面试引到你掌握的调用链。

### 边界与追问

不要写当前 commit 没有重新验证的连接数、QPS 和 P99。历史压测报告不能自动证明最新版本容量。Kubernetes 只写“提供应用部署清单并在 CI 渲染校验”，不要写“完成生产自动发布”。

## 4. 项目里最有挑战的部分是什么？

### 面试官问

这个项目最难的地方在哪里，你是怎样分析和解决的？

### 30 秒回答

最难的不是 WebSocket API，而是把“消息成功”拆成多个阶段，并处理阶段之间的故障窗口。我把 MySQL 落库、在线通知、WebSocket 写入和客户端 ACK 分开定义，再用 client_msg_id、message_id、seq、pending 和游标分别解决发送重试、接收去重、会话排序和断线恢复。这样出故障时能准确说清保住了什么，而不是笼统承诺不丢消息。

### 详细回答

最初很容易把 `WriteMessage` 成功理解成对方收到，但它只说明本机库完成写操作。对方页面可能断网、崩溃或 ACK 丢失。因此项目先把消息事实写入 MySQL，投递前登记接收者 pending，客户端处理后再 ACK。

引入重试后又会出现重复，所以发送请求用 `client_msg_id` 幂等，接收消息用 `message_id` 去重。多个 Logic 并发处理同一会话时，客户端时间不可靠，因此使用共享的会话 `seq` 排序。重连不能把所有历史永久放 Redis，也不能上线就全表扫描，所以采用短期 pending 和 timeline 优先，再对客户端明确提供的单个会话 cursor 从 MySQL 补齐。

真正的难点是跨存储边界。MySQL 提交和 Redis 通知不是一笔事务，Kafka offset 与逐成员投递也不是一笔事务。我没有用一句“最终一致”掩盖，而是逐点分析崩溃位置，依靠幂等、lease、重试和客户端恢复降低影响，并明确当前接收方持久 Outbox、多设备游标等能力尚未完整实现。

### 结合 LinkGo

这部分可以从 `PushMessage` 一直讲到 `RedisDelivery.Deliver`、Gateway Pub/Sub subscriber、`AckMessageWithDB` 和 `SyncOfflineMessagesWithDB`。群聊则从 Kafka `FetchMessage` 讲到成员 claim、retry/DLQ 和 `CommitMessages`。

### 为什么这样选，替代方案是什么

可以用更重的事务消息、完整消息投递 Outbox 或统一日志架构，把跨存储窗口进一步收紧。但每种方案都会增加数据模型、后台任务和运维成本。当前版本优先实现能够定位、测试和解释的机制，同时把未闭环部分保留为演进方向。

### 边界与追问

不要回答“最难的是环境配置”或“Kafka 搭建”。配置确实花时间，但不能体现核心工程判断。也不要声称已经 exactly-once；当前更接近至少一次方向、允许重复并依靠幂等减小副作用。

## 5. 如果面试官质疑代码由 AI 生成，怎样证明代码所有权？

### 面试官问

这个项目功能很多，你真的掌握核心代码吗？

### 30 秒回答

我使用过 AI 辅助检索、生成测试骨架和检查故障场景，但能力以我能否解释和修改代码为准。我能从 HTTP 登录、WebSocket 建连、跨 Gateway 单聊、ACK 重连和 Kafka 群聊五条链路定位到具体函数，能说出关键结构体字段和故障窗口，也会用测试结果限制简历表述。AI 生成过什么不重要，最终的设计判断和验证责任由我承担。

### 详细回答

代码所有权至少有四层。

第一层是调用链。能从客户端入口连续讲到 Gateway、gRPC、Logic、MySQL、Redis、目标 Gateway 和 ACK，不在进程边界处断掉。

第二层是数据结构。能解释 `ClientConn.ConnectionID`、`WireMessage.client_msg_id`、`message_id`、`session_id`、`seq`，以及 Redis route、pending 和群成员 lease 的作用。

第三层是失败路径。能分析 Gateway、Logic、Redis、MySQL、Kafka 和 Transfer 分别挂掉后，哪些状态还在，谁负责重试，哪里仍有缺口。

第四层是验证。能说明单元测试、race、core demo、故障脚本和 K8s 静态校验各自证明什么，也能承认没有 Playwright、真实生产集群和当前版本大规模压测。

如果现场要求修改，我会先说清不变量。例如把单值 route 改成多设备时，不能只把 Redis String 换成 Hash，还要同时修改本机连接表、ACK 游标、重连协议、route 清理、推送扇出和测试。

### 结合 LinkGo

面试前至少能脱离文档说出这些符号：

- `WebSocketHandler`、`StartClientLoopWithDB`
- `ClientConn`、`ClientManager`、`PushWorkerPool`
- `LogicHandler.PushMessage`
- `RedisDelivery.Deliver`
- `AckMessageWithDB`、`SyncOfflineMessagesWithDB`
- `LogicRouterPool.GetClient`
- `claimGroupRecipient`、`completeGroupRecipient`、`releaseGroupRecipient`

### 为什么这样选，替代方案是什么

只背架构图容易在结构体和错误分支上断掉，只逐行背代码又无法表达设计。更有效的方法是围绕一条业务链同时记入口、状态、失败和证据，再从这条链向相邻模块扩展。

### 边界与追问

不要用“所有代码都逐行手写”回避 AI 辅助问题。可信回答是明确工具参与范围，并证明自己能解释、运行、修改和约束最终结果。

## 6. 红包和 AI Bot 在这个项目里怎样定位？

### 面试官问

项目名称里还有红包和 AI，这些是核心吗？

### 30 秒回答

它们是辅助业务，不是主架构。红包用于补充 MySQL 事务、行锁、资金流水和领取幂等；AI Bot 用 provider 接口演示普通消息链路怎样扩展外部能力。我的简历主线仍是 Go IM 的连接、消息可靠性和群聊投递，AI 工程能力由独立 CodeRepair 项目承担，不会把几个 Bot 接口包装成 AI 平台。

### 详细回答

红包最值得讲的是并发一致性，而不是页面按钮。创建红包时先从钱包扣款并记录流水，领取时在数据库事务里锁定红包行，检查状态、剩余份数和重复领取，再写领取记录和钱包入账。唯一索引负责并发下的幂等兜底。它能证明对事务、锁和业务不变量的理解，但与 IM 核心链路相比不是第一优先级。

AI Bot 的价值是展示外部模型调用不应阻塞普通消息热路径，以及 provider 接口怎样隔离具体厂商。它不包含训练、微调、复杂 Agent 状态机或可靠任务队列，Logic 崩溃时进程内异步任务也可能丢失。因此 AI 岗面试应主讲独立的 DeerFlow 二次开发项目。

### 结合 LinkGo

红包核心在 `internal/logic/redpacket.go` 和钱包相关 SQL；AI Bot 核心在 `internal/logic/bot.go` 与 `internal/ai`。两部分都复用用户、关系和消息基础设施，但不会改变 Gateway、Logic、Transfer 的主链定位。

### 为什么这样选，替代方案是什么

简历空间有限时完全可以不写红包和 Bot。项目介绍聚焦一条能讲深的可靠消息链，比罗列十个功能更有说服力。面试官主动问业务扩展时，再用红包讲事务，用 Bot 讲外部依赖隔离。

### 边界与追问

红包是模拟钱包，不是真实支付系统；AI Bot 是消息扩展，不是 Coding Agent。也不要把任何“游戏运营”能力写进项目，这个仓库的稳定定位就是 IM 服务端。
