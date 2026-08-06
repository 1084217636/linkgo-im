# 当前模块速查卡

> 这不是学习顺序。只有学完 [`docs/handbook/19_COMPLETE_CODE_WALK.md`](../handbook/19_COMPLETE_CODE_WALK.md) 后，才用本页快速找代码。每张卡只记录当前实现，不把演进方案写成已完成。

## Gateway 接入

```text
入口：cmd/gateway/main.go
路由：cmd/gateway/internal/handler/routes.go
WebSocket：cmd/gateway/internal/handler/websockethandler.go
依赖装配：cmd/gateway/internal/svc/servicecontext.go
Logic 客户端：cmd/gateway/internal/svc/logicrouter.go
```

职责：HTTP、`/ws`、JWT/Origin、连接注册、上行转发、下行写 WebSocket。Logic 实例从 Etcd 发现并由 zRPC `p2c_ewma` 选择；不是一致性哈希。好友、群组、红包和部分 AI HTTP 代码当前也在 Gateway 进程访问 MySQL。

## WebSocket 连接运行时

```text
连接管理：internal/server/manager.go
单连接读循环：internal/server/client.go
UID 分片队列：internal/server/pool.go
Redis route：internal/server/route.go
ACK：internal/server/ack.go
短期重连回放：internal/server/sync.go
ACK 重试：internal/server/retry.go
```

当前 `uid -> connection` 和 `route:<uid>` 都是单值，不是完整多端模型。重连自动读取 `pending_ack + ack_idx`；`offline_msg` 只是标记，MySQL 历史另走 HTTP 接口。

## Logic 消息核心

```text
入口：internal/logic/handler.go
会话状态：internal/logic/conversation.go
发送关系权限：internal/logic/relations.go 的 validateSendPermission
好友 REST：cmd/gateway/internal/logic/friendlogic.go 的 Apply / Respond
建群 REST：cmd/gateway/internal/logic/groupcreatelogic.go 的 Create
群成员 REST：cmd/gateway/internal/logic/groupmemberslogic.go 的 List
红包：internal/logic/redpacket.go
```

`PushMessage` 完成协议归一化、权限、`client_msg_id` 幂等、Lua seq、同步写 `messages` 和投递。会话摘要/成员元信息是异步尽力更新，不与消息正文处在同一事务。

## RedisDelivery

```text
实现：internal/delivery/redis.go
状态：pending_ack、ack_idx、ack_retry、offline_msg
路由：route:<uid>
频道：im_message_push:<gateway_id>
```

Redis Pub/Sub 只做实时通知。当前 `ack_idx` 保存完整 Base64 Protobuf payload；不能描述成所有用户状态都只保存 message ID。

## Kafka Transfer

```text
生产：cmd/logic/internal/svc/kafka_dispatcher.go
消费：cmd/transfer/main.go
主题：group_message_dispatch / retry / dlq
幂等：group_delivery:<message_id>:<recipient>
```

Logic 同步解析群成员并把 recipients 放入任务；Transfer 逐收件人投递，使用 owner/lease/done 状态、retry/DLQ 和手动提交 offset。它没有重新查询成员，也没有按 Gateway 聚合批量投递。

## 数据和协议

```text
Protobuf：api/protocol.proto
HTTP 契约：api/gateway.api
建表与种子数据：sql/init.sql
迁移：sql/*.sql
```

MySQL 是历史和关系的最终来源；Redis 是路由、短期投递状态、seq 和热点索引；Kafka 是群聊投递任务通道；Etcd 只做 Logic 注册发现。

## AI

```text
Provider：internal/ai/provider.go
默认 mock：internal/ai/mock_provider.go
兼容模型调用：internal/ai/openai_provider.go
知识检索：internal/ai/knowledge_base.go
运行时语料：docs/knowledge/
```

FAQ 是关键词/二元词轻量召回，不是向量数据库 RAG。AI 私聊回复由 Logic 内非持久 goroutine 触发；Logic 崩溃可能丢任务。群聊总结是独立 HTTP 能力，当前没有群内 `@AI` 自动回复。

## 红包

```text
业务：internal/logic/redpacket.go
表：red_packets / red_packet_claims
并发：MySQL 事务 + SELECT FOR UPDATE + 唯一索引
```

当前是等额红包账本，没有钱包扣款、入账、退款、资金流水和对账；网页发送的是普通文本提示，不是结构化红包消息类型。

## 测试与部署

```text
单元测试：go test ./...
网页契约：make frontend-static-check
文档结构：make docs-check
Compose：docker-compose*.yml
K8s：deploy/k8s/
监控：deploy/observability/
CI：.github/workflows/ci.yml
```

Kubernetes production overlay 是可渲染的应用工作负载样例，不是实际生产部署证明。
