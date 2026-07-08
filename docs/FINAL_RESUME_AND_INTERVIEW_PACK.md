# 项目一简历与面试包

## 1. 简历项目名

```text
LinkGo Chat：AI 好友与红包协同 IM 系统
```

技术栈：

```text
Go / go-zero / WebSocket / gRPC / Redis / MySQL / Kafka / Etcd / Protobuf / Prometheus / Docker / AI Provider / FAQ-RAG
```

## 2. 简历 bullet

推荐 5 条版本：

```text
- 基于 Go-Zero 构建企业研发协同 IM 平台，拆分 Gateway、Logic、Transfer 三层，完成登录鉴权、WebSocket 长连接、单聊/群聊、历史消息查询和 ACK/离线补偿等核心链路。
- 基于 Redis 在线路由、会话级 Lua seq 和 pending_ack/offline_msg/ack_idx 设计消息可靠投递流程，支持跨节点定向推送、弱网重试和断线重放。
- 引入 Kafka + Transfer 处理群聊异步扩散，补充 retry / dead-letter 机制，降低大群 fanout 对主发送链路的阻塞。
- 在协同场景中接入 AI 群聊总结和 FAQ/RAG 问答能力，设计 mock / openai-compatible / fallback provider，支持调用审计、attempt 留痕和敏感信息脱敏。
- 编写 core-im-demo、ai-demo、ai-ask-demo 与统一 final demo 脚本，配合 Prometheus 指标、Docker Compose 和版本文档沉淀可演示、可复盘、可面试追问的工程闭环。
```

压缩 3 条版本：

```text
- 基于 Go-Zero 实现企业 IM 后端，采用 Gateway、Logic、Transfer 分层，支持登录、WebSocket 长连接、单聊/群聊、ACK 补偿与历史消息查询。
- 利用 Redis 在线路由、会话 seq、pending_ack 和 Kafka 异步 fanout 完成跨节点消息投递、离线回放和群聊削峰，并暴露 Prometheus 指标支撑问题定位。
- 接入 AI 群聊总结与 FAQ/RAG 问答，设计 provider/fallback/audit/脱敏机制，形成从消息协同到知识问答的业务增强闭环。
```

## 3. 1 分钟介绍

```text
这个项目是一个企业研发协同 IM 后端，我把它拆成 Gateway、Logic 和 Transfer 三层。Gateway 负责登录、JWT、WebSocket、ACK 和限流，Logic 负责消息归一化、会话 seq、落库和单聊分发，Transfer 负责 Kafka 群聊异步扩散。为了保证可靠性，我用了 Redis 在线路由、pending_ack、offline_msg 和 session_timeline 做补偿；为了保证可观测性，我加了 Prometheus 指标和标准 demo。后续我又在协同场景上接了 AI 群聊总结和 FAQ/RAG 问答，但这些都放在独立 HTTP 接口里，不影响实时消息链路。
```

## 4. 3 分钟介绍

```text
这个项目一开始只是一个 IM 基础版，但如果拿去秋招，面试官很容易把它看成普通聊天 demo。所以我做的第一件事不是加页面，而是把后端链路做成更像公司项目的形态。

我把系统拆成 Gateway、Logic 和 Transfer 三层。Gateway 专门管 WebSocket 长连接、JWT、心跳和 ACK；Logic 专门管消息归一化、会话 seq、MySQL 落库和在线路由；Transfer 负责 Kafka 异步消费和群聊扩散。这样单聊和群聊不混在一个同步发送链路里，群聊 fanout 不会拖慢主请求。

可靠性上，我用 Redis 维护 route、pending_ack、offline_msg、ack_idx 和 session_timeline。客户端收到消息后回 ACK，服务端再删 pending；如果断线重连，就根据 pending_ack 和 last_seq 做补偿。

在此基础上，我又做了两条 AI 协同增强链路：群聊总结和 FAQ/RAG 问答。这里我没有把大模型直接塞进消息链路，而是用独立 HTTP 接口、provider 抽象、fallback、ai_call_logs、ai_provider_attempt_logs 和 ai_qa_records 做留痕。这样项目既保住了 Go 后端主线，也能体现我会把 AI 能力接进真实业务。
```

## 5. 高频面试问答

### Q1：为什么不是普通 IM demo？

```text
因为我不是只做聊天收发，而是把它做成了多 Gateway、服务发现、消息可靠投递、群聊异步扩散、ACK 补偿和监控可观测的后端系统。
```

### Q2：Gateway、Logic、Transfer 为什么拆？

```text
Gateway 管长连接，Logic 管消息编排，Transfer 管群聊 fanout。这样实时接入层和高扇出扩散层解耦，群聊不会拖慢主消息链路。
```

### Q3：为什么 Redis Pub/Sub 不能当可靠队列？

```text
因为它没有持久化和消费确认，订阅者不在线时消息会丢。项目里 Pub/Sub 只做在线实时通知，可靠性靠 pending_ack、offline_msg、session_timeline 和 MySQL 历史消息。
```

### Q4：ACK 是已读吗？

```text
不是。当前 ACK 是投递确认，表示客户端已收到，不表示用户已阅读。已读回执需要额外协议和状态。
```

### Q5：群聊为什么要 Kafka？

```text
群聊 fanout 是高扇出操作，如果 Logic 同步遍历所有成员，会直接拖慢发送链路。所以我把群聊改成 Kafka + Transfer 异步扩散，再配 retry 和 dead-letter。
```

### Q6：为什么 AI 不放进 WebSocket 主链路？

```text
因为 AI 是协同增强，不应该阻塞实时消息投递。群聊总结和 FAQ/RAG 都是独立 HTTP 接口，只读已落库数据或项目文档，不影响实时消息收发。
```

### Q7：为什么 V7 先做 FAQ/RAG，不直接上向量库？

```text
因为项目一的主线是 Go 后端 IM，不是独立 AI 平台。第一版先把 question -> retrieve docs -> provider answer -> ai_qa_records 留痕 这个业务闭环做出来，再讨论向量索引优化。
```

### Q8：AI 结果怎么审计？

```text
summary 结果写 ai_summary_records，问答结果写 ai_qa_records，provider 调用写 ai_call_logs，fallback 内部 attempt 写 ai_provider_attempt_logs。
```

### Q9：为什么还保留 mock provider？

```text
为了保证本地演示和单测不依赖外部 API key。真实模型只是替换 Provider，不改业务接口、权限和留痕逻辑。
```

### Q10：你没有实习，怎么证明这是工程项目？

```text
因为我是按版本演化做的：先把 IM 主链路证据化，再做 Kafka fanout 验收、provider 抽象、审计、attempt、脱敏、FAQ/RAG，最后统一 demo 和文档。每一版都有脚本、测试和瓶颈说明，不是一次性堆功能。
```

## 6. 不要踩的坑

不要这样说：

```text
我做了一个聊天系统，然后顺手接了大模型。
我做了一个 AI 聊天机器人。
```

更好的说法：

```text
我做的是企业协同后端，AI 只是消息协同和知识问答增强。
项目重点还是分布式路由、可靠投递、群聊异步扩散和工程可观测性。
```
