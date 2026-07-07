# 测试证据

记录每一轮改动后的验收命令、成功结果和后续待补证据。

## 1. 当前基线

日期：2026-07-04

执行目录：

```text
/home/xiaobin/myproject/enterprise-im-ai
```

### make test

命令：

```bash
make test
```

结果：

```text
通过。
主要包：
cmd/gateway
cmd/logic
cmd/transfer
internal/health
internal/logic
internal/middleware
internal/server
```

### make build

命令：

```bash
make build
```

结果：

```text
通过。
生成：
bin/gateway
bin/logic
bin/transfer
```

### docker compose config

命令：

```bash
docker compose config
```

结果：

```text
通过。
配置展开后约 311 行。
```

## 2. V1 核心 IM Demo

命令：

```bash
bash scripts/demo_core_im.sh
```

当前环境：

```text
linkgo-light-gateway-a
linkgo-light-logic
linkgo-light-mysql
linkgo-light-redis
linkgo-light-etcd
```

报告：

```text
artifacts/core_im_demo/core_im_demo_report.md
```

结果：

```text
PASS gateway healthz
PASS login userA
PASS login userB
PASS redis ping
PASS mysql ping
PASS websocket connect userA
PASS websocket connect userB
PASS single chat receive + ack
PASS ack clears pending
PASS offline indexes recorded
PASS offline replay + ack
PASS mysql messages persisted
PASS gateway metrics exposed
SKIP group chat via kafka transfer
```

说明：

```text
当前运行的是 docker-compose.light.yml 栈，不包含 Kafka 和 Transfer，所以群聊 Kafka 扩散按预期跳过。
完整群聊扩散验收使用全量栈：

START_STACK=1 REQUIRE_TRANSFER=1 bash scripts/demo_core_im.sh
```

## 3. 已有测试覆盖

| 测试文件 | 当前价值 |
| --- | --- |
| `cmd/gateway/main_test.go` | Gateway 配置解析、启动相关基础检查 |
| `cmd/logic/main_test.go` | Logic 配置和 Kafka topic / broker 配置解析 |
| `cmd/transfer/main_test.go` | Transfer 辅助函数、群聊幂等 key |
| `internal/health/health_test.go` | 健康检查 |
| `internal/logic/handler_test.go` | 消息幂等、落库兼容等 Logic 行为 |
| `internal/logic/conversation_test.go` | 会话列表、未读数、会话状态 |
| `internal/logic/redpacket_test.go` | 红包创建、领取、重复领取、事务行为 |
| `internal/middleware/ratelimit_test.go` | 限流器 |
| `internal/server/manager_test.go` | 连接管理和 Redis key 生成 |

## 4. V1 演示证据覆盖情况

已新增：

```text
scripts/demo_core_im.sh
tools/core_im_demo/main.go
```

当前已覆盖：

```text
1. 登录 userA / userB 获取 token。
2. userA / userB 建立 WebSocket。
3. userA 发送单聊消息给 userB。
4. 验证 MySQL messages 落库。
5. userB 回 ACK，验证 pending_ack 清理。
6. userB 断线后 userA 发消息，验证 offline_msg / pending_ack。
7. userB 重连，验证 SyncOfflineMessages 回放。
8. full 栈下创建群组并发送群聊，验证 Kafka / Transfer 链路。
```

## 5. 成功日志关键字

登录：

```text
list conversations failed   # 仅会话列表失败时出现，不应阻断登录
```

WebSocket：

```text
sync pending messages
sync messages after last_seq
```

发消息：

```text
gateway received client message
logic accepted message
message published to gateway
gateway pushed websocket message
message saved for offline delivery
```

ACK：

```text
ack confirmed
ack timeout retry pushed
ack retry exhausted
```

群聊：

```text
group dispatch published
group dispatch consumed
group dispatch scheduled retry
group dispatch moved to dlq
```

红包：

```text
测试通过时证明重复领取和超卖被约束；当前代码没有强依赖业务日志。
```

## 6. 失败情况检查点

| 场景 | 检查点 |
| --- | --- |
| 登录失败 | `users` 表是否有账号，JWT secret 是否一致 |
| WebSocket 401 | token 是否缺失或过期，AuthMiddleware 是否注入 user_id |
| 消息不落库 | `messages` 表结构是否包含 `client_msg_id`、`conversation_id` |
| 消息重复 | 客户端是否复用 `client_msg_id`，MySQL 是否有 `uk_sender_client_msg` |
| 在线不推送 | `route:<uid>` 是否存在，Gateway Pub/Sub 是否有订阅者 |
| 重连不补偿 | `pending_ack:<uid>`、`session_timeline:<session_id>`、`message_payload:<message_id>` 是否存在 |
| 群聊不扩散 | Kafka broker、topic、Transfer 是否启动，`group_members` 是否有成员 |
| 红包重复领取 | `red_packet_claims` 唯一索引是否存在 |

## 7. 每次修改后的固定命令

```bash
make test
make build
docker compose config
```

涉及 Docker 环境时再跑：

```bash
docker compose up --build
curl http://127.0.0.1:8090/healthz
curl http://127.0.0.1:8090/readyz
curl http://127.0.0.1:8090/metrics
curl http://127.0.0.1:9102/metrics
```

## 8. V2 AI 群聊总结

日期：2026-07-05

新增内容：

```text
POST /api/v1/ai/group-summary
internal/ai mock provider
ai_summary_records
linkgo_ai_summary_requests_total
scripts/ai_demo.sh
```

固定验收命令：

```bash
make test
make build
docker compose config
make ai-demo
```

本轮结果：

```text
make test：通过，包含 internal/ai/summary_service_test.go。
make build：通过，生成 bin/gateway、bin/logic、bin/transfer。
docker compose config：通过。
START_STACK=1 make ai-demo：通过。
```

演示说明：

```text
make ai-demo 默认使用 docker-compose.light.yml：
1. 登录 userA。
2. 创建 G_AI_DEMO 群。
3. 写入 3 条演示群消息。
4. 调用 /api/v1/ai/group-summary。
5. 输出 artifacts/ai_summary_demo/ai_summary_response.json。
脚本默认会自动读取 .env.docker-cn 里的镜像配置；如果需要 Docker Hub 原始镜像，可设置 USE_DOCKER_CN=0。
```

成功响应应包含：

```text
summary_id
conversation_id = group:G_AI_DEMO
message_start_seq = 1
message_end_seq = 3
provider = mock
todos 至少 1 条
risks 至少 1 条
```

本轮实际响应摘要：

```text
summary_id = ais_1783183919858_17890a8e
group_id = G_AI_DEMO
conversation_id = group:G_AI_DEMO
message_start_seq = 1
message_end_seq = 3
provider = mock
todos = 3
risks = 1
```

## 6. V3 群聊 Transfer 验收入口

本版新增：

```text
scripts/demo_group_transfer.sh
make group-transfer-demo
docs/VERSION_TASK_TRACKER.csv
docs/PERFORMANCE_AND_EVOLUTION.md
```

固定验收命令：

```bash
make test
make build
docker compose config
bash -n scripts/demo_core_im.sh scripts/demo_group_transfer.sh
```

本轮实际结果：

```text
make test：通过。
make build：通过，生成 bin/gateway、bin/logic、bin/transfer。
docker compose config：通过。
bash -n scripts/demo_core_im.sh scripts/demo_group_transfer.sh：通过。
START_STACK=1 COMPOSE_FILE_PATH=docker-compose.light.yml make core-im-demo：通过。
```

轻量栈 demo 实际结果：

```text
PASS gateway healthz
PASS login userA
PASS login userB
PASS redis ping
PASS mysql ping
PASS websocket connect userA
PASS websocket connect userB
PASS single chat receive + ack
PASS ack clears pending
PASS offline indexes recorded
PASS offline replay + ack
SKIP group chat via kafka transfer
PASS mysql messages persisted
PASS gateway metrics exposed
```

全量链路演示：

```bash
make group-transfer-demo
```

预期结果：

```text
PASS group create
PASS group chat via kafka transfer
PASS mysql messages persisted
PASS gateway metrics exposed
```

说明：

```text
group-transfer-demo 默认使用 docker-compose.yml 启动完整栈，并设置 REQUIRE_TRANSFER=1。
如果 Transfer healthz 不可用，脚本会失败而不是 SKIP，这样能避免把没有 Kafka/Transfer 的演示误当成完整群聊闭环。
本轮没有强行启动 full stack；当前已用 compose config 和脚本语法验证入口，full stack 演示留给下一轮在停止 light stack 后执行。
```

## 7. V4 OpenAI-compatible Provider 验证

本版新增：

```text
internal/ai/openai_provider.go
internal/ai/fallback_provider.go
internal/ai/openai_provider_test.go
AI_BASE_URL / AI_API_KEY / AI_FALLBACK_TO_MOCK
```

验证命令：

```bash
go test ./...
make build
docker compose config
```

实际结果：

```text
go test ./...：通过，包含 internal/ai provider tests。
make build：通过，生成 bin/gateway、bin/logic、bin/transfer。
docker compose config：通过。
```

测试覆盖：

```text
1. OpenAI-compatible provider 请求 /chat/completions。
2. Authorization header 使用 Bearer API key。
3. 解析模型返回的 JSON summary/todos/risks。
4. primary provider 失败时 fallback 到 mock provider。
```

当前边界：

```text
V4 只完成真实模型 provider 接入能力，不强制配置真实 API key。
本地和秋招 demo 默认仍可使用 mock provider，保证演示稳定。
```

## 8. V5 AI 调用审计与指标验证

本版新增：

```text
ai_call_logs
sql/20260707_ai_call_logs.sql
linkgo_ai_provider_latency_seconds
scripts/ai_demo.sh 自动应用新迁移
SummaryService provider success/error audit tests
```

验证命令：

```bash
go test ./...
make build
docker compose config
```

当前已验证：

```text
go test ./internal/ai ./cmd/gateway：通过。
summary_service_test 覆盖 INSERT INTO ai_call_logs success。
summary_service_test 覆盖 provider error 时写入 ai_call_logs error。
```

预期指标：

```text
linkgo_ai_summary_requests_total{provider,result}
linkgo_ai_provider_latency_seconds_bucket{provider,result,le}
linkgo_ai_provider_latency_seconds_sum{provider,result}
linkgo_ai_provider_latency_seconds_count{provider,result}
```

当前边界：

```text
审计日志 best-effort 写入，失败不阻断总结接口。
真实 provider fallback 的 primary failure 还没有 attempt 级拆分记录。
```

## 9. V6 Provider Attempt 与脱敏验证

本版新增：

```text
AttemptRecorder
ai_provider_attempt_logs
sql/20260707_ai_provider_attempt_logs.sql
RedactSensitive
provider attempt audit tests
```

验证命令：

```bash
go test ./internal/ai ./cmd/gateway
```

实际结果：

```text
ok github.com/1084217636/linkgo-im/internal/ai
ok github.com/1084217636/linkgo-im/cmd/gateway
```

测试覆盖：

```text
1. summary success 写入 ai_call_logs 和 ai_provider_attempt_logs。
2. provider error 写入 ai_call_logs 和 ai_provider_attempt_logs。
3. RedactSensitive 会过滤 token/password/Bearer。
```
