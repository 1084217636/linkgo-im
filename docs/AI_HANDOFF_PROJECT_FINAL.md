# LinkGo Chat 最终项目上下文（给 AI 与本人）

> 更新日期：2026-07-17
> 项目目录：`enterprise-im-ai/`
> 项目定位：Go 后端秋招主项目，主题为“分布式 IM + 红包并发一致性 + AI 虚拟好友”。
> 使用方式：把本文完整交给其他 AI。AI 在修改项目前，应先读本文，再读本文列出的核心代码；不要把早期规划稿当成当前事实。

## 1. 一句话说明

LinkGo Chat 是一个基于 Go 和 go-zero 的分布式即时通信系统。系统以 Gateway、Logic、Transfer 三层承载 WebSocket 接入、消息编排和 Kafka 群聊扩散，以 Redis、MySQL、Etcd 保证跨节点路由、会话内有序、幂等、ACK 补偿和历史持久化，并将红包和 AI 助手作为两条差异化业务接入消息场景。

这个项目的主次关系必须始终保持为：

```text
主线：Go 分布式 IM 与消息可靠性
亮点一：红包并发领取与一致性
亮点二：AI 作为虚拟用户进入消息系统
工程保障：测试、指标、容器化、演示脚本和文档
```

不要把项目描述成“仿微信 Demo”“大模型套壳”或“完整金融支付系统”。

## 2. 事实来源与判断规则

当文档之间不一致时，可信度从高到低为：

```text
当前代码和 SQL
  > 当前自动化测试与可重复演示结果
  > 本文
  > README 和 V9-V11 版本记录
  > V0-V8 最终包
  > 早期 project1_im_ai_assistant_plan.md 和升级计划
```

AI 不得因为规划文档中写了某功能，就直接声称代码已经完成。量化性能数据只能来自重新执行的压测报告，不能编造。

## 3. 当前完成度结论

当前不是“半成品”，而是一个已经能用于简历和后端面试的工程版本；但它还没有完全达到本文定义的最终产品形态。

### 3.1 已完成并有代码支撑

- go-zero Gateway/Logic/Transfer 分层，Gateway 使用 REST 脚手架，Logic 使用 zRPC。
- JWT 登录、WebSocket 二进制 Protobuf 协议、心跳、连接管理和令牌桶限流。
- bcrypt 密码校验、旧明文密码登录后兼容迁移、统一登录失败提示。
- WebSocket 精确 Origin 白名单校验（空 Origin 默认拒绝，仅可显式配置放行且不替代 JWT），以及群历史读取前的群成员权限校验。
- Etcd 服务发现、多 Gateway 用户路由和 Redis 定向 Pub/Sub 推送。
- Gateway 使用 uid 固定分片的有界 PushWorkerPool：同 uid 分片内 FIFO、不同分片并行，并暴露 accepted/queue_full/pool_closed/context_canceled、队列深度和处理时延指标。
- Gateway 拒绝入队时返回带 `client_msg_id` 的结构化错误帧；网页客户端对 `SERVER_BUSY` 使用指数退避加随机抖动重试，保持幂等键不变且限制最多 5 次。
- 单聊、群聊、好友、群成员、历史消息和最近会话列表。
- Redis Lua 分配会话级 `seq`，`client_msg_id` 与 `message_id` 双层幂等。
- `pending_ack`、超时重试、离线回放与 `session_timeline + last_seq` 重连补偿。
- Kafka + Transfer 群聊异步扩散、收件人级幂等、retry 和 DLQ。
- MySQL 最终消息持久化；Redis 仅承载在线态、热索引和补偿状态。
- 红包创建、领取和详情接口；等额分配；MySQL 事务、主行 `FOR UPDATE` 和唯一索引防止超卖及重复领取。
- AI 群聊总结 HTTP 接口，支持总结、待办和风险输出并落审计记录。
- AI FAQ/RAG 问答 HTTP 接口，目前使用项目文档关键词召回。
- AI 虚拟好友 `9001`：用户向它发送普通私聊消息后，Logic 异步调用 AskService，并把回复重新走普通消息落库和投递链路。
- mock、OpenAI-compatible、DeepSeek 配置和 fallback provider；调用日志、attempt 日志、基础脱敏及 Prometheus 指标。
- 单文件 Web 调试台、Docker Compose、Kubernetes 清单、健康检查和标准演示脚本。

### 3.2 尚未完全收口

1. 群聊中识别 `@AI` 或 `/summary` 并把总结作为 AI 用户的普通群消息回写，目前仍缺少完整消息链路；现有群聊总结通过独立 HTTP 接口调用。
2. 红包后端闭环已完成，但前端创建红包后仍以文本形式拼接红包提示，不是协议级 `RED_PACKET` 结构化消息和完整红包卡片。
3. 前端是可用的单文件联调台，不是完整独立前端工程，也没有浏览器点击级自动化测试。
4. FAQ/RAG 是关键词召回，不是向量数据库；脱敏是基础规则，不是完整 DLP。
5. 红包没有钱包余额、扣款、退款、资金流水和对账，因此只能称“红包并发业务模型”，不能称真实资金系统。
6. 当前没有可直接写入简历的最新压测数字；只有重新实测后才能写连接数、吞吐和 P99。
7. `[已完成 V13.2]` Gateway 固定分片队列拒绝会回写 `SERVER_BUSY / SERVER_UNAVAILABLE / REQUEST_CANCELED`；可重试错误由网页客户端退避重试，拒绝帧不会伪装成 ACK。
8. `[已完成 V13.3/V13.4]` Transfer 使用 Kafka 手动提交，并以 Redis Lua 原子维护收件人 `absent → processing(owner, lease) → done`；只有投递、retry 发布或 DLQ 发布成功后才推进 offset。
9. ACK/离线/重连需要完整双端 E2E 证据；race 测试应进入具备 CGO 工具链的 CI。

因此，对外最准确的状态是：

```text
核心后端 V1 已完成，可演示、可测试、可用于简历；
最终产品版还需完成消息调度/Kafka/ACK 可靠性证据，以及群聊 AI 回写和结构化红包消息收口。
```

## 4. 最终版本应当是什么样子

最终版本只保留四个清晰能力，不继续堆朋友圈、搜索、对象存储、多 Agent 等功能。

### 4.1 核心 IM

- 用户登录后获得 JWT 和最近会话列表。
- 客户端携带 JWT 建立 WebSocket，Gateway 维护连接、路由、心跳和 ACK。
- 单聊同步落 MySQL 后实时投递；群聊落库后经 Kafka 和 Transfer 异步扩散。
- 客户端重试复用 `client_msg_id`，服务端不重复生成消息。
- 客户端收到消息后返回投递 ACK；断线后可通过 pending 和 `last_seq` 补齐。
- 只保证会话内 `seq` 单调递增，不承诺全局顺序。

### 4.2 红包

- 单聊或群聊中发送结构化红包消息，消息只引用 `red_packet_id`，红包状态保存在红包域表中。
- 红包金额统一按“分”保存，第一版采用等额分配。
- 领取前校验用户是否属于对应会话。
- 并发领取通过 InnoDB 事务、红包主行锁和 `(red_packet_id, user_id)` 唯一索引保证不超卖、不重复领取。
- 消息投递失败不影响红包主状态；红包领取接口必须幂等地返回“已领取”结果。
- 最终版仍不实现钱包资金域，并在文档中明确边界。

### 4.3 AI 虚拟好友

- AI 是系统用户 `9001`，出现在好友列表中。
- 私聊 AI 使用普通 `WireMessage`；AI 推理异步执行，不阻塞用户消息写入和投递。
- AI 回复也走 Logic 的普通消息处理流程，因此具备 `message_id/seq`、MySQL 历史、ACK 和离线补偿。
- provider 可在 mock、DeepSeek/OpenAI-compatible 间切换；外部 provider 失败可以降级，且保留调用与 attempt 审计。

### 4.4 群聊 AI 总结

- 群成员发送 `@AI 总结` 或 `/summary` 后，系统识别命令并异步创建总结任务。
- SummaryService 读取该群最近 N 条已持久化消息，执行权限校验和上下文裁剪。
- AI 生成总结、待办、负责人和风险；结构化结果写入 `ai_summary_records`。
- 同时由 AI 用户 `9001` 生成一条普通群消息，重新进入 Logic/Kafka/Transfer 链路，所有群成员都能收到并在历史中查看。
- AI 超时或失败不能阻塞群消息；失败要有日志、指标和可理解的降级提示。

## 5. 架构与职责

```text
Web Client
   │ REST + WebSocket(Protobuf)
   ▼
Gateway ──gRPC/Etcd──► Logic ──single delivery──► Redis ──► target Gateway
   │                     │  │
   │                     │  ├──► MySQL（最终事实）
   │                     │  └──► Kafka（群聊任务）──► Transfer ──► Redis
   │                     │
   │                     ├──► AI Ask/Summary ──► Provider
   │                     └──► RedPacket Service ──► MySQL transaction
   └── ACK / retry / reconnect ──► Redis
```

模块边界：

- Gateway：HTTP API、JWT、限流、WebSocket、心跳、ACK、连接和路由，不做复杂消息编排。
- Logic：消息校验、会话 ID、seq、幂等、持久化、单聊路由、群聊任务生产和 Bot 回复回写。
- Transfer：Kafka 群聊任务的成员扩散、消费幂等、重试和死信。
- Redis：在线态、跨节点通知、pending、离线补偿和热索引，不是最终历史库。
- MySQL：消息、会话、联系人、群成员、红包和 AI 审计的最终事实。
- AI：只作为异步业务增强，不能拖慢实时消息主链路。

路由事实：Etcd 负责 Logic 节点发现，Gateway 当前使用 go-zero zRPC 的 `p2c_ewma` 负载均衡；代码没有实现 Rendezvous Hash 或一致性哈希，因此简历和面试不得声称“按用户做一致性哈希粘滞路由”。

## 6. 四条核心链路

### 6.1 普通单聊

```text
Client -> Gateway WS -> Logic gRPC
-> client_msg_id 幂等
-> Lua 分配 session seq
-> MySQL 保存消息
-> Redis pending_ack + route
-> 目标 Gateway -> Client
-> Client ACK -> 清理 pending 并推进接收进度
```

### 6.2 群聊

```text
Client -> Gateway -> Logic
-> 校验群成员、分配 seq、消息落库
-> Kafka group dispatch
-> Transfer 消费并遍历成员
-> message_id + recipient 幂等
-> Redis 在线推送或离线补偿
-> retry topic -> 多次失败后 DLQ
```

### 6.3 抢红包

```text
HTTP Claim -> JWT 用户 -> 校验会话成员
-> 查询是否领取过
-> 开启 MySQL 事务
-> SELECT 红包主行 FOR UPDATE
-> 校验 active/未过期/有剩余
-> INSERT claim（唯一索引兜底）
-> UPDATE remaining_amount / remaining_count / status
-> COMMIT
```

红包当前采用等额逻辑，最后一份领取全部剩余金额，避免除法余数丢失。

### 6.4 AI 私聊

```text
用户给 9001 发普通消息
-> 原消息先完成幂等、seq、落库和投递
-> Logic goroutine 以 20 秒 timeout 调 AskService
-> 文档召回 + provider + audit/fallback
-> 生成 from=9001 的普通消息
-> 再次进入 Logic PushMessage
-> 落库、投递、ACK、离线补偿
```

## 7. 关键数据与约束

### 7.1 MySQL 核心表

- `users`：用户和 AI 系统用户。
- `messages`：消息最终历史，关键约束是发送方与 `client_msg_id` 的唯一性。
- `conversations`、`conversation_members`：最近会话、成员和读取进度的持久化基础。
- `groups`、`group_members`：群和成员权限。
- `friend_relations`：联系人关系。
- `red_packets`：红包总金额、总份数、剩余值、状态和过期时间。
- `red_packet_claims`：领取记录，唯一键 `(red_packet_id, user_id)`。
- `ai_summary_records`：群聊总结结果。
- `ai_qa_records`：问答结果与来源。
- `ai_call_logs`：一次业务 AI 调用。
- `ai_provider_attempt_logs`：fallback 内每一次 provider 尝试。

### 7.2 Redis 关键模型

- `route:<uid>`：用户当前 Gateway 和连接。
- `gateway_users:<gatewayId>`：Gateway 到用户的反向索引。
- `gateway_conn:<gatewayId>:<connId>`：连接反向索引。
- `pending_ack:<uid>`：待客户端确认的消息。
- `ack_idx:<uid>`、`ack_retry:*`：ACK 查询和重试状态。
- `offline_msg:<uid>`：离线消息引用。
- `message_payload:<message_id>`：消息热数据。
- `session_timeline:<session_id>`：会话 `seq -> message_id` 时间线。
- `user:conversations:<uid>`：按更新时间排序的会话 ZSET。
- `conversation:last:<cid>`：会话最后消息摘要。
- `client_msg:<uid>:<client_msg_id>`：发送幂等缓存。
- `group_delivery:<message_id>:<recipient>`：群聊收件人级消费幂等。

### 7.3 必须守住的不变量

```text
同一会话内 seq 单调递增。
同一发送者重试同一 client_msg_id，只产生一条业务消息。
消息先持久化，再进入投递或群聊扩散。
Redis Pub/Sub 只负责在线通知，不承担可靠存储。
收到客户端 ACK 后才清 pending；写 WebSocket 成功不等于 ACK。
投递 ACK 不等于用户已读。
同一用户对同一红包最多一条 claim。
累计领取金额不超过 total_amount，领取份数不超过 total_count。
AI 失败不能回滚或阻塞已经成功的用户消息。
API key 不进入仓库、日志、prompt 或响应。
```

## 8. 对外接口与协议

主要 REST 接口：

```text
POST /api/v1/login
GET  /api/v1/history
POST /api/v1/friend/apply
POST /api/v1/friend/respond
GET  /api/v1/friends
POST /api/v1/group/create
GET  /api/v1/group/members
POST /api/v1/red-packets
POST /api/v1/red-packets/claim
GET  /api/v1/red-packets/detail
POST /api/v1/ai/group-summary
POST /api/v1/ai/ask
GET  /ws?token=...
```

WebSocket 使用 Protobuf `api.WireMessage`。关键字段是：

```text
message_id, client_msg_id, session_id, seq,
from, to, to_type, msg_type, body,
ack_message_id, trace_id, last_seq, sent_at
```

如果实现结构化红包，优先扩展协议中的消息类型和红包载荷，不要把金额等敏感状态复制到聊天文本中作为事实来源；消息只需稳定引用 `red_packet_id`。

## 9. 代码阅读地图

建议其他 AI 和开发者按此顺序阅读：

```text
README.md
api/protocol.proto
api/gateway.api
cmd/gateway/internal/handler/routes.go
internal/logic/handler.go
internal/delivery/redis.go
internal/server/{manager,ack,retry,sync,route}.go
cmd/transfer/main.go
internal/logic/redpacket.go
cmd/gateway/internal/logic/redpacketlogic.go
internal/logic/bot.go
cmd/logic/internal/svc/ai_bot_responder.go
internal/ai/{ask_service,summary_service,provider,fallback_provider,audit,attempt}.go
sql/init.sql
scripts/final_im_ai_demo.sh
```

## 10. 从当前版本到最终版的开发任务

### P0：最终收口，完成后才称产品最终版

1. 核心 IM 可靠性收口
   - `[已完成 V13.1]` Gateway 使用 sender 固定分片、有界队列和分片内串行处理，避免同一发送者被多个 worker 并行打乱。
   - `[已完成 V13.2]` 队列满和池关闭回传关联原 `client_msg_id` 的结构化错误帧；客户端保持幂等键并执行有上限的指数退避重试。
   - 后续仍需把鉴权失败、参数错误等其他拒绝统一纳入同一错误码规范。
   - `[已完成 V13.3]` Transfer 使用 Kafka 手动提交，retry/DLQ 发布成功后才提交原消息；发布或提交失败时不继续拉取后续消息。
   - `[已完成 V13.4]` 记录收件人 processing owner/lease/done 状态；非 owner 不能完成任务，竞争任务不提交，owner 崩溃后 lease 到期可重新领取。
   - 增加双端 ACK/离线/重连 E2E，并在具备 CGO/GCC 的 CI 中运行 `go test -race`。

2. 群聊 AI 命令回写
   - 在普通群消息成功落库后识别 `@AI`、`@9001` 或 `/summary`。
   - 异步调用 SummaryService，不阻塞原消息。
   - 由 `9001` 生成普通群消息并进入 Logic -> Kafka -> Transfer。
   - 防止 AI 回复再次触发自身；命令任务需有幂等键。
   - 增加正常、超时、重复命令、非群成员和 provider 失败测试。

3. 结构化红包消息
   - 为协议增加红包消息类型或明确的扩展 payload。
   - 创建红包成功后自动发出引用 `red_packet_id` 的消息。
   - 前端按红包状态渲染卡片，点击卡片领取并刷新详情。
   - 保持红包状态与消息投递解耦，补并发领取集成测试。

4. 统一最终演示
   - 一个命令覆盖登录、双端 WebSocket、单聊 ACK、离线回放、群聊 Transfer、红包、AI 私聊和群聊总结回写。
   - 输出机器可读和 Markdown 报告；失败步骤必须返回非零退出码。

### P1：补证据，不扩业务面

- 使用真实 DeepSeek key 做一次人工验收，但不提交 key。
- 在固定机器环境重新做长连接、单聊和群聊 fanout 压测，记录环境、P50/P95/P99、错误率和资源占用。
- 增加浏览器级 smoke 或 Playwright 测试。
- 为红包补真正的多 goroutine + MySQL 集成并发测试。

### P2：生产化设想，不应阻塞秋招版本

- Etcd watch 本地缓存、Kafka 分区扩容、消息归档与冷热分层。
- AI token/cost 统计、provider 限流、熔断与知识库热更新。
- FAQ 关键词召回升级为 BM25、embedding 或代码符号级检索。
- 钱包、流水、退款和对账应作为独立资金域，不直接塞入当前红包模块。

## 11. 最终验收定义

项目满足以下条件时，才能在项目文档中标记“最终版完成”：

> 2026-07-17 本轮验证：`go test -count=50 ./internal/server`、`go test ./...`、`go vet ./internal/server`、`make fmt-check build`、全量与 light `docker compose config`、`git diff --check` 通过；本机没有 GCC，无法执行 race 构建。全仓 `go vet ./...` 仍有 3 个既有 protobuf `MessageState` 锁复制告警，本轮未把它们误写成通过。

- `go test ./...`、`make build`、全量与 light Compose 配置检查通过。
- 核心 IM 演示能证明登录、WS、单聊、ACK、离线与重连补偿。
- 全量栈能证明群聊经过 Kafka/Transfer，而不是进程内假扩散。
- 重复 `client_msg_id` 不生成第二条消息。
- 并发抢红包无超卖、无重复 claim，金额和份数守恒。
- 用户私聊 `9001` 后，AI 回复存在于消息历史并具备普通消息 seq/ACK。
- 群聊 `/summary` 后，AI 回复由 `9001` 作为普通群消息写入历史并扩散给成员。
- provider 失败不会影响原始 IM 消息，审计中能看到失败和 fallback attempt。
- 演示页面能完成聊天、红包和 AI 三条主线。
- 简历中的所有数字都有最新压测报告支撑。

## 12. 简历写法

### 12.1 当前代码可以真实使用的 4 条

```text
- 基于 Go/go-zero 构建分布式 IM 后端，拆分 Gateway、Logic、Transfer 三层，使用 WebSocket + Protobuf 承载长连接通信，并通过 gRPC 与 Etcd 完成接入层和业务层解耦及服务发现。
- 设计 Redis 在线路由、Lua 会话 seq、client_msg_id/message_id 幂等、pending_ack 与 session_timeline 补偿机制，实现跨节点定向推送、ACK 超时重试、离线回放和断线续传。
- 使用 Kafka + Transfer 异步处理群聊 fanout，并以收件人级幂等、retry 和 DLQ 应对重复消费及扩散失败；使用 MySQL 保存消息、会话和成员关系等最终事实。
- 实现等额红包发放/领取/查询，以 MySQL 事务、行锁和唯一索引防止超卖及重复领取；将 AI 虚拟好友接入普通消息链路，并支持群聊总结、FAQ/RAG、provider fallback、调用审计和敏感信息脱敏。
```

最后一条在面试中要主动说明：AI 私聊已进入普通消息链路；群聊总结当前是独立接口，回写群消息是最终收口项。

### 12.2 P0 完成后使用的最终 4 条

```text
- 基于 Go/go-zero 设计并实现 Gateway、Logic、Transfer 分层的分布式 IM 系统，使用 WebSocket + Protobuf、gRPC + Etcd 支撑多节点长连接接入和服务治理。
- 基于 Redis Lua 会话 seq、双层消息幂等、pending ACK、离线索引与 session timeline 设计可靠投递链路，支持跨 Gateway 精准路由、弱网重试和 last_seq 重连补偿。
- 使用 Kafka 将群聊 fanout 从同步发送链路解耦至 Transfer，补充收件人级消费幂等、retry/DLQ；基于 MySQL 事务、行锁和唯一索引实现红包并发领取，保证金额与份数守恒。
- 将 AI 作为系统虚拟用户接入消息链路，实现私聊问答与群聊 @AI 总结回写；抽象 mock/DeepSeek/OpenAI-compatible provider，并加入 fallback、调用审计、attempt 留痕、脱敏和 Prometheus 指标。
```

### 12.3 一分钟项目介绍

```text
LinkGo Chat 是我的 Go 后端主项目。我把实时接入、业务编排和群聊扩散拆成 Gateway、Logic、Transfer 三层：Gateway 管 WebSocket、JWT、心跳和 ACK，Logic 管消息幂等、会话 seq、落库和路由，Transfer 通过 Kafka 做群聊异步 fanout。可靠性上，我用 Redis 维护在线路由、pending ACK、离线消息引用和 session timeline，用 MySQL 保存最终历史。项目还有两个差异化场景：一个是用事务、行锁和唯一索引实现并发红包；另一个是把 AI 作为虚拟好友接入普通消息链路，并支持群聊总结、知识问答、provider 降级和审计。这个项目的重点不是功能堆砌，而是消息可靠性、并发一致性以及 AI 如何不阻塞实时链路。
```

## 13. 必须掌握的知识点

### 第一层：能讲完整流程

- Go：goroutine、channel、context 超时与取消、锁、接口、错误处理和测试。
- WebSocket：HTTP Upgrade、读写协程、单写者原则、心跳、断线清理和重连。
- go-zero/gRPC：REST/zRPC 分层、服务上下文、中间件、序列化和服务发现。
- Redis：数据结构、TTL、Lua 原子性、Pub/Sub 限制、热点 key 和缓存失效。
- MySQL：事务隔离、行锁、唯一索引、死锁、索引设计和分页查询。
- Kafka：分区、消费组、offset、至少一次投递、幂等、重试、DLQ 和消费积压。
- AI 接入：provider 抽象、超时、fallback、审计、脱敏、RAG 召回和成本边界。

### 第二层：必须能回答“为什么”

1. 为什么 Gateway、Logic、Transfer 要拆开？
2. 为什么群聊 fanout 不在 Logic 中同步循环？
3. 为什么 Redis Pub/Sub 不能保证消息可靠？
4. ACK、已送达和已读回执有什么区别？
5. 为什么同时需要 `client_msg_id`、`message_id` 和 `seq`？
6. 为什么先落 MySQL 再投递？数据库成功、Kafka 失败时怎么演进？
7. 为什么红包用行锁仍需要唯一索引？高并发下行锁有什么瓶颈？
8. 为什么当前红包不能称资金系统？
9. 为什么 AI 必须异步于实时消息主链路？
10. 为什么保留 mock provider？fallback 怎么审计？
11. 为什么当前 FAQ/RAG 不等于向量检索？
12. 如何通过指标定位 Gateway、Kafka、Redis、MySQL 或 provider 的瓶颈？

### 第三层：需要主动承认的设计边界

- 当前语义是至少一次投递配合幂等，不宣称严格 exactly-once。
- 只保证单会话有序，不保证全局有序。
- ACK 是客户端收到，不是用户阅读。
- MySQL 与 Kafka 之间尚无事务消息/outbox，生产化可用 outbox + relay 补一致性。
- 单红包行锁在热点红包下会串行化，生产级大红包可预拆份额并用 Redis Lua 抢资格，再异步落账和对账。
- AI provider 和 RAG 是业务增强，不是模型训练平台。

## 14. 14 天学习与准备路线

### 第 1-3 天：吃透 IM

- 第 1 天：读 README、协议和 Gateway，自己画登录与 WebSocket 建连图。
- 第 2 天：读 Logic、RedisDelivery，手写单聊从发送到 ACK 的每一步。
- 第 3 天：读 ACK/retry/sync，分别推演重复发送、ACK 丢失、Gateway 宕机和断线重连。

产出：不看文档讲完 1 分钟介绍，并画出 Redis key 与消息状态变化。

### 第 4-5 天：群聊与中间件

- 第 4 天：读 Kafka 生产和 Transfer 消费，理解至少一次与收件人幂等。
- 第 5 天：学习 Kafka 分区、rebalance、lag、retry/DLQ，准备大群扩散优化方案。

产出：回答“1 万人群怎么发”“Transfer 重复消费怎么办”。

### 第 6-7 天：红包

- 第 6 天：读红包 Service 和 SQL，画事务、行锁、唯一索引的并发时序。
- 第 7 天：学习 Read Committed、死锁、热点行、金额守恒和资金域边界。

产出：在白板上解释两个用户同时抢最后一份时数据库发生什么。

### 第 8-9 天：AI

- 第 8 天：读 Ask/Summary/Provider/Fallback/Audit，说明一次 AI 调用的完整留痕。
- 第 9 天：学习 timeout、重试、熔断、token/cost、RAG 和 prompt injection 基础。

产出：回答“为什么 AI 挂了不能影响聊天”“mock 有什么工程价值”。

### 第 10-11 天：Go 与数据库基础补强

- 第 10 天：复习 goroutine 泄漏、channel、mutex、context、race 和 pprof。
- 第 11 天：复习 B+Tree、联合索引、覆盖索引、MVCC、隔离级别和慢 SQL。

### 第 12-14 天：演示与模拟面试

- 第 12 天：完整运行 demo，记录每个服务的日志与指标。
- 第 13 天：做一次 1 分钟、3 分钟和 10 分钟项目讲解录音，删掉空泛技术名词。
- 第 14 天：按本文件问题模拟追问；不会的问题回到代码定位，不背脱离实现的答案。

## 15. 给后续 AI 的执行指令

后续 AI 接到开发任务时，应遵循：

```text
1. 先核对代码，不根据旧规划猜完成度。
2. 保持 Gateway/Logic/Transfer 边界，不把 AI provider 调用放进 Gateway WebSocket 热路径。
3. 所有消息功能都要考虑幂等、顺序、持久化、ACK、离线和重连。
4. 所有红包修改都要验证权限、金额守恒、重复领取、并发和事务失败。
5. AI 任务必须有 timeout、失败隔离、审计和敏感信息保护。
6. 修改协议后同步生成代码、更新前端编码解码、测试和文档。
7. 修改后至少运行 go test ./...、make build 和 Compose 配置检查。
8. 不编造压测数字，不把 mock 结果说成真实模型效果。
9. 优先完成 P0 收口，不新增与项目主线无关的大模块。
10. 提交说明中明确：改了什么、为什么、如何验证、仍有什么边界。
```
