# 核心业务调用链

文档对应提交：`40b8b5b`
生成或最后校验时间：2026-08-04
适用分支：`main`

## 单聊与 ACK（第一主链）

```text
WebSocket 客户端
→ Gateway handler
→ Logic/RPC
→ message_id 与 session seq
→ MySQL 历史写入
→ Redis 在线路由
→ 本地或跨 Gateway 投递
→ 客户端 ACK
→ 清理 pending，必要时重连补偿
```

阅读顺序：`cmd/gateway/internal/handler/websockethandler.go` → `cmd/logic/internal/logic/pushmessagelogic.go` → `internal/delivery/redis.go` → `internal/server/ack.go`。每读一段都记录输入、输出、状态变化、同步/异步边界和失败返回。

## 群聊 Kafka 链

```text
Logic 解析成员并生产 Kafka 任务
→ Transfer consumer
→ 成员级 processing/done 租约
→ Redis 通知目标 Gateway
→ Gateway 推送与 ACK
```

重点理解重复消费、offset 提交时机、DLQ/retry 和局部顺序；不要把 Kafka 任务描述成“天然只执行一次”。

## 红包与 AI

红包阅读 `internal/logic/redpacket.go` 及其测试，重点看幂等、Redis 原子操作和 MySQL 事务边界；AI 阅读 `internal/logic/bot.go`、`internal/logic/conversation.go`，明确普通消息链复用了哪些部分、模型失败如何兜底，以及当前 mock 与真实模型的边界。
