# 核心链路

本文档追 5 条面试最容易问的主链路：登录、建连、发消息、ACK / 离线、群聊扩散。

## 1. 登录链路

```text
POST /api/v1/login
  ↓
cmd/gateway/internal/handler/loginhandler.go
  ↓
cmd/gateway/internal/logic/loginlogic.go
  ↓
LogicRouter.GetClient(ctx, username)
  ↓
api.Logic.Login
  ↓
internal/logic/handler.go Login
  ↓
MySQL users 校验账号密码
  ↓
middleware.GenerateToken(uid)
  ↓
listConversations(uid)
  ↓
返回 token、user_id、最近会话列表
```

涉及表：

```text
users
conversations
conversation_members
messages
```

涉及 Redis：

```text
user:conversations:<uid>
conversation:last:<conversation_id>
user:conversation:read:<uid>
```

失败处理：

```text
用户不存在 -> user not found
密码错误 -> invalid password
会话列表失败 -> 记录日志，不影响登录主流程
```

## 2. WebSocket 建连链路

```text
GET /ws
  ↓
AuthMiddleware 从 JWT 提取 user_id
  ↓
WebSocketHandler
  ↓
LogicRouter.GetClient(ctx, userID)
  ↓
gorilla/websocket Upgrade
  ↓
server.Manager.Add(userID, conn)
  ↓
server.RefreshRoute 写 route:<uid>
  ↓
server.SyncOfflineMessages 回放 pending / timeline
  ↓
server.StartClientLoop 读取客户端消息
```

涉及 Redis：

```text
route:<uid>
gateway_users:<gatewayId>
gateway_conn:<gatewayId>:<connId>
gateway_live:<gatewayId>
pending_ack:<uid>
ack_idx:<uid>
session_timeline:<session_id>
message_payload:<message_id>
```

失败处理：

```text
无 user_id -> 401
Logic client 不可用 -> 503
WebSocket 读失败 -> 连接退出，defer 清理本地连接和 Redis route
```

## 3. 单聊发消息链路

```text
Client WebSocket protobuf WireMessage
  ↓
server.StartClientLoop
  ↓
push worker pool
  ↓
Logic PushMessage
  ↓
proto.Unmarshal
  ↓
normalizeFrame
  ↓
reserveClientMessage(client_msg_id)
  ↓
loadMessageByClientMsgID 防重复
  ↓
buildSessionID
  ↓
validateSendPermission
  ↓
nextSequence(seq:<session_id>)
  ↓
saveMessage(MySQL messages)
  ↓
deliverPersistedMessage
  ↓
RedisDelivery.Deliver
  ↓
trackPendingAck
  ↓
GET route:<target_uid>
  ↓
Publish im_message_push:<gatewayId> 或 MarkOffline
```

涉及表：

```text
messages
friend_relations
conversations
conversation_members
```

涉及 Redis：

```text
client_msg:<uid>:<client_msg_id>
seq:<session_id>
pending_ack:<target_uid>
ack_idx:<target_uid>
ack_retry:<target_uid>
route:<target_uid>
offline_msg:<target_uid>
message_payload:<message_id>
session_timeline:<session_id>
user:conversations:<uid>
conversation:last:<conversation_id>
conversation:members:<conversation_id>
```

成功日志：

```text
gateway received client message
logic accepted message
message published to gateway
gateway pushed websocket message
```

失败处理：

```text
client_msg_id 缺失 -> 拒绝
重复 client_msg_id -> 直接返回，不重新分配 seq
MySQL 唯一索引冲突 -> 加载已有消息并复用
Pub/Sub 无订阅者 -> 清理脏 route，写 offline_msg
```

## 4. ACK / 离线补偿链路

ACK：

```text
Client 发送 MsgType_ACK
  ↓
server.StartClientLoop
  ↓
server.AckMessage(uid, ack_message_id)
  ↓
HGET ack_idx:<uid>
  ↓
MarkConversationRead
  ↓
ZREM pending_ack:<uid>
  ↓
ZREM offline_msg:<uid>
  ↓
HDEL ack_idx:<uid>
  ↓
HDEL ack_retry:<uid>
```

ACK 超时重试：

```text
StartPendingRetryLoop
  ↓
扫描 gateway_users:<gatewayId>
  ↓
检查 route:<uid> 是否仍属于当前 Gateway
  ↓
查 pending_ack:<uid> 中超时消息
  ↓
HINCRBY ack_retry:<uid>
  ↓
重推 WebSocket
  ↓
超过次数 -> MarkOffline
```

重连补偿：

```text
WebSocket 建连成功
  ↓
SyncOfflineMessages
  ↓
先回放 pending_ack:<uid>
  ↓
再按 session_timeline:<session_id> 拉 seq > last_seq
  ↓
读取 message_payload:<message_id>
  ↓
写回 WebSocket
```

边界：

```text
这里的 ACK 是“客户端收到消息”的投递确认，不是完整已读回执。
```

## 5. 群聊扩散链路

```text
群聊 WireMessage
  ↓
Logic PushMessage
  ↓
resolveRecipients
  ↓
saveMessage：群消息 MySQL 只存一行
  ↓
kafkaDispatcher.PublishGroupDispatch
  ↓
Kafka group_message_dispatch
  ↓
Transfer consumeLoop
  ↓
server.RememberSessionMessage
  ↓
对每个 recipient 执行 deliverGroupRecipient
  ↓
group_delivery:<message_id>:<recipient> 幂等
  ↓
RedisDelivery.Deliver
  ↓
失败 -> retry topic
  ↓
多次失败 -> DLQ
```

涉及表：

```text
messages
im_groups
group_members
conversations
conversation_members
```

涉及 Kafka：

```text
group_message_dispatch
group_message_retry
group_message_dlq
```

为什么用 Kafka：

```text
群聊同步扇出会放大 Logic 延迟；Kafka 把“消息入库”和“成员扩散”解耦，Transfer 可以独立扩容，并保留 retry / DLQ。
```

验收说明：

```text
light 栈不包含 Kafka / Transfer，只验证登录、单聊、ACK、离线补偿。
完整群聊扩散使用：
START_STACK=1 REQUIRE_TRANSFER=1 bash scripts/demo_core_im.sh
```
