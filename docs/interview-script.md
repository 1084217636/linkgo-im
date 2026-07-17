# 面试讲稿

## 3 分钟版本

这个项目是一个分布式即时通讯系统，我把它拆成了 Gateway、Logic 和 Transfer 三层。Gateway 用 go-zero REST 脚手架承接登录接口和 WebSocket 握手，负责 JWT 鉴权、心跳和限流；Logic 用 go-zero zRPC 脚手架承接内部 gRPC 服务，负责消息归一化、会话顺序号分配和历史消息持久化；Transfer 负责 Kafka 异步消费和群聊消息扩散。

项目一开始只是一个基础版 IM，但如果要往“简历级”升级，最大的问题不是有没有 WebSocket，而是分布式场景下怎么保证路由、顺序和可靠性。所以我做了几件事。

第一，我把 Logic 节点注册到 Etcd，Gateway 通过 go-zero zRPC 服务发现拿到可用节点，再使用实际配置的 `p2c_ewma` 在健康节点中做负载均衡。Etcd 解决节点发现，p2c_ewma 通过候选节点负载估计降低热点；当前代码没有实现用户维度的一致性哈希。

第二，我把消息协议从业务 JSON 收敛成 Protobuf 二进制帧。这样字段更稳定，传输更轻，后面加 ACK、心跳、系统消息也不会乱。

第三，我用 Redis Lua 给每个会话生成递增 Sequence ID，这样同一个会话里的消息就有稳定顺序。与此同时，我把待确认消息放进 `pending_ack`，客户端 ACK 之后再删除，这样弱网断线时可以重放未确认消息。

第四，群聊场景如果 Logic 直接同步扩散，很容易被大群拖慢。所以我把群聊改成 Kafka 异步分发，Logic 只负责生产任务，Transfer 再异步消费和投递。Transfer 使用 `FetchMessage + CommitMessages` 手动提交：正常投递、retry 发布或 DLQ 发布成功之后才提交原 offset；输出失败或提交失败时停在当前消息退避，避免提交更高 offset 导致失败消息被跨过。

收件人幂等不是简单的永久 `SETNX`。我用 Lua 原子维护 `processing(owner, lease)` 和 `done`：只有 owner 能完成或释放；另一个消费者看到 processing 会保留当前 offset 并等待，进程崩溃后 lease 到期可以重新领取。这样至少一次消费不会因为一个永不释放的锁卡死，也不会把“正在处理”误当成“已经成功”。

为了让项目不仅是 IM，我增加了一条游戏运营纵向链路。活动配置每次生成新版本，operator 提交、reviewer 审批发布，创建者不能自审；发布事务同时写审计和 Outbox，再由 Outbox 把灰度配置同步到 Redis。道具发放使用 `grant_request_id + user_id + item_id` 唯一键，整批在事务内更新玩家库存，重复请求返回历史结果，失败则整体回滚并留下失败审计。

第五，我在网关层加了 JWT、令牌桶限流、双向心跳和 worker pool，并且暴露 Prometheus 指标，这样这个项目不仅能跑，还能监控、能定位、能解释工程取舍。

## 面试官继续追问时可以展开的点

如果被问“为什么要 Etcd”：
因为服务节点不是固定写死的，Etcd 负责告诉 Gateway 当前有哪些 Logic 可用。

如果被问“为什么要 Kafka”：
因为群聊扇出会把同步主链路拖慢，Kafka 用来削峰填谷，让 Logic 先快速返回。

如果被问“ACK 为什么重要”：
因为 WebSocket 只代表连接建立，不代表消息一定被客户端处理。ACK 是服务端知道“这条消息真的送达并被确认”的依据。

如果被问“worker pool 的价值”：
我没有让多个 worker 共同抢一个全局队列，而是按 uid 做固定分片。每个 shard 只有一个 worker 和有界队列，所以同一 uid 的消息按入队顺序提交，不同 shard 又能并行；队列满、池关闭和 context 取消都有独立结果与指标。入队失败不会伪造 ACK，而是回传关联原 `client_msg_id` 的结构化错误；客户端对 `SERVER_BUSY` 保持幂等键，使用指数退避、随机抖动和最大次数限制重试，避免过载时重试风暴。
