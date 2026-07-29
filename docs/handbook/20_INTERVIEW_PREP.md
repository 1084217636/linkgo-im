# 20 面试讲述与逐层追问

## 本章前置

只有当你能独立画出第 19 章链路时才读本章。本章负责压缩表达，不负责替代学习。

## 本章目标

能用 20 秒、1 分钟和 3 分钟介绍项目；能在面试官追问时从架构进入代码和失败路径；不夸大未完成能力。

## 20 秒版本

> LinkGo 是我使用 Go 和 go-zero 实现的分布式 IM 项目，采用 Gateway、Logic、Transfer 分层。单聊通过 MySQL 持久化、Redis 在线路由和跨 Gateway 通知完成实时投递，并用 client_msg_id、会话 seq、ACK 和短期重连补偿处理重复、顺序与断线；群聊使用 Kafka 和 Transfer 解耦逐成员投递，业务上还实现了事务红包和 AI 虚拟好友。

## 1 分钟版本

> 项目默认按多服务器场景设计：A、B 可以连接不同 Gateway，每台 Gateway 只保存本机 WebSocket，在线路由放在共享 Redis。Gateway 通过 Etcd 发现 Logic 集群并使用 p2c_ewma 调用。A 发单聊后，Logic 做好友权限和 client_msg_id 幂等，用 Redis Lua 生成会话 seq，同步写入 MySQL，再查询 B 的 route，把实时通知发布到 B 所在 Gateway；B 收到后返回 ACK。Redis Pub/Sub 只负责低延迟通知，当前断线自动恢复依赖 Redis pending 和单会话 timeline，MySQL 历史通过独立接口查询。群聊由 Logic 解析成员后写 Kafka，Transfer 逐收件人投递并处理 lease 幂等、retry 和 DLQ。红包使用 MySQL 事务、行锁和唯一索引，AI 作为普通账号异步回复。

## 3 分钟顺序

不要随机列功能，固定按下面讲：

1. 20 秒定位和多服务器拓扑。
2. 30 秒三个服务职责。
3. 60 秒 A 在 Gateway-1、B 在 Gateway-3 的单聊链路。
4. 30 秒可靠性：ID、seq、ACK、当前重连边界。
5. 30 秒群聊 Kafka 与 Transfer。
6. 20 秒红包和 AI。
7. 10 秒验证与真实性边界。

## 技术选择必须讲出因果

面试官问“为什么用某技术”时，不要只回答“因为性能好”或“为了防止卡死”。固定使用五句话：

```text
1. 业务约束是什么
2. 最直接做法会出现什么问题
3. 当前选择解决了其中哪一段
4. 为什么没有选择另一个方案
5. 新方案带来什么代价，当前还缺什么
```

### 为什么群聊使用 Kafka

> 群聊是一条消息对应很多收件人的扇出场景。如果让 Logic 在发送 gRPC 请求里同步查询每个用户路由、登记 pending 并逐个投递，请求耗时会随群人数增长；某个下游变慢时，Logic worker 和 Gateway 分片队列会被长时间占用，继续积累就形成拥塞，看起来像“服务卡住”。所以当前代码把消息正文写入 MySQL、解析 recipients 后，将逐成员投递任务写入 Kafka，由 Transfer 异步消费。Kafka 提供突发缓冲、消费者独立扩容以及 retry/DLQ 的落点。它防的是同步扇出造成的阻塞和级联拥塞，不是防 Go 死锁；Logic 仍同步查完整成员并等待 Kafka 写成功，Kafka 还会带来积压、重复消费和运维成本。

不用 Kafka 的最小版本并非一定错误：小群、低流量时同步循环更简单。引入 Kafka 的理由必须来自扇出规模、延迟目标和失败恢复需求，而不是“项目必须堆一个中间件”。

### 核心设计理由速查

| 选择 | 最直接做法的问题 | 当前选择的理由 | 代价与边界 |
|---|---|---|---|
| WebSocket | HTTP 轮询会产生推送延迟和大量空请求 | 一条长连接双向收发，适合聊天实时下行 | 连接有状态，需要心跳、重连、路由和容量管理 |
| Gateway/Logic 分层 | 一个进程同时维护连接和执行所有业务，扩容与故障影响绑在一起 | 接入与业务可分别扩容，Gateway 统一协议和连接生命周期 | 多一次 gRPC；部署更复杂，当前部分 HTTP 业务仍直连 DB |
| MySQL 保存历史 | Go 内存重启即失；只用缓存难以承担长期关系、事务和查询 | 关系、唯一约束、事务和按会话历史查询与当前模型匹配 | 有网络与磁盘开销，需要索引、连接池和高可用入口 |
| 不增加 MongoDB | 为“消息量大”再加数据库会形成双事实源和同步问题 | 当前消息结构稳定，MySQL 已满足历史、关系和事务需求 | 真有海量冷热分层需求时，要用数据和压测重新评估 |
| Redis route | Gateway 本机 map 无法被其他 Gateway/Logic 读取 | 共享低延迟在线位置，让 Logic 定位目标 Gateway | Redis 成为实时强依赖；TTL 只能减少脏路由 |
| Redis Pub/Sub | Gateway 两两 RPC 会形成连接网；轮询会有延迟和空读 | 已知目标 Gateway 后，用定向频道做低延迟在线通知 | 断线期间不保留，必须搭配 MySQL、pending 和 ACK |
| Lua seq/条件删除 | 多 Logic 先读后写会竞争；旧连接直接 DEL 会误删新路由 | 把判断和修改放在 Redis 原子执行 | Redis 故障会影响发送；Cluster 下还要考虑 key 同槽 |
| client_msg_id/seq | 超时重试可能重复入库；并发消息缺统一会话序号 | client_msg_id 约束同一次发送，Redis 生成共享 seq | seq 单调不等于所有并发发送的物理到达顺序 |
| pending + ACK | WebSocket write 成功不代表客户端已经处理 | 先记录待确认，收到 ACK 才清理并支持有限重推 | 可能重复；当前不是完整商业级多端同步 |
| UID 分片有界队列 | 每消息起 goroutine 会资源无界；全局单 worker 会互相阻塞 | 同一 uid 进入同一 shard，限制并发和内存并暴露背压 | 哈希碰撞用户会互相影响；只保证本 Gateway 提交 FIFO |
| Kafka + Transfer | Logic 同步逐成员投递会被大群和慢下游拖住 | 把逐成员投递移到可缓冲、可重试、可扩容的消费者 | 不是防死锁；Logic 仍同步解析 recipients，消费可能积压和重复 |
| 手动提交 + lease | 先提交可能丢任务，后提交可能重复任务 | 输出成功后提交，重复时按 message_id+recipient 控制副作用 | 状态机更复杂；`done` 不代表客户端已经 ACK |
| 行锁 + 唯一索引 | 并发领取可能超发，同一用户可能重复领取 | 行锁保护总剩余，唯一索引保护单用户约束 | 热点红包在同一行排队；当前不是钱包资金系统 |
| AI provider + 异步触发 | 外部模型同步塞进消息热路径会拖慢普通聊天 | 隔离模型厂商，原消息完成后再生成回复 | 当前 goroutine 不持久，Logic 崩溃可能丢 AI 任务 |
| Docker/CI/K8s | 只在开发者电脑可运行，提交后全靠人工检查和发布 | 固化环境、自动验证、声明式管理副本与滚动更新 | 清单和绿色 CI 不等于真实生产高可用经历 |

### “为什么不用另一种方案”怎样回答

替代方案不是越多越好，只比较与当前问题最接近的一个：

- Redis Pub/Sub 与 Kafka：前者用于在线低延迟通知，后者用于需要保留、消费、重试的群投递任务，职责不同。
- MySQL 与 MongoDB：不是谁更高级，而是当前关系、事务、查询和规模证据更适合 MySQL；没有收益时不增加双存储复杂度。
- 同步循环与 Kafka：小群同步实现简单；当扇出拖长发送路径并需要独立重试、扩容时，Kafka 才值得。
- K8s Service 与 Etcd：Service 提供稳定入口；项目对复用长连接的 Logic gRPC 调用使用 Etcd 实例列表和客户端负载均衡，避免长期只粘住一个后端。

## 组件故障理由矩阵

| 故障 | 为什么会受影响 | 当前保住什么 | 当前恢复或缺口 |
|---|---|---|---|
| Gateway 宕机 | 本机 WebSocket 随进程消失 | MySQL 历史和部分 Redis 短期状态仍在 | 客户端需重连；网页暂无自动重连，旧 route 等 TTL/比较清理 |
| Logic 宕机 | 正在执行的 gRPC 中断 | 已提交的 MySQL 消息仍在 | 未提交请求可复用 client_msg_id 重试；MySQL 到投递缺 Outbox |
| Redis 故障 | route、seq、pending、Pub/Sub 都在实时链路 | 已提交 MySQL 历史 | readiness 停止接流量并等待恢复；MySQL 不能替代全部实时状态 |
| MySQL 故障 | 权限、关系和消息最终历史依赖数据库 | Redis 已有短期状态不等于可以继续安全写消息 | 当前发送失败；需要数据库高可用入口和运维恢复 |
| Kafka 故障 | 群任务无法写入或消费 | 已提交的群消息正文可能仍在 MySQL | 当前缺 Outbox 自动补投；生产写失败会让发送调用报错 |
| Transfer 宕机 | Kafka 群任务暂时没人扩散 | 未提交 Kafka 记录仍保留 | 重启后重读，lease 到期可接管，可能重复投递 |
| Etcd 故障 | Gateway 无法刷新 Logic 实例列表 | 已建立的部分 gRPC 连接可能暂时可用 | 新发现和故障摘除受影响，不能描述为无感容灾 |

## 简历三条

- 基于 Go/go-zero 实现 Gateway、Logic、Transfer 分层 IM；Logic 通过 Etcd 注册，Gateway 使用 zRPC `p2c_ewma` 选择实例，并利用共享 Redis 在线路由和定向 Pub/Sub 完成跨 Gateway 实时投递。
- 使用 `client_msg_id` Redis/MySQL 双层幂等、Redis Lua 会话 seq、MySQL 同步消息落库、客户端 ACK 和短期 pending/timeline 构建可重试、可短期恢复的投递链路，并以 UID 分片有界队列暴露背压；当前 MySQL 提交到首次投递之间仍有未使用 Outbox 的故障窗口。
- 通过 Kafka/Transfer 解耦群聊逐成员投递，实现成员级 lease 幂等、retry/DLQ 和手动位点提交；补充 MySQL 事务红包、AI 虚拟好友、Prometheus 指标及 Docker/Kubernetes 验证清单。

不要在没有重新压测报告时写具体连接数、QPS 或 P99。

## 面试官的追问树

### 第一层：用户链路

- A、B 不在一台 Gateway，消息怎样传？
- B 离线时发生什么？
- 群聊为什么不和单聊完全相同？

### 第二层：技术选择

- Redis Pub/Sub 会丢通知，为什么还用？
- 为什么消息放 MySQL，不增加 MongoDB？
- 为什么群聊使用 Kafka？
- Etcd 和 p2c_ewma 分别解决什么？

### 第三层：一致性和故障

- MySQL 写成功、发布 Redis 前 Logic 宕机怎么办？
- 消息已到 B 但 ACK 丢了怎么办？
- Kafka 投递成功、提交位点前宕机怎么办？
- Redis、Logic、Transfer 分别宕机会影响什么？

### 第四层：代码所有权

- `WireMessage` 有哪些关键字段？
- `ClientManager` 保存什么？
- `RedisDelivery.Deliver` 写哪些状态？
- 群成员在哪里解析？
- 红包事务在哪里加锁？

## 必须诚实回答的缺口

### MySQL 提交到实时通知的窗口

当前没有完整消息 Outbox。如果 MySQL 消息已提交、Logic 在 RedisDelivery 前宕机，客户端不会立即收到实时通知，需要主动历史查询才能发现。演进方案是消息事务 Outbox，但不能说已经实现。

### 离线同步

当前不是微信式“自动扫描所有会话并从 MySQL 按游标完整同步”。重连自动使用 Redis 短期状态，历史 HTTP 接口固定最近 50 条。

### 多端登录

当前 route 和本机 uid 连接都是单值，不是同账号手机、电脑多端并存模型。

### 群规模

Logic 仍同步解析完整 recipients，所以 Kafka 只解耦逐成员投递，不能说发送请求成本与群规模无关。

### Redis/MySQL 集群

当前连接稳定单入口；高可用代理可以位于入口后，但代码不原生支持 Redis Cluster/Sentinel 和 MySQL 应用层读写分离。

### 前端和 K8s

网页是调试客户端；production K8s 是可渲染示例，没有真实生产集群、Ingress/TLS 或前端 Deployment。

## 不知道答案时怎样处理

不要猜结构体或制造生产数据。可以说：

> 我先区分当前实现和演进方案。当前代码在某文件完成某步骤，边界是某故障窗口；如果继续生产化，我会先通过某指标验证，再引入某方案。

## 模拟面试评分

每次总分 100：

```text
架构与多机链路       20
单聊和可靠性         25
Kafka 群聊           15
MySQL/Redis          15
红包/AI              10
工程化与故障         10
真实性               5
```

低于 70 回到对应教材；70–84 继续源码定位；85 以上再做压力追问。

## 闭卷检查

1. 不看资料完成 1 分钟介绍。
2. 解释 MySQL 成功、Redis 通知前宕机的当前结果。
3. 说出三个绝对不能夸大的边界。
4. 从 `WireMessage` 讲到 B 的 ACK，至少指出五个代码位置。

下一步：[21 学习检查表](21_CHECKLIST.md)
