# 17. 多服务器部署与跨 Gateway 消息链路

## 一、先背最终结论

LinkGo 的面试拓扑不是“一台机器上跑几个进程”，而是入口负载均衡、Gateway 集群、Logic/Transfer 集群、共享中间件集群。客户端 A 和 B 可以连接不同 Gateway；Gateway 只保存本机 WebSocket，在线路由写入共享 Redis；业务校验和持久化由 Logic 集群完成；群聊异步扇出由 Kafka 和 Transfer 完成。

```text
客户端 A -> LB/Ingress -> Gateway-1 --gRPC--> Logic-2
                                |                |--> MySQL 主库稳定入口
                                |                |--> Redis HA 稳定入口
                                |                `--> Kafka Broker 集群
客户端 B <- LB/Ingress <- Gateway-3 <- Redis Pub/Sub
                                ^
                                `-- user_route:B = Gateway-3

Gateway-* --Etcd /services/logic--> Logic-1、Logic-2、Logic-3
```

一句话：**连接属于某个 Gateway，路由属于共享 Redis，业务属于 Logic 集群，持久化属于 MySQL，异步群扇出属于 Kafka/Transfer。**

## 二、A 与 B 在不同服务器时，单聊怎样传递

假设 A 的 WebSocket 在 Gateway-1，B 的 WebSocket 在 Gateway-3。

1. A 通过 HTTPS 登录，服务校验密码后签发 JWT。
2. WebSocket Upgrade 时 Gateway-1 校验 Origin、JWT 和用户身份，再保存本机 `uid -> connection`。
3. Gateway-1 把 `user_route:A = gateway-1` 写入共享 Redis，并用心跳续期 TTL。B 在 Gateway-3 上线时同理写入 `user_route:B = gateway-3`。
4. A 携带 `client_msg_id`、接收者和内容发消息。Gateway-1 不直连 Gateway-3，而是通过 gRPC 调 Logic。
5. Gateway 从 Etcd 的 `/services/logic` 获得存活实例，通过 `p2c_ewma` 选择 Logic-2，而非永远固定一台。
6. Logic-2 校验好友/会话权限，用 Lua 原子生成会话序号，按 `client_msg_id` 幂等写入 MySQL。MySQL 是最终事实源。
7. Logic 查询 Redis 中 B 的路由，得到 `gateway-3`，把投递事件发布到 Gateway-3 对应的 Redis channel。
8. 每个 Gateway 只消费自己的 channel；Gateway-3 收到事件，从本机连接表找到 B 并写 WebSocket。
9. B 返回 ACK，系统清理相应待确认引用；若超时则按策略重试。客户端依据消息 ID/会话序号去重。
10. 若 B 不在线或 Gateway-3 失效，消息仍在 MySQL。B 重连后提交各会话最后收到的序号，服务从 MySQL 查询缺口并分页补齐；Redis 只做在线加速和短期待确认，不永久保存离线消息全集。

Redis Pub/Sub 只负责通知目标 Gateway，不是消息事实源，因为订阅者离线时通知不会保留。MySQL 中已提交的消息才保证重连恢复。

## 三、Logic 为什么是真正集群

每个 Logic Pod 监听 `0.0.0.0:9001`。K8s 通过 Downward API 注入 `POD_IP`；go-zero 注册服务时把 `POD_IP:9001` 写到共享 Etcd 的 `/services/logic`。Logic 宕机后租约到期，地址从服务发现中消失。

生产配置必须满足：

```text
ETCD_ENDPOINTS=etcd-1:2379,etcd-2:2379,etcd-3:2379
LOGIC_ADDR 不设置
```

若配置 `LOGIC_ADDR=logic-service:9001`，Gateway 会走直接连接。K8s Service 通常只在建立 TCP/gRPC 连接时选择后端，长连接可能长期粘在某个 Logic Pod，因此本项目生产模板改用 Etcd 实例发现和客户端负载均衡。

## 四、Redis 多服务器怎样部署

当前项目的严谨答案是“一个稳定 HA 接入地址，后面是多节点 Redis”，可使用托管 Redis、Sentinel 加代理/VIP，或平台提供的 Redis HA Service：

```text
Gateway/Logic/Transfer -> redis-ha.company.internal:6379
                              `-> primary + replicas + failover
```

所有应用服务器必须连接同一套逻辑 Redis，才能共享在线路由、短期待确认、幂等键和 Pub/Sub channel，不能每台 Gateway 使用互不相通的本地 Redis。故障时由托管服务或 HA 代理把稳定域名切到新主节点，客户端重连。

面试中不能说当前代码已原生支持 Redis Cluster 分片：跨槽 Lua、多 key 原子操作和 Pub/Sub 语义都需要专门设计，本版本没有虚构这项能力。高可用和水平分片是两个问题。

## 五、MySQL 多服务器怎样部署

应用连接稳定的主库/数据库代理地址，而不是随机轮询多个数据库节点：

```text
Logic -> mysql-primary.company.internal:3306 -> primary -> replicas
```

消息写入、会话序号、红包扣减和幂等事务都必须走主库。复制提供备份、容灾和可能的只读扩展；故障转移由托管数据库或 ProxySQL/平台控制面完成，稳定入口保持不变。

当前代码没有应用层读写分离，所以不能声称“历史消息已经从从库读取”。以后增加只读 DSN 时也要处理复制延迟：刚写后的查询、未读游标和余额等强一致读取仍走主库，允许短暂旧数据的统计才走从库。

每个 Logic 实例有独立连接池，因此 `实例数 × MaxOpenConns` 不能超过数据库连接上限。例如 10 个 Logic、每实例 80 个最大连接，理论上最多 800 条，需要按数据库容量下调或增加代理池化。

## 六、Kafka 与群聊的多机链路

群消息先由 Logic 鉴权并持久化，再写 Kafka 集群。Transfer 多实例使用相同 consumer group，一个分区同一时刻只由一个实例消费。分区键选 `conversation_id`，同一群进入同一分区保持顺序，不同群并行。

Transfer 查询群成员后解析在线路由，按目标 Gateway 聚合投递；失败成员进入 retry topic，超过次数进入 DLQ，成功写 retry/DLQ 后才提交原位点。重复消费依靠消息 ID 和成员投递状态幂等。

## 七、十台 Gateway 满了怎么扩容

WebSocket 是长连接。新增 Gateway-11 后，负载均衡只会把新连接分给它，不会自动搬迁已有连接。

1. K8s HPA 或人工新增 Pod；readiness 成功后接收新连接。
2. 若旧节点只是负载高，保留已有连接，新连接逐步流向新节点。
3. 若必须迁移，先让旧 Gateway readiness 失败，停止接收新连接并 draining。
4. 通知客户端重连或等待连接自然结束；客户端携带最后 ACK 游标连接新 Gateway。
5. 新 Gateway 覆盖 Redis 路由；旧路由靠 owner 校验和 TTL 清除，旧节点不能误删新路由。
6. 重连后从 MySQL 按会话游标补偿窗口内消息。

跨物理服务器时，应用代码不依赖本机 IP。K8s CNI/云网络保证网络可达，DNS/服务发现提供稳定名字，TLS、ACL 和 Secret 管理凭据。跨机房还涉及网络分区和数据复制，不是当前单集群版本已经完成的能力。

## 八、代码和配置证据

- `cmd/gateway/internal/svc/logicrouter.go`：未设置 `LOGIC_ADDR` 时使用 Etcd 和 `p2c_ewma`。
- `deploy/k8s/logic.yaml`：向 Logic 注入 `POD_IP`，注册可路由地址。
- `deploy/k8s/production/configmap.yaml`：Etcd/Kafka 多地址与 Redis HA 入口示例。
- `deploy/k8s/production/secret.example.yaml`：MySQL primary/proxy DSN 示例；真实密码不进 Git。
- `deploy/k8s/production/kustomization.yaml`：只部署应用，不部署单节点演示中间件。

## 九、面试官连续追问

**A 和 B 不在一台 Gateway，Gateway-1 怎么找到 Gateway-3？** B 上线时把带 TTL 的 `user_route:B=gateway-3` 写到共享 Redis；Logic 查路由后发布到 Gateway-3 的 channel。

**Redis Pub/Sub 丢了怎么办？** Pub/Sub 不是可靠存储。消息先落 MySQL；通知失败后客户端重连，按会话游标从 MySQL 补偿。

**为什么不在 Redis 保存所有离线消息？** 用户量和历史量使内存成本不可控。Redis 保存热点和短期状态，MySQL 保存可恢复历史。

**Logic-2 调用中宕机怎么办？** RPC 失败或超时，Gateway 用相同 `client_msg_id` 重试；Etcd 移除故障实例，后续选其他 Logic；数据库唯一键避免重复落库。

**三台 MySQL 为什么只配一个地址？** 那是稳定 primary/proxy endpoint，多节点复制和故障转移在入口后完成。随机轮询会把写请求送到只读从库。

**Redis Cluster 为什么不能直接加三个地址？** Cluster 需要支持槽位重定向的客户端，并处理多 key/Lua 同槽和 Pub/Sub；当前单入口客户端不等于 Cluster 客户端。

**K8s Service 能负载均衡，为何还用 Etcd？** gRPC 是长连接，Service 通常在建连时选一次 Pod。Etcd 让客户端获得实例列表，`p2c_ewma` 能按 RPC 选择实例。

## 十、90 秒背诵版

我的部署按多服务器设计。A、B 可以分别连接 Gateway-1 和 Gateway-3，连接表只在各自 Gateway 内存中，用户到 Gateway 的在线路由写在共享 Redis 并带 TTL。A 发单聊时 Gateway 通过 Etcd 发现全部 Logic 实例，使用 p2c_ewma 选择一个 Logic。Logic 做权限、幂等和会话序号处理，先把消息写入 MySQL，再查 B 的 Redis 路由并发布到 Gateway-3 的 channel，由 Gateway-3 写入 B 的 WebSocket。Redis Pub/Sub 只是在线通知，不承担永久可靠性；B 离线或通知丢失后，重连会根据各会话最后序号从 MySQL 分页补偿。Redis 和 MySQL 都不部署在某台应用服务器里，应用连接它们的高可用稳定入口；Kafka 和 Etcd 配置多个节点。当前版本严谨支持的是单集群、多 Gateway、多 Logic 和外部 HA 中间件入口，我不会夸大成已实现 Redis Cluster 分片、MySQL 读写分离或跨机房一致性。
