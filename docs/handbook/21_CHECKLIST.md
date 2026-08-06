# 21 学习检查表

## 使用规则

这不是阅读进度表，而是“能否闭卷完成”的验收表。只有能画、能讲、能定位代码，才能勾选。

每个技术名词还必须能回答五件事，缺一项都不算掌握：

```text
它解决的原始问题
不用它时最直接的故障或瓶颈
为什么当前项目选择它
最接近的替代方案是什么，为什么当前没选
它新引入的代价与当前边界
```

## 阶段一：基础

- [ ] 能解释客户端、服务端、进程、IP、端口和协议。
- [ ] 能区分 HTTP、WebSocket 和 gRPC 的使用对象。
- [ ] 能从 `cmd/gateway/main.go` 找到 HTTP 路由注册。
- [ ] 能解释 go-zero 的 handler/logic/svc/types。
- [ ] 能使用 `rg` 找入口而不是遍历所有文件。

验收：从页面一次 `GET /healthz` 讲到响应返回。

## 阶段二：登录和连接

- [ ] 能解释表、行、索引、唯一约束和连接池。
- [ ] 能画网页、Gateway、Logic、MySQL 登录链路。
- [ ] 能解释 bcrypt 哈希和旧明文迁移边界。
- [ ] 能解释 JWT 签名不是加密、认证不等于授权。
- [ ] 能解释 HTTP Upgrade 和 WebSocket 长连接。
- [ ] 能定位 `ClientConn` 与 `ClientManager`。

验收：不看文档，从点击登录讲到 `/ws` 建连完成。

## 阶段三：单聊和多服务器

- [ ] 能解释 `client_msg_id`、`message_id`、`session_id` 和 `seq`。
- [ ] 能画单 Gateway 内单聊。
- [ ] 能解释 Redis String/Hash/ZSet/Pub/Sub/Lua 在项目中的不同用途。
- [ ] 能画 A 在 Gateway-1、B 在 Gateway-3 的完整投递。
- [ ] 能解释 Etcd 服务发现与 p2c_ewma，不说一致性哈希已用于主链路。
- [ ] 能说明 Redis Pub/Sub 是通知而不是事实源。

验收：回答“Gateway-1 为什么不能直接从本机 map 找到 B”。

## 阶段四：可靠性和群聊

- [ ] 能解释 at-least-once、重复和幂等的关系。
- [ ] 能说清 pending_ack、ack_idx、offline_msg、timeline 的真实职责。
- [ ] 知道重连不会自动从 MySQL 扫描全部会话。
- [ ] 能解释 ACK 是客户端收到进度，不是用户阅读事件。
- [ ] 能解释 Kafka topic、partition、consumer group 和 offset。
- [ ] 能准确解释 Kafka 防的是同步群扇出阻塞和级联拥塞，不说成“防 Go 死锁”或“用了就绝不卡”。
- [ ] 知道 recipients 由 Logic 解析后写入 Kafka job。
- [ ] 能讲 lease、retry、DLQ 和提交位点前宕机。

验收：连续回答 ACK 丢失、Gateway 宕机、Transfer 宕机三个故障题。

## 阶段五：业务

- [ ] 能解释好友关系与群成员为什么既是数据也是权限。
- [ ] 能说明会话摘要与消息正文不是同一个东西。
- [ ] 能画红包 `SELECT FOR UPDATE` 事务。
- [ ] 能解释唯一索引为什么仍然需要。
- [ ] 能说出红包没有钱包和资金流水的边界。
- [ ] 能解释 AI provider、mock、fallback 和轻量检索。
- [ ] 知道 AI bot 使用非持久 goroutine 的风险。

验收：分别用 30 秒讲红包和 AI，但不把它们抢成项目主线。

## 阶段六：工程化

- [ ] 能区分日志、指标、healthz、readyz。
- [ ] 能使用故障五步模板。
- [ ] 能解释镜像、容器和 Compose。
- [ ] 能解释 CI 通过证明什么、不证明什么。
- [ ] 能解释 Pod、Deployment、Service、ConfigMap、Secret 和 Probe。
- [ ] 能画 production overlay 的应用/中间件边界。
- [ ] 能解释新增 Gateway 为什么不迁移旧 WebSocket。

验收：从一次 push 讲到 GitHub Actions，再讲镜像如何滚动发布和失败回滚。

## 阶段七：代码所有权和面试

- [ ] 能闭卷写出 12 个核心结构体的职责。
- [ ] 能在 10 分钟内定位登录、单聊、群聊、红包、AI 五条入口。
- [ ] 能完成 20 秒、1 分钟、3 分钟项目介绍。
- [ ] 能主动说出至少五个当前边界。
- [ ] 能写一条真实历史查询 SQL，并解释联合索引。
- [ ] 能独立写一个 Go 并发测试。
- [ ] 能完成一次模拟面试且不出现过度表述。

## 建议学习节奏

如果每天学习 2 到 3 小时：

```text
第 1 到 2 天    第 00 到 04 章，建立基础和源码导航
第 3 到 4 天    第 05 到 08 章，登录、连接、单机单聊
第 5 到 7 天    第 09 到 11 章，Redis、多 Gateway、可靠性
第 8 到 9 天    第 12 到 13 章，Kafka、好友群会话
第 10 天        第 14 到 15 章，红包、AI
第 11 到 12 天  第 16 到 18 章，安全、观测、Docker、CI、K8s
第 13 到 14 天  第 19 到 21 章，完整源码复述和模拟面试
```

时间只是参考。答不上当前章闭卷题时不要为了赶日期进入下一章。

## 本章代码阅读任务

这是最终抽查，不再增加新知识。请计时 30 分钟完成。

| 时间 | 打开位置 | 验收动作 |
| --- | --- | --- |
| 0-5 分钟 | `cmd/gateway/internal/handler/routes.go` 的 `RegisterHandlers`、`api/protocol.proto` 的 `Logic` service 与 `WireMessage` | 不借助手册列出 HTTP、WebSocket、gRPC 三类入口及关键协议字段 |
| 5-12 分钟 | `internal/server/client.go` 的 `StartClientLoop`、`pool.go` 的 `SubmitWithResult`、`manager.go` 的 `SubscribeRedis` | 画 Gateway 上行队列和目标 Gateway 下行，标出本机 map 与共享 Redis 的边界 |
| 12-20 分钟 | `internal/logic/handler.go` 的 `PushMessage`、`internal/delivery/redis.go` 的 `Deliver`、`internal/server/ack.go` 的 `AckMessage` | 画跨 Gateway 单聊，写出 MySQL、pending、PUBLISH、结果帧和接收 ACK 的顺序 |
| 20-25 分钟 | `cmd/transfer/main.go` 的 `processFetchedMessage`、`recipient_lease.go` 的三个 Lua | 口述群聊 job、lease、retry/DLQ 和 CommitMessages 条件 |
| 25-30 分钟 | `internal/logic/redpacket.go` 的 `Claim`、`internal/logic/bot.go` 的 `triggerBotResponse` | 各用 30 秒说出实现、代码证据和不能夸大的边界 |

看到这个程度才算总验收通过：不看答案画出两条主时序，随机抽一个函数能说出调用者与失败出口，随机抽一个组件能讲问题、选择、替代和代价。暂时不必背行号，也不要为了好听编造生产规模数据。

## 最终验收题

在白纸上完成：

1. 画公司多服务器拓扑。
2. 画跨 Gateway 单聊时序。
3. 画 Kafka 群聊时序。
4. 写出 Redis 和 MySQL 各自保存的数据。
5. 解释五个故障及恢复。
6. 指出当前实现的五个缺口。
7. 任选五项技术，分别说出“问题、选择理由、替代方案、代价”。

这七项能独立完成后，才进入持续模拟面试阶段。

## 阶段验收与最终验收题参考答案

### 各阶段验收口径

- 阶段一：`GET /healthz` 由 Gateway 的 `RegisterHandlers` 匹配 `LiveHandler`，返回进程存活结果。它不检查 Redis/MySQL/Logic。
- 阶段二：页面 `login()` 经 HTTP Gateway、gRPC Logic、MySQL users、bcrypt 和 JWT 返回 Token；页面再用 Token 握手 `/ws`，通过 Origin/资源检查后登记 `ClientConn`、`ClientManager` 和 Redis route。
- 阶段三：Gateway-1 不能读取 Gateway-3 的 map。共享 Redis `route:B` 定位 Gateway-3，定向 Pub/Sub 唤醒它，再由它查本机 B。
- 阶段四：ACK 丢失会触发同 message_id 重推；Gateway 宕机丢本机连接，客户端重连并用 Redis 短期状态补偿；Transfer 宕机留下未提交 Kafka offset，重启后配合 recipient lease 继续，可能重复。
- 阶段五：红包用行锁保护总剩余、唯一索引保护单用户，当前没有钱包；AI provider 隔离模型实现，Bot goroutine 不持久，当前检索不是向量 RAG。
- 阶段六：push 触发 CI 对该 SHA 执行 fmt/vet/test/race/build 和配置检查；经过外部推镜像后，发布脚本可 set image、等 rollout、smoke，失败尝试 undo。当前 Actions 本身 `push:false`，没有真实生产 CD。
- 阶段七：只有能在十分钟内定位五条入口、讲清十二个结构体所有权并主动说边界，才算代码所有权通过。

### 最终验收题答案

1. 公司拓扑应含外部 LB、多个 Gateway、Etcd 发现的多个 Logic、共享 MySQL/Redis/Kafka、多个 Transfer。WebSocket 只终止在具体 Gateway，应用连接外部稳定中间件入口。
2. 跨 Gateway 单聊：A WebSocket -> Gateway-1 uid shard -> gRPC Logic -> 权限/幂等/seq -> MySQL -> B pending -> Redis route/PUBLISH -> Gateway-3 本机连接 -> B ACK；Logic 结果另回 A。
3. Kafka 群聊：Logic 校验群成员、写一条 MySQL 消息、查询 recipients 并写 job；Transfer Fetch 后逐成员 claim lease 和 RedisDelivery，成功 done，失败转 retry/DLQ，输出耐久后才 Commit。
4. MySQL 保存账号、关系、群成员、消息历史、会话摘要、红包和 AI 记录；Redis 保存在线 route、幂等预占、seq、pending/ACK、offline/timeline 和会话热数据。Redis Pub/Sub 是通知，不是存储表。
5. 五个故障示例：Redis 宕机使实时链路失败但 MySQL 历史仍在；Logic 宕机中断 RPC且 Outbox 窗口需主动重试；Gateway 宕机要客户端重连；Transfer 宕机由未提交 offset 和 lease 恢复；ACK 丢失造成同 ID 重推并由客户端去重。
6. 五个缺口示例：没有事务 Outbox；重连不自动扫 MySQL 所有会话；不支持多设备 route；群 recipients 不分页；没有真实生产 K8s/Redis/MySQL 集群验证。也可用历史无游标、AI Worker 不持久、红包无钱包替换其中项。
7. 技术选择示例：WebSocket 解决服务端实时下行，替代轮询但增加连接管理；Redis route 解决跨 Gateway 定位，替代节点两两 RPC但成为强依赖；MySQL 解决持久关系和事务，替代进程 map但增加网络磁盘成本；Kafka 解耦群扇出，替代同步循环但引入重复和积压；K8s 管理副本和滚动发布，替代手工进程运维但增加集群复杂度。每项都必须再结合当前代码位置说明，不能只背这段。

完成本轮后回到：[查看手册总目录并按薄弱章节回读](README.md)。之后只针对检查表中的薄弱项复习，不再随机翻旧文档。
