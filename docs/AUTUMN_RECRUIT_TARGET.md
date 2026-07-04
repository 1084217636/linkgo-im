# Enterprise IM AI 秋招目标文档

下一步文件级修改计划见 [NEXT_IMPLEMENTATION_PLAN.md](NEXT_IMPLEMENTATION_PLAN.md)。

## 1. 项目定位

本项目由原 `linkgo-im` 收敛升级而来，最终简历名称建议：

```text
Go 企业级 IM / 协同通信系统
```

核心叙事：

```text
面向企业内部协同场景，基于 Go + go-zero 实现分布式即时通信系统，支持 WebSocket 长连接、消息可靠投递、群聊异步扩散、离线补偿、限流、监控和压测；在消息链路上接入轻量 AI 助手，实现群聊总结和待办提取。
```

这个项目是秋招主项目，重点证明 Go 后端工程能力，不把重点放在“AI 写代码”。

## 2. CodeRepair 归并方式

原 `CodeRepair` 不再作为独立简历项目保留，它的价值被归并为本项目的 AI 助手设计输入：

| CodeRepair 原能力 | 在本项目中的落点 |
| --- | --- |
| 任务状态机 | AI 总结任务记录：CREATED / RUNNING / SUCCEEDED / FAILED |
| RAG / 上下文召回 | 群聊消息窗口、知识库文档、历史摘要召回 |
| LLM Provider 抽象 | `MockProvider` + 可扩展真实模型 Provider |
| 产物留痕 | 保存 summary、todo、risk、prompt、model_response |
| 测试闭环思路 | AI 结果结构化校验，保证接口无 key 也能演示 |

不要把 CodeRepair 的自动改代码逻辑塞进 IM 系统。IM 项目里的 AI 只做企业协同增强：

```text
群聊总结
待办提取
知识库问答
消息语义搜索
```

第一阶段只实现前两个。

## 3. 秋招水平验收目标

### 3.1 消息系统主链路

必须能讲清楚并有代码支撑：

```text
登录签发 JWT
WebSocket 建连
Redis 在线路由
Gateway -> Logic gRPC 转发
client_msg_id 发送幂等
会话级 seq 分配
MySQL 消息落库
Redis pending_ack
ACK 清理
离线消息回放
群聊 Kafka 异步扩散
Transfer 消费幂等
retry / dead-letter
Prometheus 指标
```

### 3.2 业务亮点

至少保留并讲透一个非消息基础业务：

```text
红包：MySQL 事务 + 行锁 + 唯一索引，解决超卖和重复领取。
```

备选业务亮点：

```text
好友关系
群组管理
最近会话与未读数
```

### 3.3 AI 助手闭环

第一阶段目标：

```text
POST /api/v1/ai/group-summary
  输入：group_id、message_limit
  处理：拉取最近 N 条群消息
  输出：summary、todos、risks、message_range
  存储：summary_records
```

接口必须支持 mock provider：

```text
没有 API key 也能跑通演示。
```

后续可扩展真实 provider：

```text
OpenAI
SiliconFlow
Ollama
```

### 3.4 工程验收

必须维护这些命令可用：

```bash
make test
make build
docker compose config
```

有条件时继续补：

```bash
docker compose up --build
curl http://127.0.0.1:8090/healthz
curl http://127.0.0.1:9102/metrics
```

## 4. 第一轮开发任务

按优先级做：

```text
1. 新增 AI 助手领域模型：summary record、todo item、risk item。
2. 新增 mock LLM provider，返回稳定结构化结果。
3. 新增群聊总结接口。
4. 从历史消息或 Redis timeline 拉取最近消息。
5. 保存 summary_records 到 MySQL。
6. 给 summary service 写单元测试。
7. README 补上 AI 助手链路和演示脚本。
```

暂时不做：

```text
复杂向量数据库
多智能体
自动改代码
大 UI
```

## 5. 简历表述

```text
- 基于 Go + go-zero 设计并实现企业级 IM 系统，拆分 Gateway、Logic、Transfer 服务，支持 WebSocket 长连接、gRPC 调用、Etcd 服务发现、Redis 在线路由和 Kafka 群聊异步扩散。
- 设计消息可靠投递链路，通过 client_msg_id 幂等、会话级 seq、pending ACK、离线补偿和历史消息回放保证弱网和断线重连场景下的消息可恢复。
- 引入 Redis Lua 分配会话序列号，结合 MySQL 唯一索引和 Kafka 消费幂等解决重复发送、重复扩散和顺序补偿问题。
- 接入 Prometheus 指标、健康检查、Docker Compose / K8s 部署清单和压测脚本，沉淀连接数、消息吞吐、ACK 重试、Kafka retry 等可观测指标。
- 在企业协同场景中接入 AI 助手能力，支持群聊总结和待办提取，并通过 mock provider 保证本地无模型 key 也可完整演示。
```

## 6. 面试讲法

面试官问为什么加 AI：

```text
IM 系统天然沉淀大量协同消息，企业用户真正需要的不是聊天机器人，而是把群聊内容结构化成会议纪要、待办和风险点。所以我把 AI 能力放在消息链路的后处理和复盘上，不影响核心消息可靠投递。
```

面试官问 CodeRepair 怎么处理：

```text
原来的 CodeRepair 更像个人研发工具，不适合作为 Go 后端主项目。我把它里面的任务状态机、Provider 抽象、RAG 召回和产物留痕思路归并到 IM 的 AI 助手模块里，项目叙事更聚焦。
```
