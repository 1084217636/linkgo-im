# 16 安全、日志、指标和故障

## 本章前置

你已经走完登录、WebSocket、单聊、群聊、红包和 AI 链路。本章不会再引入新的业务功能。

## 本章目标

理解“功能能运行”与“服务可安全维护”的区别；知道怎样发现故障，而不是只会看程序有没有退出。

## 安全不是一个开关

安全需要在不同入口分别处理：

```text
登录入口      防暴力尝试、保护密码
HTTP 接口     校验 JWT 和资源权限
WebSocket     校验 JWT 和 Origin
内部服务      限制网络边界和凭据
数据库        最小权限、参数化 SQL
配置          密钥不能写进公开的普通配置对象
日志          不记录密码和完整 Token
```

不要把上面七行背成口号。分析一个安全设计时固定问四件事：

```text
问题是什么
→ 完全不处理会发生什么
→ 当前选择在哪一层拦截
→ 这个选择仍有什么代价和边界
```

以 LinkGo 为例：

| 问题 | 不处理的后果 | 当前选择 | 代价与边界 |
| --- | --- | --- | --- |
| 数据库泄漏或内部人员读取用户表 | 明文密码会立即暴露，还可能被拿去尝试用户的其他网站 | 登录链路使用 bcrypt 哈希比较 | bcrypt 只能降低密码泄漏后的破解速度，不能代替强密码、登录风控和数据库权限管理 |
| 攻击者伪造 `user_id` | 可以冒充别人发送消息或查询数据 | Gateway 校验 JWT 签名，并从校验后的 Token 取得身份 | JWT 被窃取后在过期前仍可能被滥用；当前没有黑名单和完整密钥轮换 |
| 已登录用户把群或会话 ID 改成别人的 | 认证通过但越权读取群历史或 Redis timeline | 历史接口校验资源关系；WebSocket 握手和心跳回放也校验单聊两端或 active 群成员 | 群回放多一次 MySQL 查询；每个资源入口都要单独做授权 |
| 恶意网页借浏览器发起 WebSocket | 用户可能在不知情时从错误页面建立连接 | JWT 之外再校验 Origin 白名单 | 非浏览器客户端可能没有 Origin；Origin 也不能证明用户身份 |
| 登录或建连请求突然暴增 | goroutine、连接和数据库查询被耗尽 | Gateway 使用按 key 的 TokenBucket 限流 | 当前桶在单个 Gateway 内存中，多实例总配额不是全局一致 |
| 把用户输入拼进 SQL | 输入可能改变 SQL 结构，造成注入或越权读写 | 使用 `?` 占位符和参数化执行 | 参数化只解决“数据被当成 SQL”问题，不能替代资源授权和数据库最小权限 |
| 密钥写进 Git 或普通日志 | 仓库读者和日志平台使用者都可能得到生产凭据 | 生产环境应由敏感配置对象或秘密管理系统注入，日志主动脱敏 | 本地示例仍有演示值；K8s 中名字叫 Secret 的对象也不代表数据天然加密（第 18 章再定义） |
| 所有内部服务和数据库都允许任意来源访问 | 一个进程失陷后可横向访问其他组件 | 公司环境应限制“谁能连接谁”，数据库账号只授予必要操作 | 当前仓库只能展示部署规则，本地 Compose 网络和 root 演示账号不是生产隔离证据 |

这张表同时区分了“代码已经做了什么”和“生产环境还必须做什么”。Kubernetes 的 ConfigMap、Secret 等具体对象会在第 18 章定义；此处只需先区分普通配置和敏感配置。不能因为项目出现了 Secret、限流或网络访问规则文件，就声称整个系统已经绝对安全。

### 密码

数据库不应保存可直接读出的明文密码。LinkGo 使用 bcrypt 哈希；校验时对输入密码执行 bcrypt 比较。哈希不是加密：系统不需要把原密码解密出来。

### JWT 与权限

JWT 证明请求者身份，但“已经登录”不等于“能查看任意数据”。例如查询群历史或按 `session_id` 回放 Redis timeline 时，还要确认当前用户属于该资源。这叫资源级授权。

### Origin

浏览器发起 WebSocket 握手时会携带页面来源。Gateway 使用白名单限制来源，降低恶意网页借用户浏览器建立连接的风险。Origin 校验不能替代 JWT，两者解决不同问题。

### 限流

限流限制一段时间内接受的请求数量，用来保护登录和连接入口。超过限制应明确返回错误，而不是让数据库被无限请求拖垮。

TokenBucket 可以想成一个会自动补充令牌的桶：请求到来要先拿走一个令牌，桶空时拒绝；补充速度限制长期平均请求量，桶容量允许短时间突发。按用户或来源 key 分桶，是为了避免一个请求方耗尽所有人的额度。代价是服务端要保存桶状态，还要选择合适的补充速度和容量；阈值过小会误伤正常用户，过大又保护不了下游。

当前 TokenBucket 保存在每个 Gateway 进程内存中，并按 key 分片管理。三个 Gateway 各有自己的桶，所以它是单实例保护，不是整个集群共享一个全局配额；需要全局配额时还要引入共享限流状态或在统一入口限流。

### 内部服务、数据库、配置和日志

这四项容易只记住名词，需要分别看故障场景：

- 内部服务边界：Gateway 需要访问 Logic，但普通外部客户端不应直接访问 Logic 的 gRPC 端口。否则攻击面从一个入口扩大到每个内部进程。公司部署会用网络访问规则限制来源；如果需要更强的服务身份，还要增加加密传输和双向身份校验。当前项目没有完整的内部服务双向身份与加密体系。
- 数据库最小权限：应用账号只应拥有业务需要的 `SELECT/INSERT/UPDATE` 等权限。若应用被攻破，最小权限可以限制破坏范围。当前 Compose 为方便初始化使用演示 root 账号，因此只能用于本地复现，不能作为生产账号方案。
- 配置注入：地址和开关可以放普通配置，密码、JWT Secret、AI Key 必须走秘密注入。这样更换密钥时不必修改源码，也不会把真实值留在 Git 历史里；代价是部署平台还要负责权限、轮换和审计。
- 日志脱敏：日志往往被集中收集并允许多人查询。若写入完整 Token、密码或 DSN，日志系统会变成第二个泄漏源。因此排障只记录必要标识和错误类型，敏感值删除或遮盖；代价是排查时不能依赖复原秘密内容。

## 日志是什么

日志是离散事件记录。理想的消息日志需要能把一条请求串起来：

```text
trace_id
message_id
client_msg_id
user_id
gateway_id
operation
result
error
duration
```

不要记录密码、完整 JWT 或 AI API Key。上面是目标字段清单，当前并非每一条日志都完整包含它们；实际排查前必须检查具体调用点，不能仅凭文档声称已经全链路结构化。

三个容易混淆的 ID 分别回答不同问题：

- `trace_id`：这一次请求经过了哪些步骤，适合把 Gateway、Logic、Transfer 的同一次处理串起来。
- `client_msg_id`：客户端认为是哪一次逻辑发送；重试时保持它不变，服务端才能识别重复请求。
- `message_id`：服务端为已经接受并分配 seq 的消息生成的稳定标识，适合查询消息、ACK 和投递状态。

一次跨 Gateway 单聊的排障顺序应是：

```text
用户提供时间、发送人和 client_msg_id
→ 在入口日志找到 trace_id
→ 查 Logic 错误/accepted 日志，再用 client_msg_id/message_id 查 MySQL messages 确认是否落库
→ 用同一 trace_id/message_id 查 RedisDelivery 的投递结果
→ 查目标 Gateway 是否收到 Pub/Sub envelope、是否找到本机连接
→ 最后确认客户端 ACK 是否到达
```

如果某一步没有携带这些字段，链路会在这里断开。这不是“继续猜”的理由，而是一个可观测性缺口：先用 MySQL 消息记录、Redis 短期状态和相邻服务时间范围缩小问题，再把缺少的关联字段补到该调用点。当前代码只在若干关键路径写了这些字段，并非完整的分布式追踪系统。

## 指标是什么

指标是可聚合的数字时间序列。当前项目已经定义：

- 当前 WebSocket 连接数。
- 消息累计计数（可用 Prometheus `rate()` 派生每秒速率）。
- 推送队列深度。
- 队列满次数。
- ACK 重试次数。
- Kafka retry/DLQ 写入结果的累计计数（不是 topic 当前积压量）。

通用 HTTP 请求延迟直方图目前没有实现；若要分析每个 REST 路由的 P95/P99，需要补统一 HTTP middleware 指标。P95 表示 95% 的请求耗时不超过该值，P99 同理。现有 `PushProcessingLatency` 和 AI provider latency 只覆盖各自局部链路。

只看单个进程日志，很难低成本地汇总所有实例的连接数、失败率和延迟趋势。云监控或其他时序库也能解决这类问题；LinkGo 选择 Go client 暴露 `/metrics`，Prometheus 定期 pull（主动拉取），Grafana 读指标画图。这套选择便于本地复现和编写告警规则；代价是两次抓取之间可能有短时空窗，还要维护时序存储、label 基数和通知通道。日志适合查某一次失败，指标适合发现整体趋势。

先理解三类常用指标，才不会只背名字：

| 类型 | 含义 | LinkGo 例子 | 为什么这样选 |
| --- | --- | --- | --- |
| Counter | 只累计增加的总次数；重启后可以从零开始 | 消息总数、队列提交结果、Kafka 操作、限流命中 | 用 `rate(...[5m])` 才能得到最近五分钟的每秒速率，不能把累计值直接叫 QPS（每秒请求/处理量） |
| Gauge | 当前值，可以上升也可以下降 | WebSocket 连接数、每个 shard 的队列深度 | 适合回答“现在积压多少”，不适合表示历史累计次数 |
| Histogram | 把多次耗时放入不同区间 | Push 处理耗时、AI provider 耗时 | 可以估算延迟分位数，但会增加时间序列数量，桶边界也要结合实际耗时调整 |

指标的 label 用来分组，例如 `result=queue_full`。不要把 `user_id`、`message_id` 这类几乎每次都不同的值放进 label，否则会生成海量时间序列，占用内存和存储；这些高基数细节应留在日志中。

### 从指标到处置的完整链

```text
Go 代码在事件发生时更新 Counter/Gauge/Histogram
→ 服务通过 /metrics 暴露当前数据
→ Prometheus 每隔一段时间抓取并保存时间序列
→ 查询或告警规则判断是否持续异常
→ Grafana 展示趋势，值班人员再用关联 ID 查日志
→ 根据原因限流、扩容、恢复依赖或修复代码
```

例如 `queue_full` 速率持续大于零，问题不是“看到红色图就重启”：先看队列深度和处理耗时。如果深度高且处理变慢，可能是 Logic 或 Redis 变慢；如果只有某个 shard 高，可能是热点用户。扩容可能缓解新流量，但不能代替定位下游瓶颈。

当前告警规则覆盖目标不可抓取、推送队列持续拒绝和 Kafka 操作失败。Prometheus 可以把规则标记为 firing（已触发），但仓库没有配置完整的电话、短信或聊天通知通道，所以“规则存在”不等于“值班人员一定收到通知”。

## healthz 和 readyz

两者不能混淆：

- `healthz`：进程是否活着，通常只检查自身。
- `readyz`：当前是否适合接收新流量，可以检查关键依赖。

如果把 Redis 短暂抖动直接算作 liveness 失败，平台可能反复重启本来正常的 Gateway，形成重启风暴。因此深度依赖检查更适合 readiness。

## 故障分析固定模板

遇到任何故障按五步回答：

1. 影响：哪些请求失败，哪些数据仍安全？
2. 检测：哪个日志、指标或探针能发现？
3. 隔离：是否停止新流量或暂停消费？
4. 恢复：重连、重试、租约回收还是回源？
5. 边界：当前实现仍可能丢失什么？

例如 Redis 不可用：在线路由、pending 和 Pub/Sub 受影响；已经提交到 MySQL 的历史仍在。Gateway readiness 可以失败以停止接收新流量，Redis 恢复后客户端重连并进行现有补偿。不能说“完全无影响”。

## 本章代码阅读任务

| 顺序 | 打开位置 | 这次只看什么 |
| --- | --- | --- |
| 1 | `internal/middleware/auth.go` 的 `GenerateToken`、`ParseToken` 与 `cmd/gateway/internal/middleware/authmiddleware.go` 的 `Handle` | 看签名、过期和 uid Context，列出 JWT 不能替代的资源授权 |
| 2 | `internal/middleware/ratelimit.go` 的 `TokenBucketLimiter`、`Allow`，再看 `cmd/gateway/internal/middleware/ratelimitmiddleware.go` | 找到按 key 分桶、补充令牌和拒绝入口，确认状态只在单 Gateway 内存 |
| 3 | `cmd/gateway/internal/handler/websockethandler.go` 的 `webSocketOriginAllowed`、`authorizeReplaySession` | 分别写出来源校验与资源授权拦截的威胁，不把两者混成 JWT |
| 4 | `internal/health/health.go` 的 `LiveHandler`、`ReadyHandler` 与 `cmd/gateway/internal/handler/routes.go` 的三项 readiness check | 确认 live 不检查外部依赖，ready 逐项检查 Logic、Redis、MySQL |
| 5 | `internal/metrics/metrics.go` 中 `WSConnections`、`PushQueueDepth`、`PushQueueSubmissions`、`PushProcessingLatencySeconds` 与 `Handler` | 为每项写 Counter/Gauge/Histogram 类型、更新位置和一个不能放进 label 的高基数字段 |
| 6 | `deploy/observability/prometheus.yml` 与 `deploy/observability/rules/linkgo-alerts.yml` | 找到 scrape target、queue full 和 Kafka 失败规则；确认仓库没有通知通道配置 |

看到这个程度就停：给出一个安全或故障问题时，你能指出入口拦截、日志关联 ID、指标、探针和仍未覆盖的边界。暂时不必搭 Loki/Grafana 集群、实现 OpenTelemetry 全链路追踪或设计公司统一 IAM。

## 动手练习

1. 请求 `/healthz`、`/readyz`、`/metrics`，比较输出用途。
2. 从发送一条消息的日志中找 trace_id 和 message_id。
3. 暂停 Redis 后观察 readiness，再恢复；不要在重要环境随意做故障注入。

## 闭卷检查

1. JWT 身份认证为什么不能替代群成员权限校验？
2. 日志和指标分别适合解决什么问题？
3. healthz 与 readyz 有何区别？
4. Redis 故障时哪些数据仍在，哪些能力受影响？

## 动手练习与闭卷检查参考答案

### 动手练习答案

1. `/healthz` 证明 Gateway 进程能响应；`/readyz` 展示 Logic、Redis、MySQL 是否适合接流量；`/metrics` 输出 Prometheus 文本时间序列。三者都不等于端到端聊天测试。
2. 先从 Gateway 接收日志按 `client_msg_id` 或时间找到 `trace_id`；再到 Logic 的持久化/投递日志找 `message_id`。若某一步没有携带字段，应记录为可观测性缺口，不能靠猜测补链路。
3. Redis 暂停后，Gateway `ReadyHandler` 中 `Rdb.Ping` 失败，`/readyz` 应返回失败并指出 Redis 检查；恢复后再次 Ping 成功，readiness 恢复。`/healthz` 不应因为 Redis 抖动就要求重启进程。

### 闭卷检查答案

1. JWT 只能证明请求者 uid，不能证明该 uid 属于某个群或可读某个 session；群成员和会话参与者检查是资源级授权。
2. 日志保存一次具体事件和高基数关联字段，适合追单条失败；指标聚合次数、当前值和耗时趋势，适合发现整体异常和告警。
3. healthz 判断进程活着；readyz 判断实例当前是否适合接新流量，可以检查关键依赖。依赖抖动通常不应放进 liveness 造成重启风暴。
4. 已提交的 MySQL 消息、账号和关系仍在；在线 route、seq 分配、幂等预占、pending/ACK 重试、短期 timeline、Pub/Sub 和最近会话缓存受影响，当前实时发送会失败而不是无缝退化到 MySQL。

下一步：[17 Docker 与 GitHub Actions](17_DOCKER_AND_CI.md)
