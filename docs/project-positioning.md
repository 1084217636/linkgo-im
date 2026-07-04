# LinkGo-IM 项目定位备忘

## 个人背景

- 身份：研二学生，目标投递 2026 暑期实习。
- 项目属性：Go 后端个人实践项目。
- 求职目标：依靠该项目和简历冲刺中大厂/大厂后端实习岗位。

## 项目主线

LinkGo-IM 是一个基于 Go + go-zero 的分布式即时通讯系统，核心不是“做一个聊天 Demo”，而是围绕 IM 后端里的可靠投递、多 Gateway 在线路由、会话顺序、ACK 补偿、断线重连、群聊异步扩散和可观测性做工程化实践。

## 当前最能讲的难点

- 多 Gateway 在线路由：`route:<uid> = gatewayId|connId`，结合 TTL、心跳续期、Gateway 反向索引和 CAS 清理处理动态连接与脏路由。
- JWT 鉴权与长连接建立：HTTP 登录签发 JWT，WebSocket 握手阶段解析 `Authorization Bearer` 或 URL token，鉴权通过后升级长连接。
- 会话级顺序：按 `session_id` 使用 Redis Lua 生成递增 `seq`，单聊和群聊统一按会话维度排序。
- 幂等：客户端上行用 `client_msg_id` 防重复提交，服务端投递用 `message_id` 和 Kafka 收件人级 dedupe 防重复扩散。
- 可靠投递：`pending_ack / ack_idx / ack_retry` 记录未确认消息，ACK 超时有限重试，重连回放 pending。
- last_seq 补偿：基于 `session_timeline:<session_id>` 维护 `seq -> message_id`，客户端重连携带 `session_id + last_seq` 拉取后续消息。
- 群聊扩散：Logic 只负责生成消息和写 Kafka，Transfer 异步消费扩散，失败进入 retry 和 DLQ。
- 群聊存储优化：MySQL 群消息只存一行，Redis `message_payload` 和 `session_timeline` 按消息/会话只写一份，用户侧只维护 pending/offline 指针。
- 可观测性：关键日志带 `trace_id / message_id / seq / gateway_id`，Gateway/Transfer 暴露 Prometheus 指标。

## 当前竞争力判断

这个项目已经超过普通 CRUD/简单 WebSocket Demo，具备后端实习项目里比较有区分度的系统设计点。对于中厂和部分大厂实习面试，项目主线是够用的，前提是能把每个模块的边界、失败场景、数据结构和压测结果讲清楚。

项目还不能包装成“生产级大规模 IM 系统”。当前更合理的定位是：个人主导实现的分布式 IM 后端原型，覆盖核心链路和关键可靠性机制，并通过 Docker 多 Gateway 环境做压测验证。

## 后续优先优化

1. 补真实压测数据：10 Gateway、1w WebSocket、平均延迟、P99、ACK 超时率、CPU/内存峰值。
2. 把 `ack_idx:<uid>` 中的 payload 副本优化成 message_id 指针，重推时优先读 `message_payload`，未命中再查 MySQL。
3. 持久化用户每个会话的 `last_seq`，减少客户端重连参数依赖。
4. 增加更多可靠性测试：重复 client_msg_id、Gateway 重启清理、Kafka retry/DLQ、last_seq 补偿。
5. 准备面试讲稿：登录建连链路、单聊链路、群聊链路、ACK/重试链路、Gateway 宕机链路、Redis key 设计。
