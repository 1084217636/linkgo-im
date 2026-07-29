# 10 跨 Gateway 单聊：公司多服务器默认链路

## 本章前置

你已经理解 HTTP、gRPC、MySQL、JWT、WebSocket、本机连接表和 Redis 的在线路由与 Pub/Sub。

从本章开始，除非明确写“本地演示”，所有项目链路默认使用公司单集群、多服务器场景：A、B 可以连接不同 Gateway，Logic 运行多个实例，Redis/MySQL 是所有实例共享的外部服务。

## 本章目标

读完后，你应该能够：

1. 完整讲清 A 在 Gateway-1、B 在 Gateway-3 时的单聊路径。
2. 解释入口负载均衡、Etcd 服务发现和 Logic 客户端负载均衡。
3. 说清在线路由的写入、续期、比较删除和启动清理。
4. 区分 MySQL 持久化、Redis 在线通知和 WebSocket 最后一跳。
5. 面对 Gateway、Logic、Redis 故障追问时不夸大当前能力。

## 1. 先定义公司拓扑中的新词

### 实例和集群

同一份服务代码运行一次，得到一个服务实例。多个实例共同提供同一种服务，叫服务集群。

```text
Gateway-1、Gateway-2、Gateway-3 → Gateway 集群
Logic-1、Logic-2、Logic-3       → Logic 集群
```

实例拥有各自内存，因此每个 Gateway 只保存连接到自己的 WebSocket。

### 入口负载均衡器

负载均衡器（Load Balancer，LB）接收客户端的新建连请求，并把不同请求分给多个 Gateway。

```text
客户端 A → LB → Gateway-1
客户端 B → LB → Gateway-3
```

它主要决定新连接落到哪里。已经建立的 TCP/WebSocket 连接不会因为后来增加 Gateway-4 就自动搬过去。

### 服务注册与发现

Gateway 需要知道当前有哪些 Logic 实例可用：

- 注册：Logic 启动后把自己的可访问地址登记到服务注册中心。
- 发现：Gateway 获取并持续更新 Logic 地址列表。
- 租约：Logic 需要持续证明自己活着；宕机后租约到期，地址被移除。

LinkGo 使用 Etcd 保存 Logic 的注册信息，注册 Key 是：

```text
/services/logic
```

Etcd 在这里是服务通讯录，不保存好友关系或聊天正文。

### 客户端负载均衡

Gateway 获得多个 Logic 地址后，要为一次 gRPC 调用选择一个实例。项目配置 `p2c_ewma`：大意是比较少量候选实例，并结合近期延迟选择更合适者。

它不是一致性哈希。当前 `LogicRouterPool.GetClient` 的 key 参数也没有实现“同一用户永远固定某个 Logic”的业务路由。

## 2. Logic 实例怎样注册

Logic 配置包含：

```yaml
Etcd:
  Hosts:
    - etcd:2379
  Key: /services/logic
```

服务监听 `0.0.0.0:9001` 只表示接受本机所有网卡的连接，其他服务器不能把 `0.0.0.0` 当目标地址。

在容器环境中，部署清单把实例 IP 注入 `POD_IP`。go-zero 的 zRPC 服务启动时会用该环境变量推导对外注册地址，将 `POD_IP:9001` 注册到 Etcd。

如果 Logic 异常退出，Etcd 租约到期后，Gateway 的服务发现列表会移除它。正在进行中的 RPC 仍可能失败；服务发现不能让执行到一半的请求无损转移。

## 3. Gateway 什么时候走 Etcd

`NewLogicRouter` 有两种配置路径：

```text
LOGIC_ADDR 非空 → 直接连接配置的地址
LOGIC_ADDR 为空 → 从 Etcd /services/logic 发现实例
```

生产示例配置故意不设置 `LOGIC_ADDR`，从而使用 Etcd 和 `p2c_ewma`。

为什么不只连接一个普通 Service 地址？gRPC 常复用长 TCP 连接，四层 Service 可能只在建连时选一次后端，导致一条连接长期粘在某个 Logic。客户端服务发现让 Gateway 得到实际实例列表，gRPC balancer 才能在调用层选择后端。

这不代表 K8s Service 没有价值；它仍可给其他服务提供稳定入口。这里只解释项目对 Logic 调用采用的发现方式。

## 4. 用户上线时怎样登记位置

假设 A 的连接成功建立在 Gateway-1。Gateway 生成一次连接 ID，组合为：

```text
routeValue = gateway-1|connection-a7f...
```

`RefreshRoute` 写入：

```text
route:A = gateway-1|connection-a7f...       TTL 75 秒
gateway_users:gateway-1 包含 A              反向索引
gateway_conn:gateway-1:connection-a7f...=A  连接反查
gateway_live:gateway-1=当前时间              Gateway 活跃标记
```

B 在 Gateway-3 建连时写入同类记录：

```text
route:B = gateway-3|connection-b91...
```

所有 Gateway 和 Logic 连接同一逻辑 Redis，所以 Logic-2 能读到 Gateway-3 写的 B 路由。

## 5. 谁负责续期

需要区分两种心跳：

- 客户端心跳到达 Gateway 时，`RefreshRoute` 续期这个用户的 route、connection 和 Gateway 相关 Key。
- Gateway 后台心跳 `StartGatewayHeartbeat` 定期刷新 `gateway_live` 和 `gateway_users` TTL，但不会替每一个没有心跳的用户无条件续期 `route:<uid>`。

因此用户长期不发心跳时，个人 route 会过期；Gateway 进程活着不等于每条客户端连接都活着。

## 6. A 到 B 的完整单聊链路

现在假设：

```text
A → Gateway-1
B → Gateway-3
Logic 集群有 Logic-1/2/3
```

真实主链路：

```text
1. A 网页发送 Protobuf WireMessage(client_msg_id, to=B, body)
2. Gateway-1 的 StartClientLoop 解码
3. Gateway-1 把任务放入按 A 分片的有界队列
4. gRPC 客户端通过 Etcd 地址列表和 p2c_ewma 选择一个 Logic，例如 Logic-2
5. Logic-2 用已认证 UserId 校验发送者，并检查 A/B 好友关系
6. Logic-2 处理 client_msg_id、会话 ID、seq 和 message_id
7. Logic-2 将消息 INSERT 到共享 MySQL
8. RedisDelivery 先登记 B 的待确认状态
9. Logic-2 查询共享 Redis：route:B
10. 得到 gateway-3|connection-b91...
11. Logic-2 发布投递事件到 im_message_push:gateway-3
12. Gateway-3 的 Redis 订阅循环收到事件
13. Gateway-3 从本机 ClientManager 找 B
14. Gateway-3 向 B 的 WebSocket 写二进制消息
15. B 网页解码、展示，并发送确认帧
```

这里的好友关系就是 MySQL 中双方处于可单聊状态的业务授权，不是 Redis 在线状态；第 13 章再学习申请、接受和双向关系表。本章只需记住：JWT 证明“你是 A”，好友校验决定“A 能否给 B 发”。

第 15 步的确认、重试和重连补偿在下一章详细展开。本章先把跨服务器路由讲完整。

## 7. Redis 投递事件里有什么

`PushEnvelope` 包含：

```text
target_id
message_id
session_id
seq
trace_id
gateway_id
route_value
sent_at
payload_b64
```

Protobuf 二进制 payload 被 Base64 后放入 JSON envelope，再经 Redis Pub/Sub 传递。

这说明当前在线通知不是只发送一个 message_id 后让 Gateway 回查 MySQL，而是携带完整消息 payload。优点是目标 Gateway 可直接写出；代价是通知体更大，用户 `ack_idx` 也仍保存 payload 副本。

## 8. 为什么先登记 pending 再 Publish

`RedisDelivery.Deliver` 的顺序是：

```text
trackPendingAck
→ 查询 route
→ 尝试 PUBLISH
→ 找不到在线订阅者或发布失败则记 offline
```

如果先 Publish，再登记待确认状态，消息可能已经送出但系统还没有恢复依据。当前顺序先留下短期待确认数据，再做实时通知。

但它仍不是 MySQL 与 Redis 的分布式事务：MySQL INSERT 成功后，Redis 操作可能失败。当前代码依靠 client_msg_id 和历史数据降低影响，不能声称两种数据库原子提交。

## 9. Gateway-3 收到通知后做什么

每个 Gateway 启动一个 `SubscribeRedis` 循环，只订阅自己的 channel。

```text
解析 PushEnvelope
→ Manager.GetConn(target_id)
→ Base64 解码 payload
→ conn.WriteBinary
```

如果本机找不到 B：

1. 把 message_id 记入 B 的 offline ZSet。
2. 使用完整 routeValue 做比较删除。
3. 不会盲目删除 B 可能已经在别处建立的新 route。

如果写 WebSocket 失败，也会标记 offline、比较清理 route、关闭并移除本机连接。

如果写成功，只能证明操作系统接受了本次连接写入，不能证明 B 页面已解码和处理。最终还需要 B 的应用层确认。

## 10. `PUBLISH` 返回 1 代表什么

只代表当时有一个 Redis 订阅客户端订阅该 channel。

下面三个结论都不能由它推出：

```text
Gateway-3 成功解析事件
Gateway-3 成功写入 B 的 WebSocket
B 页面成功展示消息
```

所以 Redis Pub/Sub 是实时唤醒通道，消息可靠性必须结合 MySQL、pending、确认和历史查询。

## 11. B 离线时发生什么

如果 `route:B` 不存在，或在线路由对应 channel 没有订阅者，`RedisDelivery` 把 message_id 记入 `offline_msg:B`，并返回投递步骤完成。消息正文此前已经写入 MySQL。

准确边界：

- 短期重连可使用 Redis pending/timeline 数据。
- 网页选择会话时可调用 MySQL 历史 HTTP 接口，当前一次返回最新 50 条。
- 当前 WebSocket 重连函数本身不会在 Redis 数据缺失时自动分页扫描 MySQL 所有会话缺口。

因此不能再使用“重连一定自动从 MySQL 按所有会话 last_seq 完整补齐”的口径。下一章会把当前已实现路径与建议演进分开。

## 12. 同一 Gateway 是否走另一条路

不会。

即使 A、B 都在 Gateway-1，当前 Logic 仍查询 `route:B=gateway-1|...`，发布到 `im_message_push:gateway-1`，再由 Gateway-1 的订阅循环找本机 B。

统一路径让逻辑简单，但增加一次 Redis 往返。未来可以评估安全的本地快路径，但必须保留 pending、路由 owner 校验和失败回退；当前项目没有实现。

## 13. 十台 Gateway 满了怎样扩容

WebSocket 是已建立的长连接，不能把一条连接无损地从 Gateway-1 的内存搬到新 Gateway-11。

正确过程：

1. 启动新的 Gateway 实例。
2. 新实例通过健康检查后，入口 LB 把新的握手请求分给它。
3. 旧连接继续留在原 Gateway。
4. 若旧节点需要下线，先停止给它分配新连接。
5. 等连接自然结束，或通知客户端重新连接。
6. 新连接写入新的 Redis routeValue。
7. 客户端使用确认进度和历史接口恢复可能缺失消息。

Kubernetes 怎样创建实例、停止导流和滚动更新在第 18 章解释。本章只回答网络事实：扩容首先影响新连接，不会自动迁移旧连接。

## 14. 多服务器中间件到底放在哪里

公司场景不是每台应用服务器各带一套互不相通的数据：

```text
Gateway/Logic 多实例
→ 共享 Redis 稳定入口
→ 共享 MySQL 主库/代理入口
→ 共享 Etcd 集群
```

Redis 地址和 MySQL DSN 都是逻辑稳定入口。后台高可用不等于应用已经实现 Redis Cluster 分片或 MySQL 读写分离。

Kafka 会在群聊章节加入这张图；现在单聊链路不需要为了“组件更多”强行经过 Kafka。

## 15. 当前多服务器能力边界

已能从代码和配置证明：

- 多 Gateway 各自维护本机连接。
- Redis 共享在线路由和 Gateway 定向 Pub/Sub。
- Logic 多实例注册 Etcd。
- Gateway 通过 Etcd resolver 和 `p2c_ewma` 调用 Logic。
- 应用可以连接外部 Redis/MySQL 稳定入口。

不能声称：

- Pub/Sub 自带可靠重放。
- 已实现 Redis Cluster 原生分片。
- 已实现 MySQL 应用层读写分离。
- 已实现同账号多设备路由。
- 已实现跨机房一致性和自动容灾演练。
- 已把现有 WebSocket 自动迁移到新 Gateway。

## 代码锚点

按链路顺序阅读：

1. `cmd/gateway/internal/svc/logicrouter.go`：直连与 Etcd 两种模式、`p2c_ewma`。
2. `cmd/logic/etc/logic.yaml`：Etcd Key。
3. `deploy/k8s/logic.yaml`：向实例注入 `POD_IP`。
4. `cmd/gateway/internal/handler/websockethandler.go`：建立 routeValue、登记连接。
5. `internal/server/route.go`：路由写入、心跳、比较删除、启动清理。
6. `internal/logic/handler.go`：消息保存后进入投递。
7. `internal/delivery/redis.go`：pending、route 查询、PUBLISH、offline。
8. `internal/server/manager.go`：订阅目标 Gateway channel 并写本机连接。
9. `deploy/k8s/production/configmap.yaml`：外部共享地址示例。

## 动手练习

### 练习一：手画完整链路

必须画出七个框：

```text
A、Gateway-1、Logic-2、MySQL、Redis、Gateway-3、B
```

在每条箭头上写协议或动作：WebSocket、gRPC、SQL、GET route、PUBLISH、WebSocket。

### 练习二：读配置判断模式

回答：

```text
LOGIC_ADDR=logic:9001 时走哪种模式？
LOGIC_ADDR 为空且 ETCD_ENDPOINTS 非空时走哪种模式？
```

再在 `logicrouter.go` 中找到对应分支。

### 练习三：故障推演

分别回答：

1. MySQL INSERT 前 Logic 崩溃。
2. MySQL INSERT 成功后 Redis 不可用。
3. PUBLISH 时 Gateway-3 没有订阅者。
4. Gateway-3 收到通知但本机已没有 B。
5. B 快速从 Gateway-3 重连到 Gateway-4，旧连接随后关闭。

回答格式固定为：影响什么、现有代码做什么、仍有什么边界。

### 练习四：验证 Logic readiness 辅助函数

```bash
go test ./cmd/gateway/internal/svc -run TestWaitForLogicReady -v
```

说明“连接 ready”只表示 gRPC 连接状态，不表示一条业务消息已经落库。

## 闭卷检查

1. 实例、集群和入口负载均衡器分别是什么？
2. Etcd 在项目里保存什么，不保存什么？
3. Logic 怎样登记可访问地址？
4. `LOGIC_ADDR` 非空和为空有什么区别？
5. `p2c_ewma` 是一致性哈希吗？
6. A 在 Gateway-1、B 在 Gateway-3 时，完整单聊链路是什么？
7. `route:B` 为什么同时包含 gatewayID 和 connectionID？
8. 客户端心跳和 Gateway 后台心跳分别续期什么？
9. `PUBLISH` 返回订阅者数量为什么不等于 B 收到？
10. 同一 Gateway 的消息当前是否有本地快路径？
11. 新增 Gateway 为什么不能自动搬迁旧 WebSocket？
12. 当前 Redis/MySQL 多服务器能力有哪些明确边界？

下一步：[11 ACK、重试与离线恢复](11_RELIABILITY_AND_OFFLINE.md)
