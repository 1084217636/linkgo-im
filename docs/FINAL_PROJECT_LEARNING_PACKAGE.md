# 项目一最终学习包：LinkGo Chat

## 1. 项目定位

项目名：

```text
LinkGo Chat：AI 好友与红包协同 IM 系统
```

一句话介绍：

```text
我基于 Go + go-zero 实现企业级 IM 后端，围绕长连接、消息可靠性、群聊异步扩散、离线补偿、可观测性和 AI 协同增强做了完整工程闭环。
```

这个项目不要讲成：

```text
仿微信聊天 demo。
```

应该讲成：

```text
企业研发协同场景里的消息基础设施 + 轻量 AI 助手。
```

## 2. 项目主线

真正的主线一直是 Go 后端工程能力：

```text
登录
  ↓
WebSocket 建连
  ↓
单聊 / 群聊
  ↓
MySQL 落库
  ↓
ACK / 离线补偿
  ↓
Kafka + Transfer 群聊异步扩散
  ↓
Prometheus 指标和演示脚本
```

AI 是业务增强，不抢主线：

```text
群聊总结
知识库问答
```

## 3. 为什么这个项目像公司项目

因为它不是“能发消息就算完”，而是有明确的工程边界和演进过程：

```text
Gateway / Logic / Transfer 分层
Redis 在线路由与 ACK 补偿
MySQL 最终事实存储
Kafka 群聊削峰
Etcd 服务发现
JWT + 令牌桶限流
Prometheus 指标
Docker Compose 演示入口
AI provider / fallback / 审计 / 脱敏 / FAQ-RAG
```

## 4. 最终闭环

完整闭环：

```text
user login
  ↓
JWT
  ↓
/ws 建连
  ↓
Gateway 收消息
  ↓
Logic 生成 session_id / seq / message_id
  ↓
MySQL messages 落库
  ↓
Redis 在线投递 / pending_ack / offline_msg
  ↓
ACK 清理与重放
  ↓
group message -> Kafka -> Transfer -> recipient fanout
  ↓
AI summary / AI ask 从落库消息或项目文档读取上下文
  ↓
ai_summary_records / ai_qa_records / ai_call_logs / ai_provider_attempt_logs 留痕
```

## 5. 版本演化

| 版本 | 目标 | 解决的瓶颈 |
| --- | --- | --- |
| V0 | 跑通项目和代码地图 | 原项目功能散，叙事不清晰 |
| V1 | 核心 IM 链路证据化 | 登录、建连、单聊、ACK 没有稳定演示入口 |
| V2 | AI 群聊总结闭环 | AI 只有想法，没有接口和留痕 |
| V3 | 群聊 Transfer 验收入口 | 轻量栈不能证明 Kafka fanout |
| V4 | OpenAI-compatible provider | mock provider 无法解释真实模型接入 |
| V5 | AI 调用审计与延迟指标 | 模型调用不可观察、不可复盘 |
| V6 | provider attempt 与脱敏 | fallback 内部过程不可见，错误日志有泄露风险 |
| V7 | FAQ/RAG 问答闭环 | AI 只有总结，没有知识问答 |
| V8 | 最终收口 | demo、简历、面试材料分散，不方便学习 |

## 6. 核心代码地图

主链路重点目录：

```text
cmd/gateway/        登录、HTTP API、WebSocket、JWT、限流
cmd/logic/          消息编排、会话 seq、落库、群聊生产
cmd/transfer/       Kafka 消费、群聊 fanout、retry / DLQ
internal/server/    WebSocket 管理、ACK、重放、在线路由
internal/logic/     消息、会话、红包等业务逻辑
internal/ai/        summary / ask / provider / audit / FAQ-RAG
internal/metrics/   Prometheus 指标
sql/                初始化和迁移脚本
scripts/            demo 与 smoke 入口
```

AI 重点文件：

```text
internal/ai/summary_service.go
internal/ai/ask_service.go
internal/ai/knowledge_base.go
internal/ai/openai_provider.go
internal/ai/fallback_provider.go
internal/ai/redact.go
internal/ai/attempt.go
```

## 7. 你要掌握的基础知识

核心技术栈：

```text
Go / goroutine / channel / context / interface
go-zero REST / zRPC
WebSocket
gRPC + Etcd
Redis / Lua / PubSub
MySQL 事务和索引
Kafka / retry / dead-letter
Prometheus 指标
JWT / rate limit
Docker Compose
AI provider / fallback / audit / FAQ-RAG
```

面试最该背的不是 API，而是这些问题：

```text
1. Gateway、Logic、Transfer 为什么拆？
2. ACK 和已读回执有什么区别？
3. 为什么 Redis Pub/Sub 不能当可靠队列？
4. 群聊为什么要 Kafka？
5. 为什么 AI 不进实时消息链路？
6. 为什么 V7 先做 FAQ/RAG，不先接向量库？
7. ai_call_logs、ai_provider_attempt_logs、ai_qa_records 各解决什么问题？
```

## 8. 当前可演示入口

标准命令：

```bash
make core-im-demo
make ai-demo
make ai-ask-demo
make im-ai-final-demo
```

最推荐的学习入口：

```bash
START_STACK=1 make im-ai-final-demo
```

这个脚本会按顺序执行：

```text
core IM demo
AI group summary demo
AI knowledge ask demo
```

## 9. 当前边界和升级路线

当前边界：

```text
1. light stack 默认不证明 Kafka Transfer，完整 fanout 仍需 full compose。
2. FAQ/RAG 还是关键词召回，不是向量索引。
3. AI 审计已有留痕，但没有 token/cost 字段。
4. 脱敏是基础正则，不是完整 DLP。
```

生产化升级路线：

```text
1. knowledge 检索升级为 BM25 / embedding / 代码符号级索引。
2. AI 增加 token/cost、provider 限流、知识库热更新。
3. 更细粒度的群权限和资料权限控制。
4. 压测补齐 Gateway、Transfer、AI ask 的性能基线。
```

## 10. 学习顺序

推荐这样学：

```text
第 1 天：读 README、CODE_MAP、CORE_LINKS。
第 2 天：跑 core-im-demo，理解登录、建连、单聊、ACK。
第 3 天：读 internal/server 和 internal/logic，画 Redis key / MySQL 表关系。
第 4 天：读 Transfer 和 Kafka 链路，准备群聊异步扩散的回答。
第 5 天：读 AI summary provider、audit、attempt。
第 6 天：读 AI ask、KnowledgeBase、AI_FAQ，跑 ai-ask-demo。
第 7 天：读 FINAL_RESUME_AND_INTERVIEW_PACK.md，做 1 分钟和 3 分钟表达。
```

## 11. 最推荐你背熟的一段话

```text
这个项目不是普通聊天 demo，我把它做成了企业研发协同场景里的消息基础设施。主线是 Gateway、Logic、Transfer 三层分离，解决跨节点消息路由、会话有序性、ACK 补偿、离线回放和群聊异步扩散。AI 部分只作为协同增强，围绕群聊总结和项目知识问答做了 provider 抽象、fallback、审计留痕和 FAQ/RAG 闭环，不影响实时消息链路。
```
