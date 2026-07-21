# 01 项目定位与总体架构

## 1. 必背项目定位

### 20 秒

> LinkGo 是使用 Go 和 go-zero 开发的实时通信与游戏运营平台。核心采用 Gateway、Logic、Transfer 分层，使用 MySQL、Redis、Kafka 完成消息持久化、在线投递和群聊异步扩散，并补充红包、AI、运营配置、监控和 K8s 发布能力。

### 1 分钟

> LinkGo 的主线是分布式 IM。Gateway 面向客户端，负责 HTTP、JWT、WebSocket、ACK 和连接管理；Logic 负责消息幂等、会话顺序号、MySQL 落库和路由决策；Transfer 消费 Kafka，异步完成群聊成员扩散。MySQL 是最终事实来源，Redis 保存在线路由、pending ACK、离线索引和热点数据，Kafka 把大群扩散从同步主链路拆开。业务上实现了事务红包、AI 总结/问答，以及活动版本、审批、灰度发布、幂等道具发放、审计和回滚。工程上使用 GitHub Actions、Docker、K8s、Prometheus/Grafana 建立测试、发布和监控闭环。

## 2. 为什么拆成三个服务

### Gateway

职责：客户端协议和有状态连接。

- HTTP 登录与业务接口。
- JWT 鉴权、Origin 校验、限流。
- WebSocket 建连、心跳、读写。
- 本机连接表、ACK、离线回放。
- 将上行消息交给 Logic。

不负责：复杂业务决策、Kafka 群成员循环扩散。

### Logic

职责：核心业务编排。

- 校验消息和身份。
- client_msg_id 幂等。
- 生成 message_id、conversation_id、session_id、seq。
- MySQL 消息落库与会话更新。
- 单聊投递、群聊 Kafka 生产。

不负责：长期维护客户端 Socket；大群逐成员同步发送。

### Transfer

职责：可异步扩容的群聊派送。

- Fetch Kafka 消息。
- 查询群成员。
- 按成员幂等投递。
- 失败写 retry topic 或 DLQ。
- 耐久处理成功后提交位点。

### 标准回答

> 拆分依据不是为了“微服务数量多”，而是不同职责的资源模型不同。Gateway 是连接密集型，Logic 是业务和数据库密集型，Transfer 是异步吞吐型。分开后可以独立扩容，也能避免群聊慢任务阻塞长连接入口。

## 3. 总体数据流

```text
Client
  -> Gateway (HTTP/WS)
  -> Logic (gRPC)
  -> MySQL (最终历史)
  -> Redis (实时路由/待确认)

群聊额外：
Logic -> Kafka -> Transfer -> Redis -> Gateway -> Client
```

## 4. 同步链路和异步链路

同步：登录、单聊核心处理、消息落库、红包领取、运营写操作。

异步：群聊 fanout、Kafka retry/DLQ、活动 Outbox 缓存同步、部分 AI provider 调用的业务增强。

判断原则：用户必须立刻知道成功失败的核心一致性操作走同步；耗时、可重试、可削峰的扩散工作走异步。

## 5. 框架和协议

- go-zero REST：Gateway HTTP API。
- go-zero zRPC/gRPC：Gateway 调 Logic。
- Protobuf：内部 RPC 和 WebSocket 二进制消息结构。
- WebSocket：客户端实时双向通信。
- Etcd：Logic 服务发现。

## 6. 面试追问

### 为什么不用单体？

> 单体能更快做出 Demo，但连接管理、核心业务和群聊扩散会争用资源。项目把三种负载拆开，故障和扩容边界更清晰。代价是部署、调用链、配置和故障处理更复杂。

### 为什么 Gateway 是有状态的？

> WebSocket 连接实际存在某台 Gateway 内存里，因此连接本身有状态。项目用 Redis 保存 uid 到 gateway/connection 的路由，让其他服务能找到连接所在节点。

### Logic 能否横向扩容？

> 设计上可以。Gateway 通过 zRPC/Etcd 找 Logic，关键顺序依赖 Redis 会话 seq 和数据库约束，而不是单实例内存。但真实多实例容量仍需用压测证明，不能只凭架构宣称。

### 最大亮点是什么？

> 不是功能数量，而是消息可靠性和故障边界：双层幂等、会话 seq、pending ACK、重连补偿、Kafka 手动提交、成员级 lease 幂等、背压、指标告警，以及运营发布/回滚链路。

## 7. 代码入口

- `cmd/gateway/main.go`
- `cmd/gateway/internal/handler/routes.go`
- `cmd/logic/main.go`
- `internal/logic/handler.go`
- `cmd/transfer/main.go`
- `internal/server/`
- `internal/delivery/`

第一轮只记目录职责，不背函数实现。

## 8. 本章闭卷题

1. 用 20 秒介绍项目。
2. 三个服务各自负责什么？
3. 为什么群聊派送不放在 Logic 同步执行？
4. 哪些数据是最终事实，哪些是临时状态？
5. 拆服务带来了哪些代价？
6. Gateway 为什么不能完全无状态？
