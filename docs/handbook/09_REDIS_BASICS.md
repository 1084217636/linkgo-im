# 09 Redis 基础与在线状态

## 本章前置

你已经知道进程内存不能跨 Gateway 共享，也知道一条普通消息会先进入 Logic 并写入 MySQL。

现在解决两个问题：Logic 怎样知道 B 连接在哪台 Gateway？不同 Gateway 怎样低延迟通知彼此？

## 本章目标

读完后，你应该能够：

1. 区分 Redis 与本机 Go map、MySQL。
2. 看懂 String、Hash、Set、ZSet、TTL、Lua 和 Pub/Sub。
3. 说出 LinkGo 主要 Redis Key 的真实内容。
4. 解释 Pub/Sub 为什么只能做在线通知。
5. 说明当前代码支持怎样的 Redis 部署，不支持怎样的 Redis Cluster。

## 1. Redis 是什么

Redis 是一个独立运行、通过网络访问的数据服务。多台 Gateway 和 Logic 只要连接同一套 Redis，就能读写同一份在线状态。

```text
Gateway-1 ─┐
Gateway-3 ─┼→ 同一个 Redis 逻辑服务
Logic-2   ─┘
```

这和 Go map 有根本区别：

- Go map 在一个进程内，进程退出后内容消失，其他进程不能直接访问。
- Redis 在独立进程中，通过网络协议共享给多个应用实例。

Redis 主要在内存中工作，因此适合高频、低延迟的在线状态和短期数据。但“快”不等于“应保存所有数据”。LinkGo 的长期消息历史仍以 MySQL 为准。

## 2. Key、Value 和 TTL

Redis 用 Key 找 Value。例如：

```text
route:1002 = gateway-3|conn-xyz
```

TTL 是 Time To Live，即这个 Key 还能存活多久。TTL 到期后 Key 自动失效。

在线路由需要 TTL，因为 Gateway 可能突然宕机，来不及执行清理。没有 TTL，Redis 可能永远认为 B 仍在一台已经不存在的 Gateway 上。

TTL 只能降低残留时间，不能证明路由此刻百分之百正确。网络中断、延迟和快速重连仍会产生短暂旧状态。

## 3. 五种本项目会用到的数据形式

### String：一个 Key 对应一个值

用途包括：

```text
route:<uid>                         用户当前连接位置
gateway_conn:<gateway>:<conn>      连接反查用户
gateway_live:<gateway>             Gateway 最近心跳
seq:<session>                      会话当前序号
message_payload:<message>          近期共享消息正文
```

Redis String 的值既可以是普通文本，也可以是编码后的字节文本。

### Hash：一个 Key 下有多个字段

像 Go 的 `map[field]value`：

```text
ack_idx:<uid>    message_id → Base64 编码后的完整消息 payload
ack_retry:<uid>  message_id → 已重试次数
```

必须注意源码事实：当前 `ack_idx:<uid>` 不是纯 message_id 指针，它给每个收件人保存一份完整 payload。项目另有共享 `message_payload:<message_id>`，但 ACK 重推仍优先读取用户自己的 `ack_idx`。把 `ack_idx` 进一步改成纯引用是演进项，不是当前完成状态。

### Set：无序且成员不重复

```text
gateway_users:<gateway>        该 Gateway 最近登记过的用户
conversation:members:<session> 会话成员热数据
```

它适合判断成员是否存在，不提供按时间排序。

### ZSet：带分数的有序集合

每个成员除自身值外还有一个 score，Redis 可以按 score 范围查询：

```text
pending_ack:<uid>          score=投递时间，member=message_id
offline_msg:<uid>          score=记录时间，member=message_id
session_timeline:<session> score=seq，member=message_id
user:conversations:<uid>   score=最近时间，member=conversation_id
```

`pending_ack` 和 `offline_msg` 的 ZSet 成员只是 message_id；完整 payload 当前在 `ack_idx` 和近期 `message_payload` 中。

### Pub/Sub：发布和订阅

发布者向一个 channel 发消息，正在订阅该 channel 的客户端立即收到：

```text
PUBLISH im_message_push:gateway-3 <投递事件>
```

每台 Gateway 订阅自己的 channel。它不是上面四种持久 Key 类型，而是一种实时通信机制。

为什么不是“所有 Gateway 共用一个广播频道”？广播会让每台 Gateway 都接收并解析与自己无关的消息，节点越多越浪费网络和 CPU。为什么也不是“每个用户一个频道”？在线用户很多时，订阅数量和连接管理会迅速膨胀。当前折中方案是每个 Gateway 一个频道：先用 `route:<uid>` 找到 Gateway，再由该 Gateway 的本机 map 找用户连接。代价是频道有订阅者只能证明 Gateway 的 Redis 订阅还在，不能证明目标用户连接仍在。

## 4. Pub/Sub 为什么适合在线通知

假设 B 在 Gateway-3：

```text
Logic 发布到 im_message_push:gateway-3
→ Gateway-3 的订阅连接收到事件
→ Gateway-3 查本机 ClientManager
→ 写 B 的 WebSocket
```

优点：路径短、延迟低，Gateway 之间不需要两两建立 RPC 连接。

但 Redis Pub/Sub 的语义接近“此刻喊一声”：订阅者断线期间，通知不会作为可重放日志长期保存。`PUBLISH` 返回订阅者数量，也只表示当时有多少 Redis 客户端订阅该 channel，不表示 Gateway 已经处理，更不表示 B 已经收到。

因此项目把 Pub/Sub 限定为在线通知，不能把它说成消息最终存储或可靠队列。

## 5. Redis 原子操作是什么

原子操作表示其他请求不会看见这个操作只完成一半的中间状态。

Redis 的单条命令通常是原子执行的。例如 `INCR` 可以安全地把一个数字加一。多个步骤需要作为一个整体判断和修改时，项目使用 Lua 脚本。

### 为什么清理路由要用 Lua

用户快速重连：

```text
旧连接：gateway-1|old
新连接：gateway-3|new
```

如果旧连接关闭时直接 `DEL route:uid`，会把新连接刚写入的位置删掉。

正确动作是一个不可分割的“比较后删除”：

```text
如果 route:uid 仍等于 gateway-1|old
才删除
否则不动
```

`internal/server/route.go` 的 `clearRouteScript` 在 Redis 内完成这件事。

### 会话序号为什么用 Lua

多个 Logic 实例同时处理消息时，都访问同一个 Redis Key，避免每台 Logic 用自己的内存计数器或 `GET + SET` 产生重复、覆盖。序号原子递增本身来自 Redis 的单条 `INCR`；`sessionSeqScript` 使用 Lua，是为了把 `INCR` 和刷新过期时间 `PEXPIRE` 合成一个不可分割步骤，避免只递增成功却没有按预期续期。

代价是 Redis 变成发送强依赖；Key 过期后还要用 MySQL `MAX(seq)` 兜底初始化。会话序号的完整顺序边界在第 11 章继续解释。

## 6. Pipeline 与事务不要混淆

Pipeline 把多条命令批量发送，主要减少网络往返。

go-redis 的 `TxPipeline` 会用 Redis `MULTI/EXEC` 包住命令，使这一批命令在 Redis 中连续执行，不被其他命令插入。但它仍不等于 MySQL 事务：

- 没有关系型数据库那样的行锁和隔离级别。
- 某条命令自身出错时，不提供通用业务回滚。
- Redis 与 MySQL 也不能被一个普通本地事务一起提交。

因此不要看到名字带 `Tx` 就说“Redis 和 MySQL 已经强一致事务”。

## 7. LinkGo 的 Redis Key 分成哪几类

### 在线位置

```text
route:<uid>
gateway_users:<gatewayId>
gateway_conn:<gatewayId>:<connectionId>
gateway_live:<gatewayId>
```

解决“用户在哪台 Gateway”和旧路由清理。

### 发送幂等和会话序号

```text
client_msg:<uid>:<client_msg_id>
seq:<session_id>
```

解决重复发送快速识别和共享序号。

### 待确认与短期补偿

```text
pending_ack:<uid>
ack_idx:<uid>
ack_retry:<uid>
offline_msg:<uid>
message_payload:<message_id>
session_timeline:<session_id>
```

第 11 章会沿 ACK 和重连逐项使用。本章先能识别类型和内容。

### 最近会话热数据

```text
user:conversations:<uid>
conversation:last:<conversation_id>
conversation:members:<conversation_id>
user:conversation:read:<uid>
```

登录优先读这些热数据，未命中再从 MySQL 查询会话表。

## 8. Redis 和 MySQL 的职责边界

| 问题 | 当前主要来源 |
| --- | --- |
| 用户此刻在哪台 Gateway | Redis route |
| 跨 Gateway 在线通知 | Redis Pub/Sub |
| 未确认消息的短期重推状态 | Redis pending/ack_idx |
| 最近会话热数据 | Redis，可从 MySQL 回源 |
| 完整聊天历史 | MySQL messages |
| 好友与群成员最终关系 | MySQL |
| 红包事务结果 | MySQL |

一句话：MySQL 保存长期业务事实，Redis 保存共享在线状态、短期投递状态和热点数据。

但不要过度简化成“Redis 随便丢都没影响”。若 Redis 丢失尚未确认状态，虽然 MySQL 历史还在，自动重推所需的过程信息仍可能受损。

## 9. 公司多服务器怎样连接 Redis

所有 Gateway、Logic 和 Transfer 实例必须连接同一套逻辑 Redis。不能每台 Gateway 各安装一个互不相通的 Redis，否则 Gateway-1 写的 B 路由，Logic-2 和 Gateway-3 都看不到。

当前应用代码统一使用：

```go
redis.NewClient(&redis.Options{Addr: oneAddress})
```

也就是配置一个 Redis 协议入口。公司环境可以让这个地址指向托管 Redis 的稳定端点，或者能处理主从切换的代理/VIP。后台可由主节点、复制节点和故障切换组成，但应用看到的是同一个逻辑地址。

### 当前不具备的能力

- 没有使用 `NewFailoverClient` 直接发现 Sentinel。
- 没有使用 `ClusterClient` 原生访问 Redis Cluster。
- 没有为多 Key Lua 设计 Cluster hash tag 和同槽约束。

所以准确说法是“支持连接一个外部高可用稳定入口”，不是“代码已经原生支持 Redis Cluster 分片”。高可用故障切换和水平分片是两个不同问题。

## 10. Redis 不可用会怎样

影响不只是缓存变慢：

- 新连接的共享在线位置无法可靠登记。
- Logic 无法完成发送幂等预占和会话 seq 分配，普通消息处理会失败。
- 在线 Pub/Sub 通知无法完成。
- pending、重推和短期补偿受影响。
- MySQL 中已经提交的历史消息仍存在。

这说明 Redis 在当前实时链路中是强依赖，不是一个“挂了就全部直接查 MySQL”的普通页面缓存。

工程化章节会解释健康检查怎样暂时停止向依赖异常的实例导流。这里先记影响面。

## 代码锚点

1. `cmd/gateway/internal/svc/servicecontext.go`：Gateway 创建 Redis Client。
2. `cmd/logic/internal/svc/servicecontext.go`：Logic 连接同一逻辑 Redis。
3. `internal/server/route.go`：Key 命名、TTL、比较删除 Lua。
4. `internal/delivery/redis.go`：pending、路由查询和 Pub/Sub。
5. `internal/server/manager.go`：Gateway 订阅自己的 channel。
6. `internal/server/sync.go`：timeline 和共享 payload。
7. `internal/logic/handler.go`：`sessionSeqScript`、`client_msg`。

## 动手练习

### 练习一：给 Key 分类

不看上文，把下面 Key 写成 String、Hash、Set 或 ZSet：

```text
route:1002
gateway_users:gateway-3
ack_idx:1002
pending_ack:1002
session_timeline:c2c:1001:1002
```

### 练习二：解释 Pub/Sub 返回值

假设 `PUBLISH` 返回 1。列出它不能证明的两件事。标准答案至少包括“Gateway 已经写 WebSocket”和“B 的客户端已经处理”。

### 练习三：检查路由删除

```bash
go test ./internal/server -run 'TestClientManagerRemoveOnlyMatchingSession|TestParseRoute' -v
```

再阅读 `clearRouteScript`，用自己的话说出 ARGV 中旧 routeValue 的作用。

## 闭卷检查

1. Redis 与本机 Go map 有什么区别？
2. TTL 为什么适合在线路由？
3. Set 和 ZSet 的差异是什么？
4. `pending_ack` 与 `ack_idx` 分别保存什么？
5. 当前 `ack_idx` 是否只是 message_id 指针？
6. Pub/Sub 为什么不能做最终消息存储？
7. `PUBLISH` 返回订阅者数量能证明用户收到吗？
8. 为什么清理路由要比较 connection identity？
9. Pipeline、Redis TxPipeline、MySQL 事务有什么区别？
10. 当前代码是否原生支持 Sentinel 或 Redis Cluster？

下一步：[10 跨 Gateway 单聊](10_MULTI_GATEWAY_CHAT.md)
