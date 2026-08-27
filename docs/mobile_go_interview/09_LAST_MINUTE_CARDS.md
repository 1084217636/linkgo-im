# 09 上场前快速复述卡

这一章不是关键词清单。每张卡都给出可以完整说出口的答案。面试前一晚或通勤途中，从问题开始复述；卡住时直接看详细回答，不需要再查其他文档。

## 1. 三十秒画出系统拓扑

### 面试官问

不用展开细节，先说清客户端和三个服务怎样连接。

### 30 秒回答

客户端通过 HTTP 调 Gateway 完成登录、好友、群组和历史操作，通过 WebSocket 与 Gateway 保持实时连接。Gateway 通过 gRPC 调 Logic，Logic 使用 MySQL 保存长期事实、Redis 保存在线路由与短期补偿状态。单聊由 Redis 定向通知目标 Gateway；群聊任务写 Kafka，由 Transfer 消费后逐成员调用同一投递层。Etcd只负责 Logic 实例发现。

### 详细回答

白板只画这条线：

```text
Client A → Gateway-1 → gRPC → Logic → MySQL
                         │        ├→ Redis route/pending → Gateway-3 → Client B
                         │        └→ Kafka → Transfer → Redis delivery
                         └→ Etcd resolver 发现 Logic 实例
```

Client 是不可信输入端。Gateway 是有状态连接层，因为 socket 在进程内。Logic 尽量无用户粘性，业务事实来自共享存储。Transfer 是群聊异步执行层，不直接面对客户端。

### 结合 LinkGo

三个可执行入口是 `cmd/gateway`、`cmd/logic`、`cmd/transfer`。当前客户端只有 `public/index.html` 这一种浏览器调试实现，可以同时打开多个实例，但不是已经完成桌面端和移动端。

### 为什么这样选，替代方案是什么

三层是按负载形态拆分，不是为了追求服务数量。单体适合更早期版本；当前拆分让连接、业务和群扇出分别扩容与定位故障。

### 边界与追问

Gateway 中仍有部分好友、群、红包等 HTTP 业务直接访问数据库，所以只能说核心消息链路按三层拆分，不能说 Gateway 完全没有业务逻辑。

## 2. 六十秒讲完跨 Gateway 单聊

### 面试官问

A 在 Gateway-1，B 在 Gateway-3，一条消息怎样送达？

### 30 秒回答

A 的 WebSocket 消息进入 Gateway-1 的 uid 分片队列，再通过 gRPC 调健康 Logic。Logic 验证身份和好友权限，处理 client_msg_id 幂等，生成会话 seq 并写 MySQL；随后先登记 B 的 pending，查到 `route:B`，向 Gateway-3 专属 Redis 频道发布 payload。Gateway-3 校验连接所有权并写 B 的 socket，B 按 message_id ACK 后才清 pending。

### 详细回答

必须按这个顺序说：

```text
认证 uid
→ 有界分片队列
→ Logic 权限与幂等
→ Redis 会话 seq
→ MySQL 消息事实
→ pending_ack / payload index
→ route:B
→ im_message_push:gateway-3
→ 本机 ClientManager
→ WebSocket
→ ACK 清理
```

`MESSAGE_ACCEPTED` 只表示 Logic 当前处理成功，不表示 B 已 ACK。Redis Pub/Sub 只负责在线唤醒，订阅者断开时不会保存事件，所以可靠性依赖 MySQL、pending、ACK 和重连，而不是 Pub/Sub 自己重放。

### 结合 LinkGo

入口在 `StartClientLoopWithDB`，业务在 `LogicHandler.PushMessage`，在线投递在 `RedisDelivery.Deliver`，目标 Gateway 在 `ClientManager.SubscribeRedis` 一类订阅路径中找到本机连接。

### 为什么这样选，替代方案是什么

Gateway 之间全广播浪费资源，两两维护 RPC 网格也更复杂。共享 route 加目标频道适合当前低延迟单聊。若需要更强持久投递，可以演进到内部持久日志或消息 Outbox，但路径和成本都会增加。

### 边界与追问

当前一个 uid 只保留一条连接，不是手机、电脑多端同时在线。单聊也不经过 Kafka，不能为了统一术语说所有消息都由 Kafka 投递。

## 3. 六十秒讲完顺序、幂等、ACK 和离线

### 面试官问

消息重复、乱序、ACK 丢失和用户离线分别怎样处理？

### 30 秒回答

发送重试复用 client_msg_id，Redis 短期幂等状态加 MySQL `(from_uid, client_msg_id)` 唯一索引防重复入库；同会话使用 Redis Lua 原子生成 seq，客户端按 seq 排序；下行用 message_id 做 ACK 和去重，ACK 丢失允许重推；离线消息正文保存在 MySQL，Redis只保留约 7 天的 pending 和 timeline，重连先补 pending，再按单个 session cursor 从 Redis 或 MySQL 补缺口。

### 详细回答

四个标识必须分开：

- `client_msg_id`：同一次发送意图的重试键。
- `message_id`：服务端消息身份，用于投递、去重和 ACK。
- `session_id`：消息所属会话。
- `seq`：会话内单调递增位置，用于排序和补缺口。

WebSocket 写成功不等于客户端收到，客户端处理后才 ACK。ACK 丢失会产生相同 message_id 的重复投递，这是至少一次方向的正常代价。重连不会扫描用户全部 MySQL 会话，而是先回放明确 pending，再对客户端提交并已授权的一个 `session_id + last_seq` 补齐。

### 结合 LinkGo

Gateway 分片队列保证同一 uid 在本进程中的入队处理顺序；最终会话顺序由共享 Redis seq 决定。`acked_seq` 表示客户端确认收到的位置，`read_seq` 预留给用户阅读语义，当前浏览器没有完整上报已读闭环。

### 为什么这样选，替代方案是什么

Redis 适合短期快速补偿，MySQL 适合长期历史。完整多设备同步需要设备级游标、持久客户端去重和全会话增量协议，当前没有为了功能数量继续扩大范围。

### 边界与追问

不要说 seq 连续无空洞，也不要说 ACK 等于用户肉眼已读。当前 MySQL fallback 对持久 ACK 状态的重新登记仍不完整，不能包装成微信级同步系统。

## 4. 四十五秒讲完 Kafka 群聊

### 面试官问

Kafka 为什么存在，消费者怎样避免丢和重复？

### 30 秒回答

群聊是高扇出操作，Logic 如果在同步 RPC 中逐成员投递，请求耗时会随成员数增长并被慢下游放大。项目让 Logic 校验并落库后把 frame 和 recipients 写 Kafka，Transfer 使用 FetchMessage 消费。每个 message_id 与 recipient 通过 processing owner、lease 和 done 控制重复；失败写 retry，超过次数写 DLQ，成功或失败已耐久转移后才提交原 offset。

### 详细回答

Kafka 提供的是持久日志、积压缓冲和消费组，不是 exactly-once。提交 offset 前崩溃会重复消费，所以成员级状态跳过已经 done 的接收者。投递完成但写 done 前崩溃仍可能重复，因此客户端也要按 message_id 去重。

同组多个 Transfer 能否并行取决于 partition 数。当前 demo 是单 broker、复制因子 1，两用户群只验证最小生产、消费和一个接收者投递，不证明大群吞吐和 broker 容灾。

### 结合 LinkGo

Logic 生产路径在 `cmd/logic/internal/svc/kafka_dispatcher.go`，Transfer 消费入口在 `cmd/transfer/main.go`，成员状态在 `recipient_lease.go`。群成员来自 MySQL active members，不由前端指定 recipients。

### 为什么这样选，替代方案是什么

小群和低流量可以同步循环，Redis Streams 也能做持久消费。选择 Kafka 是为了明确展示高扇出解耦、消费组和失败恢复，同时接受分区规划、积压和重复消费的成本。

### 边界与追问

不能只说“Kafka 防止卡死”。准确说法是防止同步扇出长期占用 Logic 请求和 Gateway 队列。当前 Logic 仍同步读取完整成员，所以发送前还有 O(N) 成本。

## 5. 六十秒回答故障与扩容

### 面试官问

Gateway、Logic 或依赖挂了怎么办？十台 Gateway 满了怎样扩？

### 30 秒回答

Gateway 挂掉时 WebSocket 必然断开，客户端重连到健康实例，新 route 覆盖旧 route，再用 pending 和 cursor 恢复；Logic 挂掉时在途 RPC 不能接管，客户端以同一 client_msg_id 重试到其他实例；Redis 或 MySQL 故障会影响核心不变量，所以 readiness 摘流并明确失败；Transfer 挂掉只会造成 Kafka 积压。扩容新增 Gateway 只承接新连接，已有连接不会迁移。

### 详细回答

故障回答必须说故障点，不要只说“有副本”。Logic 在 MySQL 前崩溃，通常没有消息事实；MySQL 后、Redis 前崩溃，历史已存在但实时投递可能缺失；seq 分配后落库前失败，允许出现序号空洞。客户端用相同 client_msg_id 重试，数据库唯一索引防重复。

Gateway 扩容时，Kubernetes Service 把后续新握手分给 ready Pod。缩容先摘除新流量并等待 draining，旧 socket 最终仍会断开重连。当前 HPA 按 CPU，只是示例；长连接服务还应观察连接数、队列深度、拒绝率和网络延迟。

### 结合 LinkGo

route 有 TTL，心跳与清理都比较完整 `gatewayID|connectionID`。Etcd 负责 Logic 实例变化，不能恢复已经执行一半的 RPC。故障脚本在本地 Compose 中真实停止依赖并检查 readyz 与恢复后的 smoke。

### 为什么这样选，替代方案是什么

现有 TCP 无损迁移成本极高，通用做法是客户端重连和外置恢复状态。数据库高可用、Redis Sentinel/Cluster 和多 broker Kafka 应由托管服务、代理或平台能力提供稳定入口，应用侧再处理超时和幂等。

### 边界与追问

当前没有跨机房、多数据库自动切换或连接数自定义 HPA。production overlay 的外部地址是占位符，不能描述成已经运行的线上集群。

## 6. 六十秒回答测试证据

### 面试官问

你怎样证明这些设计不是只画在架构图里？

### 30 秒回答

我把证据分四层：Go 单元和包测试覆盖队列、路由、ACK、幂等及边界，race 检查测试路径的数据竞争；core_im_demo 使用真实 HTTP、JWT 和 WebSocket 验证单聊、ACK、离线重连、MySQL 与最小 Kafka 群聊；故障脚本停止本地 Compose 服务检查 readiness 和恢复 smoke；CI 还执行格式、vet、构建、前端与文档契约、镜像构建和 K8s 清单渲染。

### 详细回答

不同测试不能混为一谈。单元测试能准确制造边界，但 fake 依赖不证明真实协议。core demo 是 Go 协议客户端，不是浏览器自动化；A、B 默认连接同一个 Gateway，所以不属于跨 Gateway 黑盒。故障脚本会真实停止本地服务，但 CI 只验证脚本契约，不执行整套停机演练。当前也没有 Playwright 页面 E2E。

Kubernetes render 只证明 YAML 能生成并满足静态约束，不证明云负载均衡、持久卷、TLS 或自动扩容真实工作。Docker job 构建镜像但 `push=false`，所以当前是完整 CI 和发布脚本设计，不是无人值守生产 CD。

### 结合 LinkGo

核心协议 demo 在 `tools/core_im_demo/main.go`，故障入口在 `scripts/fault_injection.sh`，前端 smoke 在 `scripts/frontend_smoke.sh`，CI 在 `.github/workflows/ci.yml`，K8s 清单在 `deploy/k8s`。

### 为什么这样选，替代方案是什么

下一项最有价值的测试是明确把 A、B 固定连接不同 Gateway 的端到端用例，其次是 Playwright 验证 DOM、重连和页面状态。当前时间优先用于服务端协议和故障链路，未实现项保留为明确方案。

### 边界与追问

测试通过只能证明被执行的输入和断言。不要把固定最小群测试外推成大群性能，也不要引用没有在当前 commit 重跑并保存证据的吞吐数字。

## 7. 十五秒守住项目边界

### 面试官问

最后一句话，你认为这个项目完成到什么程度？

### 30 秒回答

它已经是一个可运行、可测试、能解释多节点拓扑和关键故障窗口的 Go IM 秋招项目，但不是商业级产品。核心完成度在 Gateway、Logic、Transfer 分层、跨 Gateway 路由、消息幂等与补偿、Kafka 群投递及工程验证；未完成的是多端完整同步、真实高可用数据集群、在线生产 K8s 和最新版本大规模压测。

### 详细回答

“完成”指主链能够运行并有源码与测试证据，不代表所有演进方向都已实现。项目可以支持面试官深入到 Go 并发、网络、RPC、MySQL、Redis、Kafka、服务发现、故障和部署设计，也保留了足够清楚的限制。

最重要的是回答时先讲当前事实，再讲公司版本怎样演进。这样面试官能区分你亲自验证过的内容、你理解的架构方案和你没有经历过的生产规模。

### 结合 LinkGo

当前完整功能应以远程 `feature/im-core-wallet-extension` 分支的最新 commit 为准。仓库默认 `main` 是否包含这些能力，要在投递前单独确认，不能只发仓库首页后假设对方会切分支。

### 为什么这样选，替代方案是什么

继续堆更多普通 IM 功能不会明显增加项目深度。下一阶段应以学习、复述、源码定位和针对性补测为主，确保每一条简历描述都能守住故障追问。

### 边界与追问

最后记住四句不能说的话：消息绝不丢、严格 exactly-once、已有连接自动迁移、Kubernetes 已上线生产。用准确边界换取可信度。
