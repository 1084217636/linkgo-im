# 模块卡片

每张卡片回答：负责什么、关键结构/函数、谁调用它、依赖哪些表或 Redis key、怎么测试。

## 1. Gateway Route

位置：

```text
cmd/gateway/internal/handler/routes.go
```

职责：

```text
注册 REST API、健康检查、Prometheus 指标和 WebSocket 入口。
```

关键点：

```text
Public: /api/v1/login
Auth: /api/v1/history、好友、群组、红包
Infra: /healthz、/readyz、/metrics
WS: /ws
```

测试：

```text
cmd/gateway/main_test.go
docker compose config
```

## 2. WebSocket Handler

位置：

```text
cmd/gateway/internal/handler/websockethandler.go
```

职责：

```text
校验用户、升级 WebSocket、注册在线路由、同步离线消息、启动读循环。
```

关键函数：

```text
WebSocketHandler
server.RefreshRoute
server.SyncOfflineMessages
server.StartClientLoop
```

Redis key：

```text
route:<uid>
gateway_users:<gatewayId>
gateway_conn:<gatewayId>:<connId>
pending_ack:<uid>
session_timeline:<session_id>
message_payload:<message_id>
```

测试：

```text
internal/server/manager_test.go
scripts/demo_core_im.sh 做真实建连演示
```

## 3. Client Loop

位置：

```text
internal/server/client.go
```

职责：

```text
读取 WebSocket protobuf 帧，处理 ACK、HEARTBEAT 和普通消息。
```

关键函数：

```text
StartClientLoop
pushPool.Submit
AckMessage
RefreshRoute
SyncSessionMessagesAfterSeq
```

异常处理：

```text
protobuf 解码失败 -> 记录 decode_error
写 PONG 失败 -> 退出连接
push queue 满 -> 记录 queue_full
```

## 4. Logic Handler

位置：

```text
internal/logic/handler.go
```

职责：

```text
登录、历史消息、消息编排、幂等、seq、落库、分发。
```

关键函数：

```text
Login
GetHistory
PushMessage
normalizeFrame
reserveClientMessage
nextSequence
saveMessage
deliverPersistedMessage
resolveRecipients
```

表：

```text
users
messages
friend_relations
group_members
```

Redis key：

```text
client_msg:<uid>:<client_msg_id>
seq:<session_id>
```

测试：

```text
internal/logic/handler_test.go
```

## 5. Redis Delivery

位置：

```text
internal/delivery/redis.go
```

职责：

```text
把消息写入待确认集合，并根据在线路由选择实时推送或离线保存。
```

关键函数：

```text
RedisDelivery.Deliver
trackPendingAck
server.MarkOffline
server.ChannelForGateway
```

Redis key：

```text
pending_ack:<uid>
ack_idx:<uid>
ack_retry:<uid>
route:<uid>
offline_msg:<uid>
im_message_push:<gatewayId>
```

失败处理：

```text
Pub/Sub 无订阅者 -> 清理 stale route 并写 offline_msg
Publish 失败 -> 写 offline_msg
```

## 6. ACK / Retry

位置：

```text
internal/server/ack.go
internal/server/retry.go
```

职责：

```text
客户端 ACK 后清理 pending/offline/index；ACK 超时后有限重试。
```

关键函数：

```text
AckMessage
MarkConversationRead
StartPendingRetryLoop
retryGatewayPending
retryOnePending
```

Redis key：

```text
pending_ack:<uid>
offline_msg:<uid>
ack_idx:<uid>
ack_retry:<uid>
user:conversation:read:<uid>
gateway_users:<gatewayId>
```

面试边界：

```text
当前 ACK 是投递确认，不是已读确认。
```

## 7. Offline Sync / Timeline

位置：

```text
internal/server/sync.go
```

职责：

```text
重连后先回放未 ACK 消息，再按 last_seq 补齐会话消息。
```

关键函数：

```text
SyncOfflineMessages
syncPendingMessages
SyncSessionMessagesAfterSeq
RememberSessionMessage
```

Redis key：

```text
pending_ack:<uid>
ack_idx:<uid>
session_timeline:<session_id>
message_payload:<message_id>
```

## 8. Conversation

位置：

```text
internal/logic/conversation.go
```

职责：

```text
登录会话列表、会话最后消息、未读数、read_seq 更新。
```

关键函数：

```text
listConversations
loadConversationsFromRedis
loadConversationsFromDB
updateConversationState
cacheConversationState
persistConversationState
```

表：

```text
conversations
conversation_members
messages
```

Redis key：

```text
user:conversations:<uid>
conversation:last:<conversation_id>
conversation:members:<conversation_id>
user:conversation:read:<uid>
```

测试：

```text
internal/logic/conversation_test.go
```

## 9. AI Group Summary

位置：

```text
internal/ai/
cmd/gateway/internal/logic/aisummarylogic.go
cmd/gateway/internal/handler/aisummaryhandler.go
```

职责：

```text
给企业群聊提供总结、待办提取和风险提取能力；当前 V2 使用 mock provider，先保证业务闭环稳定。
```

关键函数：

```text
AISummaryHandler
AISummaryLogic.Generate
ai.NewSummaryService
SummaryService.Generate
MockProvider.Summarize
```

表：

```text
group_members
messages
ai_summary_records
```

关键校验：

```text
1. 当前 JWT 用户必须是 active 群成员。
2. 只读取 conversation_id = group:<group_id> 且 to_type = group 的消息。
3. message_limit 会被 AI.MaxMessages 截断，避免一次请求读取过多消息。
4. 结果落库保存 summary_id、operator_id、seq 范围、todos_json、risks_json 和 provider。
```

指标：

```text
linkgo_ai_summary_requests_total{provider,result}
```

测试：

```text
internal/ai/summary_service_test.go
make ai-demo
```

## 9. Kafka Transfer

位置：

```text
cmd/logic/internal/svc/kafka_dispatcher.go
cmd/transfer/main.go
```

职责：

```text
群聊消息异步扩散、失败重试、DLQ。
```

关键函数：

```text
PublishGroupDispatch
consumeLoop
deliverGroupRecipient
writeDeadLetter
groupRecipientDedupKey
```

Kafka topic：

```text
group_message_dispatch
group_message_retry
group_message_dlq
```

Redis key：

```text
group_delivery:<message_id>:<recipient>
```

测试：

```text
cmd/transfer/main_test.go
cmd/logic/main_test.go
```

## 10. Red Packet

位置：

```text
internal/logic/redpacket.go
cmd/gateway/internal/logic/redpacketlogic.go
```

职责：

```text
等额红包创建、领取、详情查询。
```

关键函数：

```text
RedPacketService.Create
RedPacketService.Claim
RedPacketService.Detail
loadRedPacketForUpdate
```

表：

```text
red_packets
red_packet_claims
```

并发控制：

```text
SELECT ... FOR UPDATE 锁红包主行；
red_packet_claims(red_packet_id, user_id) 唯一索引防重复领取；
UPDATE 带 remaining_amount / remaining_count 条件防超卖。
```

测试：

```text
internal/logic/redpacket_test.go
```

## 11. AI Provider

位置：

```text
internal/ai/provider.go
internal/ai/mock_provider.go
internal/ai/openai_provider.go
internal/ai/fallback_provider.go
cmd/gateway/internal/svc/servicecontext.go
```

职责：

```text
把 AI 群聊总结的模型调用从业务逻辑中隔离出来。SummaryService 只依赖 Provider 接口，不关心背后是 mock、OpenAI-compatible 还是其他厂商。
```

关键结构：

```text
Provider
ProviderOptions
OpenAICompatibleProvider
FallbackProvider
```

配置：

```text
AI_PROVIDER
AI_MODEL
AI_BASE_URL
AI_API_KEY
AI_TIMEOUT_SECONDS
AI_FALLBACK_TO_MOCK
```

面试价值：

```text
能说明 AI 接入不是把 HTTP 调用写死在业务里，而是通过 provider 抽象、超时和 fallback 控制外部模型的不确定性。
```
