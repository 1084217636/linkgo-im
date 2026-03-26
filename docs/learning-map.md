# 学习地图

这份学习地图的目标不是“把所有文件看完”，而是让你按正确顺序掌握这个项目，最后能达到：

- 能独立讲清楚项目整体架构。
- 能按链路解释单聊、群聊、ACK、离线补偿、服务发现。
- 能回答每种技术为什么引入、解决了什么问题。
- 能把代码里的实现和简历里的表述一一对上。

---

## 0. 先确认你现在要掌握什么

这个项目的学习不是背 API，也不是背技术栈列表，而是掌握三层内容：

1. 代码层
   也就是“项目现在怎么跑”。
2. 设计层
   也就是“为什么要这样拆 Gateway / Logic / Transfer”。
3. 技术层
   也就是“Redis、Etcd、Kafka、JWT、Protobuf、go-zero 分别解决什么问题”。

如果你只看代码，不懂设计，你面试会卡在“为什么这样做”。
如果你只背设计，不看代码，你面试会卡在“具体怎么实现”。

---

## 1. 第一阶段：先建立全局视角

这一阶段的目标只有一个：
先知道系统里有哪些进程，每个进程负责什么，不要一开始陷进某个函数里。

### 先看这些文件

- `README.md`
- `docker-compose.yml`
- `cmd/gateway/main.go`
- `cmd/logic/main.go`
- `cmd/transfer/main.go`

### 你要回答的问题

- 系统为什么拆成 `gateway / logic / transfer` 三层？
- 哪个进程负责长连接？
- 哪个进程负责消息顺序号和持久化？
- 哪个进程负责 Kafka 异步消费？
- 哪些中间件是同步主链路，哪些是异步链路？

### 学习结果

看完这一轮，你应该能先讲一句总述：

这个项目是一个基于 go-zero 的分布式 IM，Gateway 负责接入和长连接，Logic 负责消息编排和顺序控制，Transfer 负责 Kafka 异步扩散和重试死信。

---

## 2. 第二阶段：按四条主链路学习

这一阶段不要按文件夹学，要按“业务链路”学。

### 链路一：登录与鉴权

学习顺序：

- `cmd/gateway/internal/handler/loginhandler.go`
- `cmd/gateway/internal/logic/loginlogic.go`
- `cmd/gateway/internal/svc/logicrouter.go`
- `cmd/logic/internal/server/logicserver.go`
- `cmd/logic/internal/logic/loginlogic.go`
- `internal/logic/handler.go`
- `internal/middleware/auth.go`

你要搞懂：

- 登录请求为什么先打 Gateway，不直接打 Logic？
- Gateway 为什么还要调 Logic 的 gRPC 登录？
- JWT 是在哪生成的？
- JWT 为什么适合 WebSocket 握手场景？

你最终要能讲：

用户先通过 REST 登录，Gateway 调 Logic 校验用户名密码，Logic 查询 MySQL 后生成 JWT，客户端再带 JWT 建立 WebSocket。

### 链路二：单聊消息主链路

学习顺序：

- `cmd/gateway/internal/handler/websockethandler.go`
- `internal/server/client.go`
- `internal/server/pool.go`
- `cmd/logic/internal/logic/pushmessagelogic.go`
- `internal/logic/handler.go`
- `internal/delivery/redis.go`
- `internal/server/manager.go`

你要搞懂：

- WebSocket 收到消息之后为什么不直接处理业务？
- Gateway 为什么要把消息转发给 Logic？
- 单聊消息从 Gateway 到 Logic 到 Redis 再到目标网关的路径是什么？
- 为什么这里要有 worker pool？

你最终要能画出一条消息路径：

客户端 A -> Gateway A -> Logic -> RedisDelivery -> Redis Pub/Sub -> Gateway B -> 客户端 B

### 链路三：群聊异步扩散链路

学习顺序：

- `internal/logic/handler.go`
- `cmd/logic/internal/svc/kafka_dispatcher.go`
- `cmd/transfer/main.go`
- `internal/delivery/redis.go`

你要搞懂：

- 为什么单聊可以同步投递，群聊不能简单同步 for 循环扇出？
- Kafka 在这里到底解决了什么？
- retry topic 和 dead-letter topic 是怎么工作的？

你最终要能讲：

群聊场景下 Logic 不直接扇出，而是先把任务写进 Kafka，Transfer 再异步消费并逐个投递，失败时进入重试和死信链路。

### 链路四：ACK、重连与离线补偿

学习顺序：

- `internal/delivery/redis.go`
- `internal/server/ack.go`
- `internal/server/sync.go`
- `internal/server/route.go`
- `internal/server/manager.go`

你要搞懂：

- 为什么消息“发出去了”不等于“用户真的收到了”？
- `pending_ack`、`ack_idx`、`offline_msg` 分别是干什么的？
- 为什么用户重连时要回放未 ACK 消息？
- 为什么我后面又补了 route session 的精确删除？

你最终要能讲：

服务端把消息先放进待确认集合，客户端 ACK 后再删除；如果客户端断线，重连后会按顺序重放未 ACK 消息，避免弱网场景下漏收。

---

## 3. 第三阶段：再按技术组件学习

这一阶段是“把技术栈变成自己的话”。

### Go-Zero

重点看：

- `cmd/gateway/main.go`
- `cmd/gateway/internal/config`
- `cmd/gateway/internal/handler`
- `cmd/gateway/internal/logic`
- `cmd/gateway/internal/svc`
- `cmd/gateway/internal/types`
- `cmd/logic/main.go`
- `cmd/logic/internal/server`
- `cmd/logic/internal/logic`
- `cmd/logic/internal/svc`

你要掌握：

- go-zero REST 脚手架的 `config / handler / logic / svc / types` 分层。
- go-zero zRPC 的 `config / server / logic / svc` 分层。
- `svc.ServiceContext` 的作用是统一注入依赖。
- `handler` 负责协议解析和响应。
- `logic` 负责业务编排。

### Protobuf 和 gRPC

重点看：

- `api/protocol.proto`
- `api/protocol.pb.go`
- `api/protocol_grpc.pb.go`

你要掌握：

- `.proto` 是统一契约。
- `WireMessage` 同时服务于 WebSocket 二进制帧和内部 gRPC 载荷。
- gRPC 在这里是内部服务调用，不是给前端直接用。

### Etcd

重点看：

- `internal/discovery/etcd.go`
- `cmd/gateway/internal/svc/logicrouter.go`
- `cmd/logic/etc/logic.yaml`

你要掌握：

- Logic 节点为什么要注册到 Etcd。
- Gateway 为什么要通过 Etcd 发现 Logic。
- 项目里用的是 Rendezvous Hash，也就是一致性哈希思路的一种实现。

### Redis

重点看：

- `internal/delivery/redis.go`
- `internal/server/ack.go`
- `internal/server/sync.go`
- `internal/logic/handler.go`

你要掌握：

- String：`route:<uid>`
- Set：`group_members:<gid>`、`user_groups:<uid>`
- Hash：`ack_idx:<uid>`
- ZSet：`pending_ack:<uid>`、`offline_msg:<uid>`
- Pub/Sub：`im_message_push:<gatewayID>`
- Lua：会话级 `seq` 递增

### Kafka

重点看：

- `cmd/logic/internal/svc/kafka_dispatcher.go`
- `cmd/transfer/main.go`

你要掌握：

- 为什么 Kafka 适合群聊削峰。
- 什么是 consumer group。
- 什么是 retry topic。
- 什么是 dead-letter topic。

### MySQL

重点看：

- `sql/init.sql`
- `internal/logic/handler.go`

你要掌握：

- 用户表和消息表分别干什么。
- 为什么 `messages` 要建 `session_id + seq` 索引。
- 历史消息为什么按会话维度查。

### JWT / 限流 / 心跳 / Prometheus

重点看：

- `internal/middleware/auth.go`
- `cmd/gateway/internal/middleware/authmiddleware.go`
- `internal/middleware/ratelimit.go`
- `cmd/gateway/internal/middleware/ratelimitmiddleware.go`
- `internal/server/client.go`
- `internal/metrics/metrics.go`

你要掌握：

- JWT 为什么适合无状态鉴权。
- 令牌桶限流是怎么工作的。
- 双向心跳为什么能清理僵尸连接。
- 指标系统为什么是工程化项目的加分项。

---

## 4. 目录理解地图

你可以把整个项目记成下面这张脑图：

- `api/`
  - 协议层
- `cmd/gateway/`
  - go-zero REST 接入层
- `cmd/logic/`
  - go-zero zRPC 逻辑层
- `cmd/transfer/`
  - Kafka 消费与异步投递
- `internal/logic/`
  - 核心消息业务
- `internal/delivery/`
  - Redis 投递、离线、ACK
- `internal/server/`
  - WebSocket 连接管理与回放
- `internal/discovery/`
  - Etcd 注册发现
- `internal/middleware/`
  - JWT 与限流基础能力
- `internal/metrics/`
  - Prometheus 指标
- `sql/`
  - 表结构
- `docs/`
  - 简历和面试材料

---

## 5. 建议你的实际学习顺序

### 第一天

- 只看 `README.md`
- 只看 `docker-compose.yml`
- 只看 `cmd/gateway/main.go`
- 只看 `cmd/logic/main.go`
- 只看 `cmd/transfer/main.go`

目标：
能讲清楚三个进程各自负责什么。

### 第二天

- 学登录链路
- 学单聊链路

目标：
能讲清用户登录、发单聊消息的全过程。

### 第三天

- 学 ACK
- 学离线补偿
- 学心跳

目标：
能讲清“可靠投递”到底靠什么保证。

### 第四天

- 学群聊异步链路
- 学 Kafka retry / dead-letter

目标：
能讲清群聊为什么要异步化。

### 第五天

- 学 Etcd
- 学一致性哈希
- 学 go-zero 分层

目标：
能讲清为什么这是一个分布式系统，而不是单机聊天程序。

### 第六天

- 学 Redis 各种数据结构
- 学 MySQL 表结构和索引
- 学 Prometheus 指标

目标：
能回答“为什么选这个中间件”和“为什么这样建表”。

### 第七天

- 对照 `docs/resume-project.md`
- 对照 `docs/interview-script.md`
- 自己脱稿讲 3 分钟、5 分钟、10 分钟版本

目标：
从“能看懂代码”升级到“能面试表达”。

---

## 6. 面试前你必须能回答的 20 个问题

1. 为什么要拆 Gateway、Logic、Transfer？
2. Gateway 为什么不直接处理消息业务？
3. 为什么登录要通过 Gateway 调 Logic？
4. 为什么消息协议改成 Protobuf？
5. 为什么要上 Etcd？
6. 什么是一致性哈希，你这里怎么用？
7. Redis 在这个项目里不只是缓存，具体承担了什么角色？
8. 为什么要用 Lua 生成 seq？
9. `session_id` 和 `message_id` 是怎么设计的？
10. ACK 解决了什么问题？
11. `pending_ack` 和 `offline_msg` 有什么区别？
12. 为什么重连时要回放未 ACK 消息？
13. 为什么群聊不能同步扇出？
14. Kafka 为什么能削峰？
15. retry topic 和 dead-letter topic 分别解决什么问题？
16. 为什么要有 worker pool？
17. JWT 为什么适合这里？
18. 令牌桶限流的原理是什么？
19. 为什么要暴露 Prometheus 指标？
20. go-zero 在这个项目里到底提供了什么价值？

如果这 20 个问题你都能顺下来，面试基本就稳了。

---

## 7. 你讲项目时推荐的表达顺序

永远按这个顺序讲，不要一上来背技术栈：

1. 项目目标是什么
2. 基础版 IM 遇到了什么分布式问题
3. 我怎么拆分服务
4. 每个中间件分别解决什么问题
5. 最终带来了什么收益

最稳的讲法是：

先讲问题，再讲方案，再讲收益。

---

## 8. 当前项目和简历的对应关系

目前已经可以比较稳地对应这些简历点：

- 基于 Go-Zero 构建 Gateway + Logic 架构
- gRPC + Etcd 服务注册发现
- 一致性哈希路由
- Protobuf 二进制协议
- Redis Lua 会话级顺序号
- ACK 未确认消息补偿
- Kafka 群聊异步扩散
- retry / dead-letter
- JWT 鉴权
- 令牌桶限流
- 双向心跳
- worker pool
- Prometheus 指标

有一个表达建议：

- “优化消息头”这句话面试时不要讲得太玄，最好落回“统一 Protobuf 二进制载荷、减少 JSON 冗余、保留长度编解码能力”。

---

## 9. 你现在最该做的事

不是继续加新功能。

而是先按这个顺序把项目真正讲熟：

1. 先记住三个服务的职责。
2. 再记住四条核心链路。
3. 再把每个中间件和问题一一对应。
4. 最后再训练口头表达。

只要你能把“为什么这样设计”说清楚，这个项目就已经够你打大多数校招 / 初中级后端面试了。
