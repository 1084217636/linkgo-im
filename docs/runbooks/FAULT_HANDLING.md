# LinkGo 故障先行演练与恢复手册

这份手册回答一个完整 IM 系统的第一问题：某个组件坏了以后，系统怎样发现、怎样停止继续放大故障、怎样替换实例、怎样验证恢复。

它不是“把容器重启一下”的命令清单。每个场景都要按下面顺序处理：

```text
发现故障
→ readiness 摘掉不能接收新流量的实例
→ 保留 Redis/MySQL/Kafka 中的持久或可重放数据
→ 重启或扩容替换实例
→ 先验证依赖健康，再验证登录、单聊、离线回放、群聊
→ 观察指标和日志，必要时回滚镜像
```

## 1. 先分清 healthz 和 readyz

`/healthz` 只说明进程还活着，不能证明它可以处理业务。`/readyz` 才是是否接收新流量的判断。

| 组件 | healthz | readyz | readyz 检查的依赖 |
| --- | --- | --- | --- |
| Gateway | HTTP 进程存活 | 可以接新请求和 WebSocket | Logic gRPC、Redis、MySQL |
| Logic | HTTP 健康端口存活 | 可以处理消息 | Redis、MySQL、Kafka |
| Transfer | HTTP 进程存活 | 可以消费和投递群消息 | Redis、Kafka |

故障时不能把 liveness 也绑定到每一个短暂依赖错误，否则 Redis 抖动会导致所有 Pod 重启。当前依赖错误优先进入 readiness，Kubernetes 会停止给该 Pod 发送新流量；旧 WebSocket 不会被迁移，客户端必须通过稳定入口自动重连。

## 2. 故障矩阵和当前处理口径

### Gateway 实例挂掉

Kubernetes 的 readiness 失败或进程退出后，Service 不再把新连接发给这个 Pod。现有 WebSocket 连接会断开，浏览器客户端使用指数退避重连到稳定的负载均衡入口。Gateway 退出时清理自己持有的 Redis 路由；即使来不及清理，路由 TTL 也会过期。

消息不会把 WebSocket 连接“搬到”新 Pod。发送方使用稳定入口重新建立连接，Logic 依据 Redis 中的在线路由选择新 Gateway；未 ACK 的消息保留在 pending 索引中，重连后按游标回放。

### Logic 实例挂掉

正在处理的 RPC 会失败，Gateway 返回可重试的拒绝结果，客户端保持同一个 `client_msg_id` 重试，避免生成两条逻辑消息。已经写入 MySQL 的历史消息由数据库保留；Redis 投递前的 pending 和会话 outbox 负责后续补偿。

Kubernetes 通过 Logic 的 `/readyz` 防止依赖不全的新 Pod 接流量；Gateway 在自己的 `/readyz` 中还会通过同一条 gRPC 连接调用标准 Health Check，因此 Etcd 发现到依赖不全的 Logic 时也会被 Gateway 摘掉。Deployment 按滚动策略补回副本。这个健康检查只负责流量决策，不能把它说成跨服务事务。

### Redis 挂掉

Redis 同时承载在线路由、Pub/Sub、pending/ACK、短期回放和限流状态。Gateway、Logic、Transfer 的 readiness 会失败，系统应拒绝可能造成假成功的消息写入，而不是只记录日志后返回成功。MySQL 中的历史消息仍然存在，但没有 Redis 就不能保证实时投递和 ACK 清理。

生产环境需要 Redis Sentinel、Cluster 或托管 HA，并让应用连接一个稳定入口。当前仓库验证的是单 Redis 容器故障和恢复，不是 Redis 集群自动故障转移。

### MySQL 挂掉

Gateway 和 Logic readiness 失败，新的登录、消息持久化和历史查询不能返回假成功。已经提交的消息仍在数据库中；没有完成数据库提交的发送请求由客户端使用原 `client_msg_id` 重试。恢复后先确认连接池能 Ping，再运行单聊和历史查询验收。

生产环境需要主从、代理或托管数据库的故障转移，并通过一个稳定 DSN 暴露给应用。当前代码没有在应用内部实现读写分离或主库选举。

### Kafka 挂掉

Kafka 是群聊异步扩散的缓冲和重放边界，不是单聊实时转发的必需组件。Logic 的群消息发布失败时不能返回群消息已完成；Transfer readiness 失败，Logic readiness 也会因为 Kafka 检查失败而失败。Kafka 恢复后，未提交 offset 的消息继续消费，处理失败的成员进入 retry/DLQ，只有处理或转移成功后才提交 offset。

这就是“用 Kafka 防止群聊堵死”的完整含义：Gateway 不同步轮询所有群成员，生产者把工作交给有界的 broker 队列，Transfer 消费者独立扩容。它不能保证 Kafka 永不失败，只能把失败变成可观察、可重试的状态。

### Transfer 实例挂掉

Kafka 保留未提交记录，其他 Transfer 副本继续消费；如果所有副本都挂掉，消息不会因为进程内存消失，但会受到 Kafka retention 限制。恢复后使用消费组继续处理，成员投递状态通过 lease/idempotency 防止重复发放。

### Etcd 或服务发现异常

新 Logic 地址可能无法被 Gateway 发现，已有 gRPC 连接也可能继续存活。此时先恢复 Etcd quorum 或切换到稳定的服务发现入口，再观察 Gateway 的 Logic ready 和 RPC 错误率。当前项目不把 Etcd 故障伪装成业务数据已经丢失；生产需要独立的 Etcd 集群或平台服务发现。

## 3. 扩容、替换和回滚

Gateway 扩容只接收新连接，不能迁移旧 WebSocket。Kubernetes HPA 增加 Pod 后，Service 把后续连接分给新 Pod；旧 Pod 因节点故障退出时，客户端重连完成连接恢复。Logic 扩容依赖 Etcd 发现和客户端负载均衡；Transfer 扩容依赖 Kafka consumer group 分配分区，不能只复制进程而不增加分区容量。

替换遵循以下顺序：

1. 发布不可变镜像标签，不使用 `latest`。
2. `kubectl apply` 后等待新 Pod readiness。
3. 旧 Pod 先从 Service 端点移除，再执行 5 秒 `preStop` 排空窗口。
4. 执行健康检查和真实业务 smoke。
5. 错误率、队列拒绝、Kafka 操作失败持续异常时执行 `kubectl rollout undo`。

仓库中的 Gateway、Logic、Transfer Deployment 都配置了 `maxUnavailable: 0`、PDB 和 `preStop`；这证明的是发布策略和演练配置，不等于已经在公司的多节点集群上完成演练。

## 4. 本地故障演练

先启动完整 Compose。轻量 Compose 没有 Kafka/Transfer，不能执行完整故障矩阵。

```bash
docker compose --env-file .env.docker-cn up -d --build
make ops-smoke
```

静态契约检查：

```bash
make fault-check
```

真实破坏性演练会停止并重启 Redis、MySQL、Logic、Kafka、Transfer 和 Gateway。必须明确确认：

```bash
FAULT_INJECTION_CONFIRM=1 \
COMPOSE_FILE_PATH=docker-compose.yml \
COMPOSE_ENV_FILE=.env.docker-cn \
WAIT_SECONDS=45 \
bash scripts/fault_injection.sh
```

脚本会逐场景记录：

```text
依赖停止后 readyz 是否失败
服务重新启动后 readyz 是否恢复
恢复后登录、在线单聊、离线回放、ACK、AI 和群聊是否完成
Gateway-a 停止时 Gateway-b 是否可替换接流量
```

结果文件在 `artifacts/fault_injection_report.md`，每个业务 smoke 的标准输出在同目录下的 `business-*.log`。如果中途 Ctrl-C，退出钩子会尝试启动已经停止的服务，但仍要人工执行 `docker compose ps` 确认状态。

## 5. 面试时能说什么，不能说什么

可以说：

> 我把故障处理拆成存活探针、就绪探针、数据保留和业务验收四层。Gateway、Logic、Transfer 的 readyz 检查各自真正需要的依赖；故障时先摘掉新流量，Gateway 连接由客户端自动重连，单聊使用 client_msg_id 重试，群聊依靠 Kafka 未提交 offset、retry/DLQ 和幂等 lease 恢复。我用 Compose 故障脚本依次停止 Redis、MySQL、Logic、Kafka、Transfer 和 Gateway，验证 readyz 失败、服务替换后恢复，并重新跑单聊、离线回放、ACK 和群聊 smoke。

不能说：

- WebSocket 连接可以无损迁移到另一个 Gateway。当前实现是断开后自动重连。
- 当前仓库已经实现 Redis Cluster、MySQL 主从自动切换或 Kafka 多副本生产高可用。仓库只提供稳定入口配置和本地单实例故障演练。
- K8s YAML 代表真实生产集群已经运行。当前证据是清单渲染、静态校验和本地演练。
- readyz 成功就代表所有业务都成功。仍然需要跑真实消息链路和查看指标。
