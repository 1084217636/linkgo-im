# 13 代码所有权与核心结构体

面试官问结构体，是在判断项目是否由你真正掌握。先记“位置、职责、关键字段、方法”，不要背生成代码每一行。

| 结构体 | 位置 | 必背职责/字段 |
| --- | --- | --- |
| `api.WireMessage` | `api/protocol.proto` | WS/gRPC 消息协议；message/client ID、发送者、目标、类型、内容、seq |
| `ClientConn` | `internal/server/manager.go` | 单个 WS 连接；uid、conn、发送通道/连接身份 |
| `ClientManager` | 同上 | 本机 uid 到连接管理，查找、注册、移除 |
| `PushWorkerPool` | `internal/server/pool.go` | UID 分片有界队列、提交、处理、关闭 |
| Gateway `ServiceContext` | `cmd/gateway/internal/svc` | Redis、DB、LogicRouter、限流、AI、GameOps 依赖注入 |
| `LogicRouterPool` | 同上 | zRPC Logic client 和 readiness |
| Logic `ServiceContext` | `cmd/logic/internal/svc` | DB、Redis、Kafka dispatcher 等 Logic 依赖 |
| `RedisDelivery` | `internal/delivery/redis.go` | pending、route、Pub/Sub、offline 投递 |
| `groupDispatchJob` | Logic/Transfer | Kafka 群任务消息模型 |
| `ActivityVersion` | `internal/gameops/activity.go` | activity/version/status/config/rollout/creator/approver |
| `ActivityService` | 同上 | 草稿、提交、审批、发布、回滚、Outbox |
| `GrantRequest` | `internal/gameops/grant.go` | grant_request_id 与批量道具项 |
| `GrantService` | 同上 | 幂等发放、库存、失败审计、结果查询 |

## 五条必须定位的入口

1. REST 注册：`cmd/gateway/internal/handler/routes.go`。
2. WS 建连：`websockethandler.go`。
3. 消息核心：`internal/logic/handler.go`。
4. Redis 投递：`internal/delivery/redis.go`。
5. Kafka 消费：`cmd/transfer/main.go`。

## 学习方法

每次只选一个结构体，手写：它谁创建、被谁持有、三个关键字段、两个方法、一个失败路径。能从入口沿调用链找到它，才算掌握。

## 红线

不知道字段时不要编。回答“我记得职责和关键字段，我会从某入口定位到某文件”比虚构更好；但上述 13 个必须最终闭卷掌握。
