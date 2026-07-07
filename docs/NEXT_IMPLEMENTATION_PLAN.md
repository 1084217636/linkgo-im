# Enterprise IM AI 下一步修改计划

## 1. 项目新定位

项目一不再按“仿微信 IM”叙事，而是按这个名字准备：

```text
基于 Go-Zero 的企业研发协同 IM 与 AI 助手平台
```

主线闭环：

```text
登录
  ↓
WebSocket 建连
  ↓
单聊 / 群聊
  ↓
消息落库
  ↓
离线消息
  ↓
ACK 确认
  ↓
AI 群聊总结 / 待办提取 / 知识库问答
```

当前阶段必须先把 IM 主链路做透，再接 AI。AI 是业务增强，不是第一步。

## 2. 版本路线

### V0：跑通项目 + 建代码地图

目标：

```text
先确认项目能跑、能测、能构建，并把你需要理解的代码地图画出来。
```

当前状态：

```text
已完成首版 CODE_MAP / CORE_LINKS / MODULE_CARDS / TEST_EVIDENCE / INTERVIEW_QA。
V1 已补 demo_core_im.sh 和 tools/core_im_demo。
当前 light 栈已验证登录、建连、单聊、ACK、离线补偿、重连回放、MySQL 落库、metrics。
群聊 Kafka 扩散需要完整 docker-compose 栈，light 栈会按预期 SKIP。
```

要做：

```text
1. 记录 make test / make build / docker compose config 结果。
2. 梳理目录结构和服务入口。
3. 梳理核心调用链。
4. 梳理 MySQL 表和 Redis key。
5. 给核心模块写模块卡片。
```

新增或修改：

```text
docs/CODE_MAP.md
docs/CORE_LINKS.md
docs/MODULE_CARDS.md
docs/TEST_EVIDENCE.md
docs/INTERVIEW_QA.md
README.md
```

V0 不做：

```text
不新增 AI 接口
不改 WebSocket 协议
不改 Kafka 链路
不重写 go-zero 结构
```

### V1：IM 核心链路证据化

目标：

```text
把登录、WebSocket、单聊/群聊、落库、离线消息、ACK 做成可演示、可解释、可测试的闭环。
```

要追的 5 条核心链路：

```text
1. 登录链路：/api/v1/login -> JWT -> user_id -> 会话列表
2. 建连链路：/ws -> AuthMiddleware -> route:<uid> -> gateway 连接管理
3. 发消息链路：WebSocket -> Gateway -> Logic gRPC -> normalize -> seq -> MySQL -> RedisDelivery
4. ACK / 离线链路：pending_ack -> ack_idx -> offline_msg -> 重连回放
5. 群聊链路：group message -> Kafka -> Transfer -> recipient fanout -> retry / dead-letter
```

需要沉淀：

```text
docs/CORE_LINKS.md 每条链路都要写入口、关键函数、表、Redis key、异常处理、测试方式。
docs/TEST_EVIDENCE.md 记录命令、成功日志、失败处理。
scripts/demo_core_im.sh 给出本地演示脚本。
```

当前状态：

```text
已完成 scripts/demo_core_im.sh。
已完成 tools/core_im_demo Go WebSocket/Protobuf 演示客户端。
light 栈验证报告输出到 artifacts/core_im_demo/core_im_demo_report.md。
完整 Kafka/Transfer 验证命令：START_STACK=1 REQUIRE_TRANSFER=1 bash scripts/demo_core_im.sh。
```

优先补测试的方向：

```text
internal/logic/handler_test.go         # 消息规范化、seq、幂等、会话
internal/server/manager_test.go        # 连接管理、路由清理、ACK
internal/server/retry.go / ack.go      # ACK 重试与离线回放
internal/logic/conversation_test.go    # 会话列表、未读数
internal/logic/redpacket_test.go       # 红包事务业务亮点
```

V1 验收：

```bash
make test
make build
docker compose config
```

并且文档能回答：

```text
1. 消息怎么保证不重复？
2. 断线重连怎么补消息？
3. 群聊为什么用 Kafka？
4. Redis Pub/Sub 为什么不能当可靠队列？
5. ACK 是投递确认还是已读确认？
6. MySQL 和 Redis 分别承担什么职责？
```

### V2：AI 助手闭环

目标：

```text
在 IM 主链路稳定后，先接入 AI 群聊总结和待办/风险提取。
```

第一轮 AI 只做：

```text
POST /api/v1/ai/group-summary
```

请求：

```json
{
  "group_id": "g1001",
  "message_limit": 50,
  "include_todos": true,
  "include_risks": true
}
```

响应：

```json
{
  "summary_id": "sum_20260703_xxx",
  "group_id": "g1001",
  "conversation_id": "group:g1001",
  "message_start_seq": 101,
  "message_end_seq": 150,
  "summary": "本轮讨论主要围绕...",
  "todos": [
    {
      "title": "补充接口压测报告",
      "owner_id": "1001",
      "source_seq": 132
    }
  ],
  "risks": [
    {
      "level": "medium",
      "description": "上线前需要确认 Kafka retry 配置",
      "source_seq": 140
    }
  ],
  "provider": "mock",
  "created_at": 1783060000000
}
```

V2 文件级修改：

```text
api/gateway.api
cmd/gateway/internal/types/types.go
cmd/gateway/internal/handler/routes.go
cmd/gateway/internal/handler/aisummaryhandler.go
cmd/gateway/internal/logic/aisummarylogic.go
cmd/gateway/internal/svc/servicecontext.go
cmd/gateway/internal/config/config.go
cmd/gateway/etc/gateway-api.yaml
internal/ai/provider.go
internal/ai/mock_provider.go
internal/ai/summary_service.go
internal/ai/types.go
internal/ai/summary_service_test.go
internal/metrics/metrics.go
sql/init.sql
sql/20260705_ai_summary.sql
scripts/ai_demo.sh
README.md
```

V2 Provider 策略：

```text
第一阶段只要求 mock provider 必须可用。
没有 API key 也能跑通演示。
后续再接 OpenAI / SiliconFlow / Ollama。
```

新增表：

```text
ai_summary_records
```

字段：

```text
summary_id
group_id
conversation_id
operator_id
message_start_seq
message_end_seq
summary
todos_json
risks_json
provider
created_at
```

当前 V2 已落地：

```text
1. internal/ai 独立业务包，支持 mock provider、消息加载、群成员权限校验、结果落库。
2. Gateway 新增 POST /api/v1/ai/group-summary。
3. AI 配置支持 AI_PROVIDER / AI_MODEL / AI_TIMEOUT_SECONDS / AI_MAX_MESSAGES。
4. Prometheus 新增 linkgo_ai_summary_requests_total。
5. scripts/ai_demo.sh 支持登录、建群、写入演示消息、调用总结接口。
6. SQL 新增 ai_summary_records 表和旧库迁移脚本。
```

V2 不做：

```text
不接真实模型 API key
不做向量知识库
不做消息全文语义搜索
不在 WebSocket 主链路里同步调用 AI
```

### V3：群聊 Kafka/Transfer 验收入口

目标：

```text
先把群聊异步扩散做成明确的公司级验收入口，再继续增强 AI。
```

本版已落地：

```text
1. scripts/demo_core_im.sh 支持 COMPOSE_FILE_PATH / COMPOSE_ENV_FILE / USE_DOCKER_CN。
2. 新增 scripts/demo_group_transfer.sh，默认启动完整 docker-compose.yml，并强制 REQUIRE_TRANSFER=1。
3. 新增 make group-transfer-demo。
4. 新增 docs/VERSION_TASK_TRACKER.csv，按 Excel/CSV 方式记录每版任务、验收、瓶颈和下一步。
5. 新增 docs/PERFORMANCE_AND_EVOLUTION.md，记录核心链路、群聊 fanout 和 AI 总结的性能演化思路。
```

V3 验收命令：

```bash
make test
make build
docker compose config
bash -n scripts/demo_core_im.sh scripts/demo_group_transfer.sh
```

全量链路演示命令：

```bash
make group-transfer-demo
```

如果 light 栈占用了本地端口，先执行：

```bash
make docker-light-down
```

V3 不做：

```text
不重写 Transfer 消费逻辑
不改 WebSocket 协议
不把 AI 加到消息实时投递链路里
```

### V4：AI 助手增强

目标：

```text
把 mock provider 替换成可插拔真实 provider，并补知识库问答的最小闭环。
```

本版已落地：

```text
1. 新增 OpenAI-compatible provider，兼容 OpenAI / SiliconFlow 等 chat/completions API。
2. AI 配置新增 BaseURL / APIKey / FallbackToMock。
3. 环境变量新增 AI_BASE_URL / AI_API_KEY / AI_FALLBACK_TO_MOCK。
4. 新增 FallbackProvider，真实 provider 失败时可降级 mock。
5. 使用 httptest 覆盖 provider 请求、鉴权、JSON 解析和 fallback。
```

V4 当前边界：

```text
1. 还没有保存 AI 调用日志和 token 成本。
2. 还没有对模型返回 JSON 做强 schema 校验。
3. 还没有新增 /api/v1/ai/ask 知识库问答。
4. 真实 provider 默认不进入 WebSocket 主链路，只服务总结接口。
```

下一步 V5：

```text
1. 增加 AI 调用审计记录和 provider latency 指标。
2. 新增企业 FAQ / 项目文档知识库问答的最小闭环。
3. 增加敏感信息脱敏策略，避免把手机号、token、密钥发给模型。
```

V5 已落地：

```text
1. 新增 ai_call_logs 表和 sql/20260707_ai_call_logs.sql。
2. SummaryService 在 provider 调用后写入 call log，记录 provider、消息数、seq 范围、耗时、状态和错误信息。
3. 新增 linkgo_ai_provider_latency_seconds Prometheus histogram。
4. scripts/ai_demo.sh 自动应用 ai_call_logs 迁移。
5. 单元测试覆盖 provider success audit 和 provider error audit。
```

V5 当前边界：

```text
1. ai_call_logs 是 best-effort，不阻断总结主流程。
2. fallback 的 primary error 还没有独立 attempt 级日志。
3. error_message 还没有做敏感信息脱敏。
4. 还没有知识库问答接口。
```

下一步 V6：

```text
1. 增加 provider_attempt_logs 或把 attempt 明细写进 ai_call_logs。
2. 增加敏感信息脱敏。
3. 新增 /api/v1/ai/ask，基于 FAQ/项目文档做 RAG 问答。
```

V6 已落地：

```text
1. 新增 AttemptRecorder，通过 context 收集 provider attempt。
2. mock/openai-compatible provider 写入 attempt 状态、耗时和错误信息。
3. 新增 ai_provider_attempt_logs 表和迁移脚本。
4. 新增 RedactSensitive，对 token/password/API key/Bearer 做基础脱敏。
5. scripts/ai_demo.sh 自动应用 attempt 迁移。
```

V6 当前边界：

```text
1. 脱敏规则仍是基础正则。
2. 没有 token 成本和模型输入摘要。
3. 还没做知识库问答。
```

下一步 V7：

```text
1. 新增 /api/v1/ai/ask。
2. 用企业 FAQ/项目文档实现最小 RAG 闭环。
3. 整理最终简历 bullet 和 demo 脚本。
```

V7 已落地：

```text
1. 新增 /api/v1/ai/ask。
2. 新增 internal/ai/KnowledgeBase，按 README / CODE_MAP / CORE_LINKS / INTERVIEW_QA / AI_FAQ 做关键词召回。
3. 新增 AskService，复用 provider、attempt audit 和脱敏能力。
4. 新增 ai_qa_records 表和 sql/20260707_ai_qa_records.sql。
5. 新增 docs/AI_FAQ.md 和 scripts/ai_ask_demo.sh。
```

V7 当前边界：

```text
1. 当前是最小 FAQ/RAG，检索基于关键词，不是向量库。
2. sources 还是文档段落级，不是代码符号级索引。
3. ai_qa_records 保存结果和失败信息，但没有 token/cost 字段。
```

下一步 V8：

```text
1. 做项目一最终收口：统一 demo、简历 bullet、面试问答。
2. 补 AI token/cost 口径或知识检索命中指标。
3. 明确生产化升级边界：向量索引、完整 DLP、权限分级、知识库热更新。
```

## 3. 每次 AI 帮你改完必须补的内容

每做完一个功能，都要补：

```text
1. 改了哪些文件？
2. 新增了哪些 struct / 函数？
3. 谁调用谁？
4. 涉及哪些 MySQL 表？
5. 涉及哪些 Redis key？
6. 怎么测试？
7. 成功日志是什么？
8. 失败情况怎么处理？
9. 面试官可能怎么追问？
```

这些内容写入：

```text
docs/MODULE_CARDS.md
docs/TEST_EVIDENCE.md
docs/INTERVIEW_QA.md
```

## 4. 下一步立刻执行

当前项目一已经完成 V0、V1、V2、V3、V4、V5、V6、V7。下一步建议进入 V8 收口：

```text
1. 保持 IM 主链路、AI summary 和 group-transfer-demo 稳定可演示。
2. 把 ai-demo 和 ai-ask-demo 固定成标准演示流程。
3. 准备简历 bullet、演示脚本和面试讲法。
```
