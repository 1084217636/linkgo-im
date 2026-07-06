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
