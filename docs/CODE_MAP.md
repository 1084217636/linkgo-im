# 代码地图

本文档用于先把项目跑通、看懂，再进入功能开发。当前项目定位：

```text
基于 Go-Zero 的企业研发协同 IM 与 AI 助手平台
```

## 1. 服务分层

| 服务 | 目录 | 职责 |
| --- | --- | --- |
| Gateway | `cmd/gateway/` | REST API、JWT 鉴权、WebSocket 建连、心跳、ACK、离线回放、转发消息到 Logic |
| Logic | `cmd/logic/` + `internal/logic/` | 登录校验、消息编排、seq 分配、幂等、MySQL 落库、会话状态更新、群聊任务发布 |
| Transfer | `cmd/transfer/` | 消费 Kafka 群聊扩散任务，按成员投递，处理 retry 和 DLQ |
| Delivery | `internal/delivery/` | 写 pending_ack / ack_idx / ack_retry，按 route 定向 Pub/Sub，失败写 offline_msg |
| Server Runtime | `internal/server/` | WebSocket 连接管理、Redis 路由、ACK、离线同步、session timeline |
| Metrics | `internal/metrics/` | Prometheus 指标 |

## 2. 入口文件

| 入口 | 文件 | 说明 |
| --- | --- | --- |
| Gateway 启动 | `cmd/gateway/main.go` | 加载配置，初始化 ServiceContext，注册 REST / WS / health / metrics |
| REST 路由 | `cmd/gateway/internal/handler/routes.go` | 手工维护 go-zero 路由，包含 `/login`、`/history`、好友、群组、红包、`/ws` |
| WebSocket | `cmd/gateway/internal/handler/websockethandler.go` | JWT user_id、获取 Logic client、升级 WS、写 Redis route、回放离线消息 |
| Logic RPC | `cmd/logic/main.go` | 启动 zRPC，注入 Redis、MySQL、Kafka dispatcher |
| Logic 实现 | `internal/logic/handler.go` | `PushMessage`、`Login`、`GetHistory` 核心逻辑 |
| Transfer 启动 | `cmd/transfer/main.go` | 消费 Kafka topic / retry topic，失败写 DLQ |

## 3. 核心代码路径

```text
api/
  protocol.proto                 # WebSocket / gRPC 共用消息协议
  gateway.api                    # Gateway REST API 声明

cmd/gateway/
  internal/handler/              # HTTP handler
  internal/logic/                # Gateway REST logic，主要转调 zRPC 或本地服务
  internal/middleware/           # Auth / RateLimit
  internal/svc/                  # Redis、MySQL、LogicRouter、限流器等依赖

cmd/logic/
  internal/svc/                  # Logic ServiceContext，Kafka dispatcher
  internal/server/               # zRPC server wrapper

internal/
  logic/                         # IM 业务核心：消息、会话、好友、群、红包
  server/                        # 连接、路由、ACK、离线补偿、timeline
  delivery/                      # Redis 投递实现
  middleware/                    # JWT 与令牌桶
  metrics/                       # Prometheus 指标
```

## 4. MySQL 表

| 表 | 作用 |
| --- | --- |
| `users` | 登录用户和账号密码 |
| `messages` | 最终历史消息，含 `message_id`、`client_msg_id`、`conversation_id`、`seq` |
| `conversations` | 会话元信息，保存 `last_seq`、`updated_at` |
| `conversation_members` | 用户会话关系，保存 `read_seq` |
| `friend_requests` | 好友申请 |
| `friend_relations` | 双向好友关系 |
| `im_groups` | 群组元信息 |
| `group_members` | 群成员与角色 |
| `red_packets` | 红包主表 |
| `red_packet_claims` | 红包领取记录，唯一索引防止重复领取 |

## 5. Redis Key

| Key | 类型 | 作用 |
| --- | --- | --- |
| `route:<uid>` | String | 用户当前连接在哪个 Gateway，值为 `gatewayID|connID` |
| `gateway_users:<gatewayId>` | Set | Gateway 反向索引，用于心跳和 ACK 重试扫描 |
| `gateway_conn:<gatewayId>:<connId>` | String | Gateway 连接反查用户 |
| `gateway_live:<gatewayId>` | String | Gateway 心跳 |
| `client_msg:<uid>:<client_msg_id>` | String | 上行发送幂等 |
| `seq:<session_id>` | String | 会话级递增 seq |
| `pending_ack:<uid>` | ZSet | 等待客户端 ACK 的消息 ID |
| `ack_idx:<uid>` | Hash | `message_id -> base64(protobuf payload)` |
| `ack_retry:<uid>` | Hash | `message_id -> retry_count` |
| `offline_msg:<uid>` | ZSet | 离线消息索引 |
| `message_payload:<message_id>` | String | 消息 payload 热缓存 |
| `session_timeline:<session_id>` | ZSet | `seq -> message_id`，用于 last_seq 补偿 |
| `user:conversations:<uid>` | ZSet | 用户最近会话 |
| `conversation:last:<conversation_id>` | Hash | 会话最后一条消息摘要 |
| `conversation:members:<conversation_id>` | Set | 会话成员缓存 |
| `user:conversation:read:<uid>` | Hash | 用户每个会话已确认 seq |
| `group_delivery:<message_id>:<recipient>` | String | 群聊收件人级幂等 |

## 6. 下一步阅读顺序

```text
1. cmd/gateway/internal/handler/routes.go
2. cmd/gateway/internal/handler/websockethandler.go
3. internal/server/client.go
4. internal/logic/handler.go
5. internal/delivery/redis.go
6. internal/server/ack.go
7. internal/server/sync.go
8. internal/logic/conversation.go
9. cmd/logic/internal/svc/kafka_dispatcher.go
10. cmd/transfer/main.go
```
