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
在 IM 主链路稳定后，接入 AI 群聊总结、待办提取和知识库问答。
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
      "owner": "1001",
      "content": "补充接口压测报告",
      "deadline": ""
    }
  ],
  "risks": [
    "上线前需要确认 Kafka retry 配置"
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
internal/ai/prompt.go
internal/ai/types.go
internal/ai/summary_service_test.go
internal/metrics/metrics.go
sql/init.sql
sql/20260703_ai_summary.sql
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
request_user_id
message_start_seq
message_end_seq
message_count
summary
todos_json
risks_json
provider
prompt_hash
model_response
status
error_message
created_at
updated_at
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

下一步先做 V0，不写功能代码：

```text
1. 补 docs/CODE_MAP.md。
2. 补 docs/CORE_LINKS.md。
3. 补 docs/MODULE_CARDS.md。
4. 补 docs/TEST_EVIDENCE.md。
5. 补 docs/INTERVIEW_QA.md。
```

V0 完成后再决定是否补测试或进入 V1 脚本。
