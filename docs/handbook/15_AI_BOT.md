# 15 AI 虚拟好友：怎样把不稳定的模型接入 IM

## 本章前置

你已经掌握普通单聊消息、好友关系、MySQL 历史、后台 goroutine，以及 HTTP 调用的基本概念。

本章不要求机器学习基础。这里只研究：服务端怎样把一个外部或 mock 的文本生成能力接入聊天系统。

## 本章目标

学完后，你必须能回答：

1. AI provider 是什么，为什么要抽象接口？
2. 默认 mock provider 有什么价值和限制？
3. 用户私聊 `9001` 后，回复怎样重新进入普通消息链路？
4. FAQ 问答当前怎样检索文档，为什么不是向量 RAG？
5. 群聊总结 HTTP 接口怎样校验权限和保存结果？
6. fallback、timeout、审计分别解决什么问题？
7. 当前 AI 实现在哪些地方还不能称为生产级 Agent 或可靠 Worker？

## 1. 当前 AI 功能有三条入口

LinkGo 中容易混淆的 AI 功能其实有三种使用方式。

### 入口一：AI 虚拟好友私聊

系统预置用户：

```text
user_id  = 9001
username = ai_assistant
```

普通用户把消息发送给 9001，Logic 后台生成回答，再让 9001 以普通用户身份回复。

网页当前主要提供的就是这个入口：点击“AI 助手”，然后像私聊好友一样发送消息。

### 入口二：知识库问答 HTTP 接口

```text
POST /api/v1/ai/ask
```

它检索项目文档，把命中的片段交给 provider 回答，并把结果落库。

### 入口三：群聊总结 HTTP 接口

```text
POST /api/v1/ai/group-summary
```

它读取群聊最近若干条 MySQL 历史，生成 summary、todos 和 risks。

当前网页没有完整的群总结按钮；主要通过脚本或手工 HTTP 请求演示。

## 2. provider 是什么

provider 是“模型能力提供者”的统一接口：

```go
type Provider interface {
    Name() string
    Summarize(ctx context.Context, req SummaryRequest) (*SummaryResult, error)
    Answer(ctx context.Context, req AskRequest) (*AskResult, error)
}
```

业务层只依赖这个接口，不需要知道背后是：

- 本地 mock；
- OpenAI-compatible HTTP API；
- DeepSeek 的兼容接口；
- 以后新增的其他实现。

这样做的价值是把“读取消息、权限校验、结果落库”与“怎样调用某个模型厂商”分开。

如果业务代码直接拼死某一家 HTTP 请求，切换模型时权限、检索和落库代码也容易被一起修改。Provider 接口降低了这类耦合，也方便用 mock 做测试。代价是所有厂商先被压到 `Answer/Summarize` 这组共同能力：流式输出、工具调用等厂商特性暂时无法直接暴露；所谓 OpenAI-compatible 也不保证每家都完整支持相同参数和 `response_format`。

## 3. 默认为什么使用 mock

默认配置是：

```text
AI_PROVIDER=mock
```

mock 不访问外部模型，也不需要 API key。它使用确定性的 Go 规则：

- 总结：选取前几条非空消息组成摘要；
- 待办：查找“待办、需要、请、负责、明天”等关键词；
- 风险：查找“风险、失败、报错、延迟、超时”等关键词；
- 问答：把检索到的文档片段组合成回答。

mock 的工程价值：

- 本地和 CI 不受外网影响；
- 没有 token 成本；
- 输出相对稳定，测试容易复现；
- 可以先验证权限、检索、落库和消息回写链路。

mock 的限制：它不代表大模型的语义理解质量，不能把演示结果包装成真实模型评测。

## 4. OpenAI-compatible provider 怎样调用外部模型

当配置为 `openai`、`openai-compatible`、`siliconflow` 或 `deepseek` 时，代码会向：

```text
<AI_BASE_URL>/chat/completions
```

发送 HTTP POST，请求包含：

```text
model
system message
user message
temperature = 0.2
response_format = json_object
```

请求头使用：

```text
Authorization: Bearer <AI_API_KEY>
```

API key 通过环境变量或 Secret 注入，不能提交到 Git 仓库。

Provider 自带 HTTP client timeout，默认 10 秒。业务接口还会创建自己的超时 context，避免外部模型无限占用请求资源。

## 5. fallback 是什么

如果开启：

```text
AI_FALLBACK_TO_MOCK=true
```

流程是：

```text
先调用外部 provider
→ 成功：返回真实结果
→ 失败：调用 mock
→ mock 成功：返回降级结果
```

降级结果的 provider 名会类似：

```text
deepseek:fallback:mock
```

fallback 提高了演示可用性，但也有代价：

- 用户可能以为降级文本也来自真实模型；
- 外部 provider 长时间故障时仍会先等待超时；
- mock 质量与真实模型不同。

所以响应和监控必须保留 provider 名称，不能悄悄隐藏降级。

## 6. FAQ 问答当前怎样检索

Logic 或 Gateway 启动时，`KnowledgeBase` 从配置路径读取 Markdown 文档，并按标题分段保存在进程内存。

收到问题后：

```text
问题分词
→ 中文词生成相邻双字片段
→ 统计问题/词在 title、path、content 中出现次数
→ 计算简单分数
→ 按分数排序
→ 取 topK，默认 3
```

每个命中项包含：

```text
path
title
snippet
score
```

然后 provider 只能基于这些片段组织回答。

为什么第一版选关键词而不是立即增加 embedding 和向量数据库？当前知识源只有三份固定项目文档，内存扫描没有外部服务和索引运维，结果相对确定，离线 CI 也能复现。代价是同义词和语义相近但字面不同的问题容易漏召回，文档越多线性扫描越慢；当固定评测证明 Recall 不足或数据规模增长后，再引入 embedding、向量索引和 rerank 才有可验证的收益。

### 为什么这不是向量检索

当前没有：

- embedding 模型；
- 向量数据库；
- cosine similarity；
- BM25 索引；
- reranker；
- 文档增量索引和热更新。

它是内存中的关键词和字符串计数召回。可以称“最小检索增强问答闭环”，面试时最好主动补一句“当前不是向量 RAG”。

## 7. AI 私聊怎样进入普通 IM

假设用户 1001 给 9001 发送“Redis 用来做什么？”：

```text
1001 的普通 WireMessage
→ Gateway
→ Logic.PushMessage
→ 校验 1001 与 9001 是 normal 好友
→ 分配 c2c:1001:9001 的 seq
→ 原问题写入 MySQL messages
→ 执行普通 RedisDelivery
→ 原消息处理完成后 triggerBotResponse 启动 goroutine
```

goroutine 中：

```text
aiBotResponder.BuildReply
→ AskService 检索 KnowledgeBase
→ provider.Answer
→ 构造 From=9001、To=1001 的普通 WireMessage
→ 再次调用 Logic.PushMessage
→ 校验 9001 与 1001 的好友关系
→ 分配新的 seq
→ AI 回复写入 MySQL
→ Redis 路由到 1001 所在 Gateway
→ 页面收到、渲染并 ACK
```

这条设计的亮点是：AI 回复不是直接绕过 IM 向浏览器返回一段 HTTP 文本，而是重新使用普通消息的 ID、seq、历史和投递机制。

## 8. “异步 AI”当前到底是什么

`triggerBotResponse` 使用：

```go
go func() { ... }()
```

它让原消息不必等待 AI 回复完成，避免 provider 延迟阻塞原消息的入库和投递。

但它只是一条 Logic 进程内 goroutine，不是持久任务队列：

- 没有 Kafka/Redis Stream AI task；
- 没有任务表；
- Logic 进程崩溃时，正在生成的回复会丢失；
- 原消息重试命中幂等后不会再次触发 Bot；
- 没有多 Worker claim、lease 或失败重试状态机。

因此准确说法是“Logic 内异步 goroutine 隔离”，不能说“已实现可靠 AI Worker 集群”。

还有一个当前实现细节：9001 并没有真实 WebSocket 在线连接，但用户发给 9001 的原问题仍执行普通 Delivery，因此它可能为 9001 留下一份 pending，直到 Redis key 过期。Bot 读取问题并不依赖这份 pending，这是后续应优化的系统用户投递策略。

## 9. 群聊总结 HTTP 链路

请求用户调用 `/api/v1/ai/group-summary` 后：

```text
Gateway JWT 得到 operator_id
→ 查询 group_members
→ 必须 active，当前代码也会拒绝仍处于 mute_until 的用户
→ 从 MySQL messages 读取 group:<gid> 最近 N 条群消息
→ 按 seq 从旧到新交给 provider.Summarize
→ 得到 summary / todos / risks
→ 写 ai_summary_records
→ 写 ai_call_logs 和 provider attempt 日志
→ HTTP 返回结果
```

默认读取 50 条，配置上限默认 100 条。

当前没有实现：

```text
群成员在群里发送 @AI 或 /summary
→ AI 作为 9001 生成普通群消息
→ Kafka/Transfer 扩散给群成员
```

仓库中群总结是独立 HTTP 接口，不能说成已经完成群聊命令自动回写。

## 10. AI 结果与调用审计保存什么

主要表：

### `ai_qa_records`

保存问题、回答、命中 sources、provider、命中数、状态和错误。

### `ai_summary_records`

保存群 ID、覆盖的起止 seq、摘要、todos、risks、provider。

### `ai_call_logs`

保存一次群总结业务调用的消息范围、耗时、状态和错误。

### `ai_provider_attempt_logs`

保存一次业务调用内部每个 provider 尝试。例如外部 provider 失败、mock fallback 成功，可以看到两次 attempt。

Prometheus 还记录请求结果、provider latency 和知识命中数量等指标。

审计的作用是回答：调用了谁、耗时多久、是否降级、覆盖哪些消息，而不是只在页面显示一段无法追踪的文本。

## 11. 当前脱敏能力的真实边界

`RedactSensitive` 使用正则替换日志或数据库错误文本中的：

```text
Bearer token
api_key / token / password / secret
```

Ask 保存 question 时也执行这类基础替换。

但当前代码不会在发给外部 provider 前对群消息正文和问题执行完整 DLP：

- 没有识别身份证、手机号、邮箱或业务敏感字段；
- 没有租户级数据策略；
- 没有 prompt injection 防护；
- 没有 source 白名单审批；
- 没有模型侧数据保留策略校验。

所以只能说“对部分审计错误和问题字段做基础正则脱敏”，不能说“敏感数据不会发送给外部模型”。

## 12. 当前 AI 的其他工程边界

### 已实现

- Provider 接口抽象。
- mock 与 OpenAI-compatible HTTP provider。
- DeepSeek 兼容配置。
- timeout 与可选 mock fallback。
- 静态项目文档关键词召回。
- 问答与群总结结果落库。
- provider attempt 审计与基础指标。
- AI 虚拟好友回复重新走普通消息链路。

### 尚未实现

- 向量 embedding、向量库、rerank。
- 多轮对话记忆；Bot 当前主要使用本次问题和检索片段。
- 流式 token 输出、SSE 或 WebSocket 增量消息。
- AI 调用限流、熔断、预算和 token/cost 统计。
- 持久 AI 任务队列和失败恢复。
- 群聊 `@AI`/`/summary` 普通消息回写。
- 工具调用、多 Agent、模型训练或微调。
- 完整 DLP 和 prompt injection 防护。
- 知识库热更新；当前默认读取 `docs/knowledge/` 下三份固定静态语料，进程启动时加载，修改后需要重启才生效。

## 13. AI 为什么不能放在 WebSocket 热路径里同步等待

外部模型具有不确定性：

- 可能 100ms，也可能数秒；
- 可能限流；
- 可能网络超时；
- 可能返回格式不正确；
- 可能临时不可用。

如果普通消息落库前必须等待 AI：

```text
模型慢
→ Logic gRPC 变慢
→ Gateway 发送队列积压
→ 普通聊天也被拖累
```

当前私聊 Bot 至少在原消息完成后启动 goroutine，使 AI 失败不会回滚已经成功的用户消息。

更可靠的下一版应是：

```text
原消息提交
→ 写 AI task/outbox
→ 独立 Worker claim
→ provider 调用
→ 幂等写 AI 回复
→ 普通消息链路投递
```

## 14. 面试时怎样准确回答

可以这样说：

> 我把 AI 作为系统用户 9001 接入普通单聊。用户问题先按普通消息完成权限校验、seq 和 MySQL 落库，之后 Logic 启动后台 goroutine调用 AskService；AskService 对固定项目文档做关键词 topK 召回，再调用 mock 或 OpenAI-compatible provider。回复重新调用 Logic.PushMessage，所以也有普通消息的 message_id、seq、历史和 ACK。Provider 层有 timeout、可选 mock fallback、调用结果与 attempt 审计。群总结当前是独立 HTTP 接口，会校验群成员并读取 MySQL 历史。我要明确两个边界：默认演示是 mock，检索不是向量 RAG；goroutine 也不是持久 Worker，群聊 @AI 回写、任务恢复、成本控制和完整 DLP 尚未实现。

## 代码锚点

按顺序阅读：

1. `internal/ai/provider.go`：Provider 接口和配置选择。
2. `internal/ai/mock_provider.go`：mock 实际怎样生成结果。
3. `internal/ai/knowledge_base.go`：关键词检索算法。
4. `internal/ai/ask_service.go`：问答、sources 和落库。
5. `internal/ai/summary_service.go`：群权限、历史读取和总结落库。
6. `internal/ai/openai_provider.go`：外部 HTTP 请求。
7. `internal/ai/fallback_provider.go`：降级路径。
8. `internal/logic/bot.go` 与 `cmd/logic/internal/svc/ai_bot_responder.go`：虚拟好友回写。
9. `cmd/gateway/internal/logic/aiasklogic.go` 与 `aisummarylogic.go`：HTTP 入口逻辑。
10. `public/index.html`：搜索 `openAIChat`。

## 动手练习

画出用户 1001 向 9001 发送问题的两条消息：

```text
用户问题：1001 → 9001
AI 回复： 9001 → 1001
```

分别标出两次 `Logic.PushMessage`、两个 seq、两次 MySQL 消息写入，以及 provider 调用发生在中间哪里。

然后推演：外部 provider 超时且 fallback 开启时，用户最终看到什么，哪些审计记录应该出现？

## 闭卷检查

1. Provider 接口解决了什么耦合问题？
2. mock 有什么工程价值，为什么不能当真实模型效果？
3. 当前知识库怎样召回文档？为什么不是向量 RAG？
4. AI 回复为什么再次调用 `PushMessage`？
5. 私聊 Bot 的 goroutine 为什么不等于可靠 Worker？
6. 群聊总结当前是 HTTP 接口还是群消息命令？
7. fallback 为什么必须在结果中暴露 provider 名称？
8. 当前审计表分别记录什么？
9. 当前脱敏为什么不能称完整 DLP？
10. AI provider 故障为什么不应该回滚用户原消息？

十个问题能闭卷讲清楚后，再进入第 16 章。

下一步：[16 安全、日志、指标和故障](16_SECURITY_OBSERVABILITY.md)
