# LinkGo 当前架构事实

## 服务

- Gateway：HTTP、WebSocket、本机连接、ACK、推送队列；部分好友、群、红包和 AI HTTP 接口当前也直接访问 MySQL。
- Logic：登录、历史和 WebSocket 消息的 gRPC 处理，消息 ID/seq、同步消息落库、单聊投递和群聊任务生产。
- Transfer：消费已带完整 recipients 的 Kafka 群任务，逐收件人投递，处理 retry/DLQ 和成员级 lease 幂等。

## 多实例

Logic Pod 把 Pod IP 注册到 Etcd `services/logic`。Gateway 未配置 `LOGIC_ADDR` 时取得实例列表，并由 go-zero zRPC `p2c_ewma` 选择。当前主链路不是一致性哈希。

客户端 A、B 可以位于不同 Gateway。每台 Gateway 只持有本机 WebSocket；共享 Redis 的 `route:<uid>` 指出目标用户当前 Gateway，定向 Pub/Sub 频道把在线通知送到目标 Gateway。

## 存储

- MySQL：消息最终历史、关系数据、红包和 AI 记录。
- Redis：在线路由、会话 seq、短期 pending/ack payload/timeline 和热点索引。
- Kafka：群聊逐成员投递任务的异步缓冲、重试与死信链路。

当前应用通过单个 Redis 稳定地址和单个 MySQL DSN 接入，不原生实现 Redis Cluster/Sentinel 客户端或 MySQL 应用层读写分离。production K8s 目录是可渲染示例，不是实际生产上线证明。

## 2026-10-27：参考 GitHub 的下一阶段架构升级

项目已经能跑跨 Gateway 单聊、业务 ACK、Outbox、群聊 Kafka 重试和短期补偿。评价“架构升级”不能只看又接入几个中间件，关键要看连接消失、旧 worker 复活、缓存过期、客户端换设备、服务重启时数据状态能否收敛。参考 [goim 按用户连接路由到节点](https://github.com/Terry-Mao/goim/blob/master/internal/logic/push.go)和 [Mattermost 后台任务的领取与结果状态](https://github.com/mattermost/mattermost/blob/master/server/channels/jobs/base_workers.go)；这里只借机制，不直接照搬它们的业务语义。

### 本轮已经修复的竞态

`internal/logic/delivery_outbox.go` 原来用条件 `UPDATE` 领取任务并把 `attempts` 加一，但成功时只检查 `status=processing`，失败时甚至仅按 `id` 更新。假设 worker A 领取后卡住，30 秒租约过期，worker B 重新领取；A 此时醒来，可能把 B 的记录改成 `done/pending/dead`。现在后续状态更新必须同时满足 `status=processing AND attempts=本次领取的代数`；A 的旧代数匹配不到 B 的新代数，就不能改写 B 的状态。`delivery_outbox_test.go` 增加旧 worker 完成更新影响 0 行的回归测试。

这只**栅栏化 MySQL 状态**，不能收回 A 已经发出的 Redis/Kafka 副作用。外部投递仍可能重复，接收方继续靠 message_id、成员级状态和客户端去重。若一次外部调用确实可能超过 30 秒，要再限制调用超时、续租或把租约改为足够长；不能说这一补丁实现了“恰好一次”。

### 下一阶段按数据不变量分三条线推进

| 线 | 当前漏洞和用户可见例子 | 建议改造及验收 |
|---|---|---|
| **离线与多端同步** | `pending_ack` 是 TTL 热状态。TTL 过后，客户端若没有可靠的每设备游标，重连只回放短期 pending 和指定会话，可能不知道另一个会话多了消息。`acked_seq=max` 也不能证明更小 seq 全收齐 | 把 MySQL `messages` 作为事实源，增加每用户/设备的持久化收件变更游标或按会话分页的服务端同步协议；先给会话列表变更，再按会话游标取缺口，响应带 `has_more/next_cursor`。ACK 要区分“收到这一条”与连续水位，空洞和乱序按已存在消息 ID 处理。验收：Redis 清空、换 Gateway、换设备、同会话乱序、100 条以上分页，最终展示和 DB 一致 |
| **消息投递状态** | Outbox `done` 表示外部投递动作成功，不能代表 B 已收到；离线时 Redis pending 过期后，MySQL 只有消息事实，无完整可查询的每接收方交付状态 | 在消息事实之外建立每接收方交付记录或用户 inbox，明确 `accepted/dispatched/delivered/read` 的不同定义。群聊成员任务完成与客户端 ACK 分开，TTL 只影响加速缓存。要给离线收件人和群成员上限设计索引、分区或归档，不把 ACK 状态塞进 Outbox。验收：关闭 Redis、断开 B 直到 TTL 后重登仍能补；Outbox 已 done 而未 ACK 时状态可解释 |
| **高并发背压与故障恢复** | Gateway 队列、Logic 扫描和 Transfer 的串行成员循环都可能在大群/慢连接下积压；现有 1 万 WS 与 500 msg/s 是分阶段本地结果，不能外推百万用户 | 对 Gateway 每连接发送队列设可观测上限和慢客户端断开/降级；Outbox 批领取、Kafka 分区按 `session_id` 保序、Transfer 限定并行度且按收件人去重；做队列长度、租约过期、DLQ、端到端延迟、DB/Redis/Kafka 饱和度和重连峰值监控。用阶梯负载加故障注入找拐点，再决定分片数和节点数，不能预报“100 万用户需几台” |

**顺序**：先同步协议与交付状态契约，再做性能分片。否则只扩大 Gateway/Transfer 会更快放大重复或丢补偿的情况。Redis Pub/Sub 现阶段只承担在线通知；将来可用 Gateway 定向 RPC 或独立通知队列替换，但替换通知路径不能代替持久化同步。群聊 Kafka topic 和消费者组的划分由消息顺序、独立扩容和失败隔离决定，不是“单聊/群聊各加一个 group”就自动可靠。

### 公司团队开发时的边界和风险

- `Gateway` 负责连接、入站协议、认证、发送队列和设备游标确认；`Logic` 拥有消息事务、权限、会话顺序和业务 ACK 语义；`Transfer` 负责群任务到收件方的派发。Gateway 直接访问 MySQL 的现有历史等接口要逐步迁到 Logic 的版本化 API，避免两个团队各写一套权限条件。
- 契约先行：协议字段增加保持旧客户端可忽略；数据库采用 expand → backfill → dual-read/dual-write → switch → contract，回滚时旧字段仍可读。每条消息保存 `message_id/session_id/seq/from_uid` 的不变量；消费和投递必须可重试。改变 ACK 语义需要同时更新客户端、服务端和故障演练。
- 安全与隐私：JWT 与 Origin 只保护建连入口，还需校验每次消息的 sender、目标关系与会话归属；跨用户历史、群成员变更后旧连接、伪造 ACK 和消息重放都要有负例测试。日志与 Trace 只存必要 ID 和错误类别，默认不打印正文、Token、密码。Redis 路由覆盖要防旧连接删除新连接；慢连接发送队列满时必须明确拒绝/断开策略，不能无限缓冲造成 OOM。
- CI 门槛：单元测试守幂等和租约代数；MySQL/Redis/Kafka 集成测试守落库、重推、清缓存重连；协议兼容测试守旧客户端；定期故障演练守节点崩溃与恢复。性能报告标明机器、节点数、并发连接、发送速率、消息大小、阶段、P95/P99、错误率和 backlog，不把不同阶段合并描述。

面试表述可以落在一个真实故障：**“我们发现 Outbox 即使有领取租约，旧 worker 在新 worker 重新领取后仍能写状态。我把领取次数作为代数，加在完成和失败更新条件里，并补了旧 worker 更新 0 行的测试。这样 MySQL 状态不会被旧 worker 覆盖；外部通知仍可能重复，因此消费端继续做幂等。下一步是把离线同步从 Redis 热 pending 扩展为持久收件游标。”**

**状态**：本轮 Outbox 代数保护已实现并通过 `go test ./internal/logic ./internal/delivery ./internal/server`；持久收件箱、全会话/多设备同步、分片与大规模故障压测仍是计划，不能写进简历为已完成。

## 对照面试追问决定下一次代码改造

[牛客 IM 项目追问](https://www.nowcoder.com/discuss/927859873293795328) 和 [弱网/消息空洞面经](https://www.nowcoder.com/discuss/925154540138020864?sourceSSR=home) 都会问“客户端换网关后缺口如何发现”“历史查询与 ACK 有何关系”。这是面试问题样本。对本仓库而言，最值得做的是**持久同步协议**，而不是新建 IM 仓库、把 Redis Pub/Sub 换成另一种通知中间件，或接入 MCP。

建议以现有 `messages` 为事实源分三次可回滚改造：

1. **先定义游标契约**：客户端每会话保存最后展示的真实 `seq`，HTTP 历史补偿接口返回 `messages/next_seq/has_more`。服务端按用户身份校验会话归属，按 `seq > cursor ORDER BY seq LIMIT N` 分页。即使 Redis pending 和 timeline 全部过期，也能从 MySQL 查到真实存在的消息；seq 空洞本身不等于丢消息。当前自动重连只补 pending 和一个指定会话，不能在面试说成全会话自动同步。
2. **再补全会话变更发现**：为用户维护可持久查询的会话变更索引或收件事件表。客户端重连先分页取哪些会话有新消息，再逐会话用上一步的游标补缺。`acked_seq` 当前取最大 ACK，不能直接当作连续送达水位；升级时单条 ACK 和连续水位分开。Redis 的 route、pending、Pub/Sub 继续做在线加速，TTL 后补偿依靠持久事实。
3. **最后扩容与压测**：只有同步正确性通过，再按真实分布测试连接数、发送速率、消息尺寸、群大小、离线比例和重连风暴。看 MySQL 写入/索引、Redis 热 key、Gateway 队列、Kafka lag、Transfer 重试/DLQ 与 P95/P99，记录负载拐点后再决定分区和节点数。现有 1 万连接与 500 msg/s 分阶段本地数据不能推导百万用户容量。

每步都要提供负例：断 Redis 超过 TTL、MySQL 提交后 Gateway 崩溃、旧连接迟到 ACK、群成员中途退出、客户端换设备与跨网关、`seq` 有空洞。预期结果应以消息 ID 与数据库事实核对，不用“连接写成功”代替客户端处理成功。当前 Outbox 租约代数补丁是**已实现**；上述三步仍是**升级计划**。
