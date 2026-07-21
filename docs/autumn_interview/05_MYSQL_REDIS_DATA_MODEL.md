# 05 MySQL、Redis 与数据模型

## 1. 总原则

> MySQL 保存最终业务事实，Redis 保存高频在线状态、过程状态和热点索引。Redis 可以丢失后重建的热数据不应成为唯一历史来源。

部署口径：多台 Gateway、Logic、Transfer 连接同一套共享中间件。`REDIS_ADDR` 指向托管 Redis 或 Sentinel/代理提供的 HA 稳定入口；`DB_DSN` 指向 MySQL primary/proxy 稳定入口。不能解释成每台应用服务器各装一份互不相通的 Redis/MySQL。

当前版本没有原生 Redis Cluster 分片和应用层 MySQL 读写分离。MySQL 主从复制、故障切换可位于稳定入口之后；所有事务写和强一致读仍走主入口。这是已实现能力边界，不要为了“公司级”而夸大。

## 2. MySQL 核心表

### IM

- `users`：账号、密码哈希、用户信息。
- `messages`：最终消息历史，包含业务 ID、会话、seq、发送者、接收目标和内容。
- `conversations`：会话元信息与最新 seq。
- `conversation_members`：会话成员与进度。
- `im_groups`、`group_members`：群和成员角色。
- `friend_requests`、`friend_relations`：好友关系。

### 红包

- `red_packets`：总金额、数量、剩余量、状态。
- `red_packet_claims`：领取明细，唯一约束防止同用户重复领取。

### AI

- 总结、问答、调用日志和 provider attempt 表用于结果留痕与故障复盘。

## 3. Redis Key 分类

### 在线路由

- `route:<uid>`
- `gateway_users:<gatewayId>`
- `gateway_conn:<gatewayId>:<connId>`
- `gateway_live:<gatewayId>`

### 消息可靠性

- `client_msg:<uid>:<client_msg_id>`：上行幂等。
- `seq:<session_id>`：会话 seq。
- `pending_ack:<uid>`：未 ACK。
- `ack_idx:<uid>`：ACK payload/索引。
- `ack_retry:<uid>`：重试次数。
- `offline_msg:<uid>`：离线索引。
- `message_payload:<message_id>`：共享 payload 热缓存。
- `session_timeline:<session_id>`：seq 到 message_id。

### 会话热点

- `user:conversations:<uid>`
- `conversation:last:<conversation_id>`
- `conversation:members:<conversation_id>`
- `user:conversation:read:<uid>`

### 群聊幂等

- `group_delivery:<message_id>:<recipient>`：processing lease/done。

## 4. Redis 数据结构为什么这样选

- String：单值、状态、序号。
- Hash：一个用户下多个 message/会话字段映射。
- Set：无序且去重的成员集合。
- ZSet：需要按时间或 seq 排序的 pending、offline、timeline、最近会话。

## 5. 索引必须会说

### 消息幂等唯一索引

发送者 + client_msg_id 唯一，兜底防重复。

### 红包领取唯一索引

red_packet_id + user_id 唯一，防重复领取。

## 6. 事务什么时候用

需要多个数据库变化要么一起成功、要么一起失败时使用事务：

- 消息及会话元信息更新。
- 红包扣减和领取明细。

Redis 和 MySQL 不能被普通本地事务一起提交；当前 IM 主链路以 MySQL 历史作为最终事实，Redis 只承载可补偿的在线态和热点索引。

## 7. SQL 与 EXPLAIN 初级背诵

EXPLAIN 重点看：

- `type`：访问方式，避免大表 ALL 全表扫描。
- `key`：实际使用哪个索引。
- `rows`：估算扫描行数。
- `Extra`：是否 Using filesort、Using temporary、Using index。

联合索引遵循最左前缀。查询字段不在索引中时可能回表。索引不是越多越好：会占空间并增加写成本。

## 8. 缓存一致性怎么回答

> MySQL 是事实来源。普通热点缓存可以失效后回源；运营发布要求更强可追踪性，所以数据库事务写 Outbox，再更新 Redis。Redis 失败时 API 返回 202 pending，后台重放，不把已提交数据库状态误报为普通失败诱导重复发布。

## 9. 常见面试陷阱

### Redis 宕机，历史消息丢不丢？

MySQL 历史仍在，但在线路由、pending 和热索引会受影响，服务 readiness 应失败并停止接新流量；恢复后部分热数据可回源，过程状态是否完整取决于 Redis 持久化和备份配置。

### 为什么不用 Redis 保存全部聊天历史？

成本高、关系查询和长期可靠性不如 MySQL，且内存数据库不适合作为唯一事实来源。

### 为什么不全部查 MySQL？

在线路由、ACK、热点会话是高频低延迟操作，全部打 MySQL 会增加延迟和连接压力。

## 10. 闭卷题

1. MySQL 和 Redis 的职责边界？
2. messages 表为什么需要 seq 和 client_msg_id？
3. pending 为什么用 ZSet？
4. timeline 解决什么问题？
5. 四个重要唯一索引分别是什么？
6. 什么场景必须用事务？
7. 为什么需要 Outbox？
8. EXPLAIN 先看哪些字段？
9. 什么是联合索引最左前缀？
10. Redis 宕机后哪些数据仍然存在？
