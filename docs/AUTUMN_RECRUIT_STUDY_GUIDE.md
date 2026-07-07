# Autumn Recruit Study Guide

这个项目按 Go 后端主项目准备，学习目标不是背功能，而是能解释每条链路的工程取舍。

## 第一阶段：先吃透 IM 主链路

优先学习：

```text
1. Go 基础：goroutine、channel、context、interface、错误处理。
2. WebSocket：握手、心跳、读写协程、连接关闭、弱网重连。
3. Redis：route:<uid>、pending_ack、offline_msg、ack_idx、Pub/Sub。
4. MySQL：messages、conversations、group_members、ai_summary_records。
5. gRPC/go-zero：Gateway 调 Logic，服务发现，配置管理。
```

能讲清楚：

```text
用户怎么登录？
WebSocket 怎么鉴权？
消息怎么从 userA 到 userB？
ACK 失败后如何补偿？
Redis 和 MySQL 分别承担什么角色？
```

## 第二阶段：再学分布式和可靠性

优先学习：

```text
1. Kafka 为什么用于群聊 fanout。
2. Transfer 为什么可以独立扩容。
3. retry / DLQ / 幂等 key 的作用。
4. Prometheus 指标怎么定位瓶颈。
5. 压测时看 QPS、P95/P99、错误率、Redis/MySQL/Kafka 指标。
```

面试重点：

```text
不要只说“用了 Kafka”，要说同步群发会拖慢 Logic，所以把群聊扩散拆到 Transfer，失败可重试，消费者可横向扩容。
```

## 第三阶段：最后学 AI 接入

优先学习：

```text
1. Provider 抽象：mock / openai-compatible / fallback。
2. Timeout、fallback、审计和敏感信息风险。
3. AI 为什么不能阻塞实时消息链路。
4. 群聊总结如何做权限校验和结果留痕。
5. ai_call_logs 和 provider latency 指标如何定位模型问题。
```

面试重点：

```text
AI 是业务增强，不是项目主体。主体仍然是 Go 后端 IM、可靠投递和分布式链路。
```

## 推荐复习顺序

```text
第 1-2 天：跑 core-im-demo，按 TEST_EVIDENCE 画登录/建连/单聊/ACK 流程图。
第 3-4 天：读 CORE_LINKS 和 MODULE_CARDS，重点看 Redis key 和 MySQL 表。
第 5 天：读 Kafka/Transfer，准备“为什么群聊不用同步 for 循环”的回答。
第 6 天：读 AI provider，准备“为什么不是套壳大模型”的回答。
第 7 天：按 INTERVIEW_QA 自问自答一遍。
```
