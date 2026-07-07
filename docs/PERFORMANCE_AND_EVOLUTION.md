# Performance and Evolution Notes

这个文档记录项目从“能跑的 IM”逐步演化到秋招项目的过程。它不是宣传稿，而是你面试时解释取舍、瓶颈和下一步优化的材料。

## V1：核心 IM 链路证据化

本版目标是把登录、WebSocket 建连、单聊、ACK、离线补偿、重连回放、MySQL 落库和 Gateway metrics 变成一个可重复演示的闭环。

验收入口：

```bash
START_STACK=1 COMPOSE_FILE_PATH=docker-compose.light.yml make core-im-demo
```

当前结论：

```text
light 栈可以稳定验证 IM 主链路。
light 栈不包含 Kafka/Transfer，所以群聊异步扩散按预期 SKIP。
```

瓶颈判断：

```text
1. 长连接数量上来后，Gateway 的连接管理、心跳和写协程会先成为压力点。
2. ACK 与离线补偿依赖 Redis pending_ack / ack_idx / offline_msg，热点用户或异常客户端会增加 Redis 写压力。
3. MySQL 负责历史消息持久化，写入慢会影响 Logic 主链路，需要后续通过批量写、异步落库或分库分表继续演进。
```

## V2：AI 群聊总结闭环

本版目标是让 IM 不只是聊天系统，而是企业协同系统：从群消息中提取总结、待办和风险，并保存审计记录。

验收入口：

```bash
START_STACK=1 make ai-demo
```

当前结论：

```text
mock provider 已经跑通权限校验、消息读取、总结生成、MySQL 留痕和 Prometheus 指标。
AI 不进入 WebSocket 主链路，避免模型超时影响实时消息投递。
```

瓶颈判断：

```text
1. 真实模型 provider 的延迟和失败率不可控，需要超时、降级和重试。
2. 总结输入不能无限取消息，后续要按 seq 范围、token 预算和消息重要性做裁剪。
3. AI 调用日志需要脱敏，否则会把企业聊天内容带入审计风险。
```

## V3：群聊 Kafka/Transfer 验收入口

本版目标是把群聊异步扩散单独沉淀成可执行入口，而不是只在文档里说“有 Kafka/Transfer”。

验收入口：

```bash
make group-transfer-demo
```

等价展开：

```bash
START_STACK=1 REQUIRE_TRANSFER=1 COMPOSE_FILE_PATH=docker-compose.yml bash scripts/demo_core_im.sh
```

链路目标：

```text
Gateway WebSocket
  -> Logic gRPC
  -> MySQL messages
  -> Kafka group_message_dispatch
  -> Transfer consumer
  -> Redis route / pending_ack
  -> Gateway 下行
  -> client ACK
```

为什么这是公司级能力：

```text
群聊不是简单 for 循环发给所有人。同步扇出会把 Logic 主链路拖慢，也很难处理部分成员投递失败。
Kafka/Transfer 把写消息和扩散解耦，Transfer 可以独立扩容，失败可以进入 retry/DLQ。
```

后续压测方向：

```text
1. Gateway 连接压测：连接数、心跳成功率、消息下行延迟。
2. 单聊压测：QPS、P95/P99 延迟、Redis route 查询耗时、MySQL 写入耗时。
3. 群聊扇出压测：群成员数、Kafka 消费 lag、Transfer 消费速率、Redis pending_ack 写入量。
4. AI 总结压测：并发 summary 请求、provider 超时率、数据库写入耗时。
```

下一版建议：

```text
补 benchmark/group_fanout 或 scripts/bench_group_transfer.sh，记录小群/中群的 fanout 耗时和 Kafka lag 指标。
```

## V4：OpenAI-compatible Provider 与降级策略

本版目标是让 AI 总结从 mock provider 演进到可接真实模型，但仍保持本地演示稳定。

当前能力：

```text
AI_PROVIDER=mock
  -> 本地稳定演示

AI_PROVIDER=openai-compatible
AI_BASE_URL=https://...
AI_API_KEY=...
  -> 调用 OpenAI-compatible chat/completions
  -> 解析 JSON summary/todos/risks
  -> 失败时可 fallback 到 mock provider
```

为什么这是工程化改造：

```text
1. Provider 是接口，业务层不关心具体模型厂商。
2. Timeout 放在 provider 内部，避免模型调用无限阻塞。
3. 默认 FallbackToMock=true，保证无 key 或模型服务异常时 demo 不崩。
4. 使用 httptest 覆盖 provider 请求、鉴权 header、响应解析和 fallback。
```

当前瓶颈：

```text
1. 真实模型延迟可能远高于普通 HTTP 接口。
2. 模型返回 JSON 可能不稳定，需要更强 schema 校验。
3. 还没有记录 prompt 摘要、模型耗时、token 成本和失败原因。
4. 聊天内容可能包含敏感信息，后续要做脱敏和审计策略。
```

下一步建议：

```text
1. 给 AI provider 增加调用耗时指标和失败原因分类。
2. 增加 ai_call_logs 表，保存输入摘要、provider、耗时、错误码。
3. 补 /api/v1/ai/ask，把企业 FAQ 或项目文档问答做成第二个 AI 闭环。
```

## V5：AI 调用审计与 provider 延迟指标

本版目标是把 AI 调用从“接口能返回”升级成“可审计、可观察、可优化”。

当前能力：

```text
AI summary request
  ↓
权限校验和消息加载
  ↓
provider.Summarize
  ↓
ai_call_logs 记录 provider、消息数、seq 范围、耗时、状态、失败原因
  ↓
ai_summary_records 保存总结结果
  ↓
Prometheus 暴露 linkgo_ai_provider_latency_seconds
```

相比 V4 的改进：

```text
1. 不只知道 AI 请求成功/失败，还能知道 provider 调用耗时。
2. 每次调用都有 call_id，可以追溯谁在什么时候对哪个群做了总结。
3. 失败 provider 也会写入 ai_call_logs，便于复盘模型超时、限流或返回异常。
4. ai_demo 自动应用 20260707_ai_call_logs.sql，旧库也能升级。
```

当前边界：

```text
1. ai_call_logs 是 best-effort，写失败不阻断总结主流程。
2. FallbackProvider 目前只记录最终 provider 结果，不展开 primary/fallback 每次 attempt 明细。
3. error_message 只做长度截断，还没有敏感信息脱敏。
4. 还没有 token 成本、prompt 摘要和模型返回原文的审计字段。
```

下一步建议：

```text
1. 增加 provider_attempt_logs，把 primary failure 和 fallback success 都记录下来。
2. 增加脱敏工具，过滤手机号、token、API key 等敏感字段。
3. 接 /api/v1/ai/ask，用 FAQ/项目文档做知识库问答。
```

## V6：Provider attempt 明细与敏感信息脱敏

本版目标是补齐真实 provider fallback 时的内部过程留痕，并避免错误信息里泄露 token/API key。

当前能力：

```text
SummaryService
  ↓
AttemptRecorder 注入 context
  ↓
provider 记录每次 attempt
  ↓
ai_call_logs 记录汇总调用
  ↓
ai_provider_attempt_logs 记录每次 provider 尝试
  ↓
RedactSensitive 过滤 token/password/API key/Bearer
```

相比 V5 的改进：

```text
1. fallback 不再只有最终 provider 字段，可以按 attempt_order 看 primary/fallback 尝试。
2. provider 错误信息进入审计表前会做基础脱敏。
3. mock/openai-compatible provider 都会通过 AttemptRecorder 记录耗时和状态。
4. ai_demo 会自动应用 ai_provider_attempt_logs 迁移。
```

当前边界：

```text
1. 脱敏规则是基础正则，还不是完整 DLP。
2. attempt 表没有记录 token 成本和模型返回原文。
3. fallback 的错误传播仍以最终返回为主，后续可以把 attempt 摘要也返回给运维日志。
```

下一步建议：

```text
1. 做 /api/v1/ai/ask 知识库问答闭环。
2. 增加 token/cost 字段。
3. 引入更完整的敏感信息检测规则。
```

## V7：AI FAQ/RAG 问答闭环

本版目标是把 AI 从“总结群聊”扩成“回答项目知识问题”，但保持范围可控，不把 IM 主项目带偏成独立 AI 平台。

当前能力：

```text
/api/v1/ai/ask
  ↓
AskService
  ↓
KnowledgeBase 从 README / CODE_MAP / CORE_LINKS / INTERVIEW_QA / AI_FAQ 检索
  ↓
provider Answer
  ↓
ai_qa_records 留痕
  ↓
ai_provider_attempt_logs 记录 fallback attempt
```

相比 V6 的改进：

```text
1. AI 不再只有群聊总结接口，新增项目知识问答第二条业务链路。
2. 检索层先用项目文档做最小 FAQ/RAG，而不是一开始接向量库。
3. 问答结果和失败信息会写入 ai_qa_records，形成可审计闭环。
4. 新增 docs/AI_FAQ.md 和 ai-ask-demo，秋招演示更完整。
```

当前边界：

```text
1. 关键词召回适合当前规模，但对复杂自然语言问题的召回精度有限。
2. sources 是文档段落级，不是代码符号级或知识图谱级索引。
3. 还没有 token/cost、知识库热更新和更完整的资料权限控制。
```

下一步建议：

```text
1. 做项目一最终收口：统一简历 bullet、demo 流程和面试问答。
2. 增加 token/cost 或 knowledge hit 指标。
3. 把 FAQ/RAG 的生产化升级路线说清楚：BM25、embedding、权限分级、热更新。
```
