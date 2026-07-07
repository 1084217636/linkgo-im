# 面试问答

## 1. 你这个项目不是普通 IM demo 吗？

不是。我把它定位成企业研发协同 IM 后端，重点不是聊天页面，而是消息系统的工程闭环：

```text
WebSocket 长连接
Gateway / Logic / Transfer 分层
Redis 在线路由
MySQL 最终消息存储
Kafka 群聊异步扩散
ACK / 离线补偿
会话列表 / 未读数
Prometheus 指标
```

后续 AI 助手只是协同场景增强，主线仍是 Go 后端工程能力。

## 2. Gateway、Logic、Transfer 为什么要拆？

Gateway 管连接，Logic 管消息编排，Transfer 管群聊扩散。

```text
Gateway：长连接、心跳、ACK、离线回放
Logic：校验、seq、幂等、落库、会话状态
Transfer：消费 Kafka、按群成员扩散、retry / DLQ
```

这样连接层和业务编排层解耦，群聊扇出也不会阻塞主消息链路。

## 3. 消息怎么保证不重复？

上行和下行分别处理：

```text
上行：client_msg_id + Redis client_msg:<uid>:<client_msg_id> + MySQL uk_sender_client_msg
下行：message_id 唯一；群聊 Transfer 用 group_delivery:<message_id>:<recipient> 做收件人级幂等
```

客户端重试同一条消息时复用 `client_msg_id`，Logic 命中 Redis 或 MySQL 后不会重新分配 `seq`。

## 4. 消息顺序怎么保证？

只保证会话内顺序，不做全局顺序。

```text
session_id -> seq:<session_id> -> Redis Lua INCR
message_id = session_id + seq
MySQL uk_conversation_seq(conversation_id, seq)
session_timeline:<session_id> 保存 seq -> message_id
```

全局顺序没有业务必要，成本也高；IM 展示一般按会话维度排序。

## 5. ACK 是已读吗？

不是。当前 ACK 是投递确认：

```text
客户端收到消息 -> 发送 ACK -> 服务端清理 pending_ack / offline_msg / ack_idx
```

它表示“客户端已收到”，不表示“用户已阅读”。如果要做已读回执，需要额外的 read receipt 协议和状态。

## 6. 离线消息怎么做？

消息投递时先写 `pending_ack` 和 `ack_idx`。如果目标用户不在线，或 Pub/Sub 没有订阅者，就写 `offline_msg`。

重连时：

```text
WebSocket 建连
  ↓
SyncOfflineMessages
  ↓
先回放 pending_ack
  ↓
再用 session_id + last_seq 从 session_timeline 补齐
```

ACK 后会清理 `pending_ack`、`offline_msg`、`ack_idx` 和 `ack_retry`。

## 7. pending_ack 和 offline_msg 有什么区别？

```text
pending_ack：所有已经进入投递流程但还没收到 ACK 的消息。
offline_msg：明确因为离线或推送失败而需要离线补偿的消息索引。
```

ACK 成功后两者都会清理。`pending_ack` 更通用，`offline_msg` 更偏离线补偿标记。

## 8. 为什么 Redis Pub/Sub 不能当可靠队列？

Redis Pub/Sub 没有持久化和消费确认。订阅者不在线时消息会丢。

所以这里只把 Pub/Sub 当在线实时通知通道，可靠性靠：

```text
pending_ack
ack_idx
offline_msg
message_payload
session_timeline
MySQL messages
```

## 9. 群聊为什么用 Kafka？

群聊扩散是扇出操作，成员越多同步耗时越长。如果 Logic 同步扩散，会影响发送延迟。

Kafka 的作用：

```text
消息先落库
群聊扩散异步化
Transfer 可独立扩容
失败可 retry
最终失败进 DLQ
```

## 10. MySQL 和 Redis 分别负责什么？

MySQL 是最终事实来源：

```text
users
messages
conversations
conversation_members
friend_relations
group_members
red_packets
```

Redis 是在线状态、热索引和补偿状态：

```text
route
pending_ack
ack_idx
offline_msg
session_timeline
message_payload
user:conversations
conversation:last
```

Redis 可以丢部分热数据，MySQL 需要保留最终历史。

## 11. Gateway 宕机怎么处理？

在线路由有 TTL：

```text
route:<uid>
gateway_users:<gatewayId>
gateway_conn:<gatewayId>:<connId>
gateway_live:<gatewayId>
```

Gateway 启动时可以清理自己旧的 route。Pub/Sub 推送时如果没有订阅者，也会清理 stale route 并转离线。

## 12. 红包怎么防超卖？

红包领取走 MySQL 事务：

```text
先查 red_packet_claims 是否已领取
事务里 SELECT ... FOR UPDATE 锁 red_packets 主行
计算本次金额
插入 red_packet_claims
UPDATE red_packets 扣 remaining_amount / remaining_count
red_packet_claims(red_packet_id,user_id) 唯一索引兜底重复领取
```

这是项目里的业务亮点，能证明你不只是写消息转发。

## 13. 为什么后续 AI 助手要放在 IM 里？

企业 IM 沉淀的是协同上下文。AI 不应该抢消息系统主线，而应该做结构化复盘：

```text
群聊总结
待办提取
风险点归纳
知识库问答
```

这样 AI 是真实业务增强，不是套壳聊天机器人。

## 14. 面试官问你做了什么，怎么回答？

可以这样讲：

```text
我主要负责把 IM 从单体 demo 升级成 Gateway / Logic / Transfer 分层架构，补齐 WebSocket 长连接、Redis 在线路由、会话级 seq、client_msg_id 幂等、MySQL 历史消息、pending ACK、离线补偿、Kafka 群聊异步扩散和 Prometheus 指标。同时我把红包做成业务亮点，用事务和唯一索引解决并发超卖和重复领取。后续在企业协同场景上接 AI 群聊总结和待办提取。
```

## 15. AI 群聊总结是不是套壳大模型？

不是。当前 V2 的重点不是模型生成文本，而是把 AI 能力接入企业 IM 的业务闭环：

```text
JWT 当前用户
  ↓
群成员权限校验
  ↓
按 group:<group_id> 读取最近消息
  ↓
Provider 生成总结 / 待办 / 风险
  ↓
结果写入 ai_summary_records
  ↓
Prometheus 指标记录成功或失败原因
```

模型只是 provider，业务系统负责权限、上下文、审计、超时和演示闭环。

## 16. 为什么 V2 先用 mock provider？

因为秋招项目要先证明工程闭环稳定：

```text
没有 API key 也能演示
单元测试结果确定
接口结构稳定
后续真实模型只替换 Provider
```

真实模型接入会增加网络、成本、限流和敏感信息问题，适合放在 V3。

## 17. AI 总结怎么保证不越权？

触发接口时不相信客户端传用户 ID，而是从 JWT 上下文取当前用户。服务端查询 `group_members`，只有 `status = active` 且未禁言过期的成员才能读取该群最近消息并生成总结。

## 18. AI 结果怎么审计？

每次总结都会写入 `ai_summary_records`：

```text
summary_id
group_id
conversation_id
operator_id
message_start_seq / message_end_seq
summary
todos_json
risks_json
provider
created_at
```

这样能追溯是谁在什么时候总结了哪一段群聊消息。

## 19. V4 接真实模型后，为什么还保留 mock？

因为真实模型是不稳定外部依赖，可能遇到无 API key、网络超时、限流、成本限制和返回格式不稳定。项目里把模型封装成 `Provider`：

```text
SummaryService -> Provider interface -> mock / openai-compatible / fallback
```

默认 demo 仍可用 mock，配置 `AI_PROVIDER=openai-compatible`、`AI_BASE_URL`、`AI_API_KEY` 后才接真实模型。这样既能演示工程闭环，也能说明生产里如何做降级。

## 20. 真实模型调用会不会拖慢 IM？

不会进入 WebSocket 实时消息链路。AI 总结是独立 HTTP 接口，只读取已落库的群聊消息，然后写 `ai_summary_records`。实时消息仍走 Gateway / Logic / Redis / Kafka / Transfer，不依赖模型返回。

## 21. V5 为什么要做 ai_call_logs？

因为只保存总结结果还不够。真实模型调用有超时、限流、成本和稳定性问题，所以 V5 额外记录：

```text
provider
message_count
seq 范围
duration_ms
status
error_message
operator_id
```

这样可以回答：

```text
哪类 provider 慢？
失败是不是集中在某个模型？
一次总结用了多少条消息？
失败时用户是谁、群是什么、覆盖哪段消息？
```

## 22. ai_call_logs 写失败怎么办？

当前是 best-effort，不阻断总结主流程。理由是 AI 总结本身是用户可见功能，审计表临时故障不应该让接口完全不可用。但这个边界要说清楚：生产上可以把失败日志打到错误日志或消息队列，后续异步补偿。

## 23. V6 为什么还要 ai_provider_attempt_logs？

因为 fallback 场景下，一个用户请求可能经历多次 provider 尝试：

```text
openai-compatible -> error
mock -> success
```

如果只看最终 summary，会以为 provider 是 `openai-compatible:fallback:mock`，但看不到 primary 失败耗时和原因。attempt 表可以按 `call_id + attempt_order` 还原全过程，便于排查模型超时、限流和降级频率。

## 24. AI 错误日志怎么避免泄露密钥？

V6 增加了基础脱敏：

```text
token=...
password=...
api_key=...
Bearer ...
```

这些进入 `ai_call_logs` 或 `ai_provider_attempt_logs` 前会替换成 `[REDACTED]`。当前还是基础正则，不是完整 DLP，面试时要承认这个边界。
