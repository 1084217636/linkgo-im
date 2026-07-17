# LinkGo IM 项目完全学习手册

> 目标：用本文档替代当前 docs/ 下所有零散文档。读完你能经得起面试官对架构、技术选型、接口设计、优化方向的所有追问。

---

## 目录

1. [项目一句话概览](#1-项目一句话概览)
2. [架构全景图](#2-架构全景图)
3. [服务职责矩阵](#3-服务职责矩阵)
4. [核心业务流（4条链路）](#4-核心业务流4条链路)
5. [数据存储设计](#5-数据存储设计)
6. [协议与接口](#6-协议与接口)
7. [中间件与横切关注点](#7-中间件与横切关注点)
8. [技术选型原因](#8-技术选型原因)
9. [运维与可观测性](#9-运维与可观测性)
10. [项目中的设计边界](#10-项目中的设计边界)
11. [当前局限与优化方向](#11-当前局限与优化方向)
12. [面试常见追问清单](#12-面试常见追问清单)
13. [每日学习计划（7天）](#13-每日学习计划7天)

---

## 1. 项目一句话概览

**一个分布式即时通讯（IM）后端系统，使用 Go + go-zero 框架构建，通过 WebSocket（长连接）+ gRPC（服务间通信）+ Kafka（异步分发）+ Redis（在线状态/消息暂存）+ MySQL（持久化）实现单聊与群聊的消息实时投递。**

### 简历版（2句话）

基于 go-zero 微服务框架的分布式 IM 系统。Gateway 层负责 WebSocket 接入和 JWT 鉴权，Logic 层负责消息编序、存储和路由分发，Transfer 层负责 Kafka 消费实现群聊异步扩散；通过 Etcd 做服务发现，Redis 做在线路由和离线消息暂存，Protobuf 做二进制通信协议。

---

## 2. 架构全景图

```
                              ┌──────────────┐
      浏览器 / 客户端          │    Etcd      │  服务发现 & 注册
          │                   │  (注册中心)    │
          │ WebSocket         └──────┬───────┘
          │ + HTTP REST              │
          ▼                          │
┌──────────────────────┐             │
│   Gateway (API网关)   │◄────────────┘
│   ├─ HTTP 路由        │    发现 Logic 节点
│   ├─ JWT 鉴权         │
│   ├─ 令牌桶限流        │
│   ├─ WebSocket 管理    │
│   └─ 路由注册(Redis)   │
└──────┬───────────────┘
       │  gRPC (PushMessage/Login/GetHistory)
       ▼
┌──────────────────────┐
│   Logic (逻辑服务)     │───────► MySQL (消息/用户持久化)
│   ├─ 消息编序(Lua)     │
│   ├─ 单聊直投          │───────► Kafka (群聊异步分发)
│   ├─ 群聊Kafka分发     │
│   └─ JWT签发           │
└──────┬───────────────┘
       │  Kafka consume
       ▼
┌──────────────────────┐
│   Transfer (传递服务)  │───────► Redis (消息投递)
│   ├─ 群聊消息消费       │
│   ├─ 重试机制(3次)     │
│   └─ DLQ死信处理       │
└──────────────────────┘
       
       ┌──────────────────────┐
       │   Redis (状态中心)     │
       │   ├─ route:<uid>      │  在线路由 (gateway_id|session_id)
       │   ├─ seq:<sessionId>  │  消息序列号 (Lua原子递增)
       │   ├─ pending_ack:<uid>│  待确认消息
       │   ├─ offline_msg:<uid>│  离线消息
       │   ├─ ack_idx:<uid>    │  确认索引
       │   ├─ group_members:<gid>│ 群成员
       │   └─ user_groups:<uid>│ 用户所属群
       │   + Pub/Sub 跨Gateway推送
       └──────────────────────┘
```

### 数据流方向

```
客户端 ──WebSocket──► Gateway ──gRPC──► Logic ──Kafka──► Transfer ──Redis Pub/Sub──► Gateway ──WebSocket──► 目标客户端
                  │                   │
                  │                   ├──► MySQL (持久化消息)
                  │                   │
                  ├──► Redis (路由注册) │
                  │                   │
                  └──► Redis Pub/Sub (接收推送)
```

---

## 3. 服务职责矩阵

| 职责 | Gateway | Logic | Transfer |
|------|---------|-------|----------|
| 协议 | HTTP/REST + WebSocket | gRPC | Kafka 消费 |
| 鉴权 | JWT 验证中间件 | JWT 签发(Login) | 无 |
| 限流 | 令牌桶(20/s REST, 5/s WS) | 无 | 无 |
| 长连接管理 | gorilla/websocket | 无 | 无 |
| 路由注册 | 向 Redis 写 route 信息 | 无 | 无 |
| 消息编序 | 无 | Lua 脚本原子递增 seq | 无 |
| 消息存储 | 无 | MySQL INSERT | 无 |
| 历史查询 | 透传 gRPC | MySQL SELECT | 无 |
| 单聊投递 | 无 | 直投 Redis | 无 |
| 群聊投递 | 无 | 写 Kafka | 读 Kafka → 投 Redis |
| 重试/DLQ | 无 | 无 | 3次重试 → 死信队列 |
| 健康检查 | /healthz /readyz /metrics | gRPC 探针 | /healthz /readyz /metrics |
| 水平扩展 | 多实例(gateway-a/b/c) | 多实例 | 多实例(同消费组) |

---

## 4. 核心业务流（4条链路）

### 4.1 用户登录

```
POST /api/v1/login {username, password}
        │
        ▼
  [RateLimit 中间件]     ← 令牌桶限流
        │
        ▼
  LoginHandler           ← httpx.Parse 解析请求
        │
        ▼
  LoginLogic.Login()     ← 通过 Etcd 发现 Logic 节点
        │
        ▼  gRPC: cli.Login()
  Logic.Login()
    ├─ MySQL: SELECT user_id, password FROM users WHERE username=?
    ├─ 密码比对
    ├─ middleware.GenerateToken(uid) → HS256 JWT, 24h过期
    └─ 返回 {user_id, token}
        │
        ▼
  客户端收到 token → 存入 localStorage
```

### 4.2 WebSocket 建立长连接

```
GET /ws?token=<jwt>
        │
        ▼
  [Auth 中间件]          ← 解析 JWT, 提取 userID 注入 context
  [RateLimit 中间件]     ← WebSocket 专用限流器 (5/s)
        │
        ▼
  WebSocketHandler
    ├─ HTTP Upgrade → WebSocket
    ├─ server.NewClientConn(conn, sessionID)
    ├─ server.Manager.Add(userID, clientConn)     ← 注册到全局连接管理器
    ├─ server.RefreshRoute(Redis)                  ← route:<uid> = "gatewayID|sessionID", TTL 75s
    ├─ server.SyncOfflineMessages(Redis)           ← 回放 pending_ack 中的离线消息
    │
    └─ server.StartClientLoop()                    ← 消息收发主循环
         ├─ 收到 ACK → AckMessage(Redis)           ← 清除 pending_ack
         ├─ 收到 HEARTBEAT → RefreshRoute + PONG
         └─ 收到 NORMAL → PushWorkerPool.Submit()  ← gRPC 发送到 Logic
```

### 4.3 单聊消息发送（端到端）

```
发送方客户端
  │  WebSocket Binary (Protobuf WireMessage)
  │  {from, to, to_type:"user", body, msg_type:NORMAL}
  ▼
Gateway-A
  ├─ 收到 WireMessage
  ├─ PushWorkerPool.Submit(uid, logic, data)
  │    │ gRPC: cli.PushMessage()
  │    ▼
  Logic
  │  ├─ protobuf 反序列化
  │  ├─ normalizeFrame()        ← 校验 from/to/body, 设置 msg_type
  │  ├─ buildSessionID()        ← "c2c:userA:userB" (字母排序确保唯一)
  │  ├─ nextSequence(Lua)       ← INCR seq:<sessionID>
  │  │    └─ message_id = "sessionID-seq"
  │  ├─ resolveRecipients()     ← to_type="user" → 直接返回 [targetUser]
  │  ├─ dispatchToRecipients()  ← 遍历 recipients:
  │  │    └─ RedisDelivery.Deliver()
  │  │         ├─ protobuf Marshal → base64编码
  │  │         ├─ trackPendingAck (ZADD pending_ack:<uid>, HSET ack_idx:<uid>)
  │  │         ├─ GET route:<targetID>
  │  │         ├─ 在线: PUBLISH im_message_push:<gatewayID> {target_id, payload_b64}
  │  │         └─ 离线: ZADD offline_msg:<uid>
  │  │
  │  └─ go saveMessage()        ← 异步写入 MySQL
  │
  ▼
Redis Pub/Sub 广播到 "im_message_push:<gatewayID>"
  │
  ▼
Gateway-B (目标用户所在的 Gateway)
  ├─ SubscribeRedis() 收到推送
  ├─ Manager.GetConn(targetID)  ← 找到本地连接
  └─ conn.WriteBinary(payload)  ← 通过 WebSocket 推送给客户端
  │
  ▼
接收方客户端
  └─ 收到消息 → 发送 ACK (WireMessage {msg_type:ACK, ack_message_id})
       │
       ▼
     Gateway → AckMessage(Redis) ← ZREM pending_ack, HDEL ack_idx
```

### 4.4 群聊消息发送

```
与单聊的区别点在 Logic 层:

Logic.PushMessage()
  ├─ ...
  ├─ resolveRecipients()        ← to_type="group" → SMEMBERS group_members:<gid>
  │    └─ 排除发送者自身
  │
  ├─ GroupDispatcher.PublishGroupDispatch()  ← 不走直投，写 Kafka
  │    └─ Kafka Message {frame, recipients, attempt}
  │
  ▼
Transfer (消费 Kafka)
  ├─ 读取消息 → protobuf Marshal
  ├─ 遍历 recipients → RedisDelivery.Deliver()
  ├─ 成功 → ack Kafka offset
  ├─ 失败 → 重试 topic (最多3次)
  └─ 超过3次 → 死信队列 topic
```

**为什么群聊走 Kafka？** 群聊有 N 个接收者，如果 Logic 直接逐个投递，会阻塞 gRPC 响应，且无法保证所有投递都成功。走 Kafka 实现异步解耦，Transfer 可以独立扩容消费。

---

## 5. 数据存储设计

### 5.1 MySQL（持久化）

**users 表**
```sql
id        BIGINT UNSIGNED AUTO_INCREMENT  -- 主键
user_id   VARCHAR(64) UNIQUE             -- 用户ID (业务主键)
username  VARCHAR(32) UNIQUE             -- 用户名
password  VARCHAR(128)                    -- bcrypt 哈希；旧明文仅兼容迁移
```

**messages 表**
```sql
id          BIGINT UNSIGNED AUTO_INCREMENT
message_id  VARCHAR(160) UNIQUE           -- 全局唯一消息ID = "sessionID-seq"
session_id  VARCHAR(128)                  -- 会话ID = "c2c:userA:userB" 或 "group:G001"
seq         BIGINT                        -- 会话内序号
from_uid    VARCHAR(64)
to_id       VARCHAR(64)                   -- 目标 (用户ID 或 群ID)
to_type     ENUM('user', 'group')
content     TEXT                          -- 消息体
create_time BIGINT                        -- Unix 毫秒

INDEX (session_id, seq)                   -- 拉取历史消息
INDEX (session_id, create_time)           -- 按时间范围查询
```

**关键设计决策：session_id 生成规则**
- 单聊: `"c2c:" + sort(from, to).join(":")` — 例如 `"c2c:userA:userB"`
  - 双方排序确保 A→B 和 B→A 共享同一个 session
- 群聊: `"group:" + groupID` — 例如 `"group:G001"`

### 5.2 Redis（运行态数据）

| Key | 类型 | 用途 | TTL |
|-----|------|------|-----|
| `route:<uid>` | String | 用户→Gateway映射 `"gatewayID\|sessionID"` | 75s (心跳刷新) |
| `seq:<sessionID>` | String | 会话消息序号 (Lua原子递增) | 7天 |
| `pending_ack:<uid>` | ZSET | 待确认消息列表 (score=时间戳) | 无 |
| `offline_msg:<uid>` | ZSET | 离线消息 | 无 |
| `ack_idx:<uid>` | Hash | messageID→base64(protobuf payload) | 无 |
| `group_members:<gid>` | Set | 群成员列表 | 无 |
| `user_groups:<uid>` | Set | 用户加入的群列表 | 无 |
| `im_message_push:<gatewayID>` | Pub/Sub Channel | 跨Gateway推送通道 | — |

**关键设计决策：为什么用 Lua 脚本生成 seq？**
```
EVAL "local n = redis.call('INCR', KEYS[1]); redis.call('PEXPIRE', KEYS[1], ARGV[1]); return n"
```
INCR + PEXPIRE 需要两次 Redis 调用，在并发场景下需要原子性。Lua 脚本在 Redis 内原子执行，保证 seq 严格递增且不会因 TTL 设置失败导致 key 永远不过期。这是 Redis 做分布式计数器的标准做法。

### 5.3 Kafka

| Topic | 生产者 | 消费者 | 用途 |
|-------|--------|--------|------|
| `group_message_dispatch` | Logic | Transfer | 群聊消息分发 |
| `group_message_retry` | Transfer(重试) | Transfer | 失败重试 |
| `group_message_dlq` | Transfer(终态) | 运维人工处理 | 死信队列 |

消费者组 `transfer-group` 确保同一个 topic 被 Transfer 实例均匀消费（Kafka 分区分配机制）。

---

## 6. 协议与接口

### 6.1 gRPC 服务定义（Proto）

**文件**: `api/protocol.proto`

```protobuf
service Logic {
    rpc Login(LoginReq) returns (LoginReply);
    rpc PushMessage(PushMsgReq) returns (PushMsgReply);
    rpc UserLogin(UserLoginReq) returns (UserLoginReply);
    rpc GetHistory(GetHistoryReq) returns (GetHistoryReply);
}
```

**WireMessage**（WebSocket 二进制帧结构）:
```
message_id      — 全局唯一消息ID
session_id      — 会话ID
seq             — 会话内序号
from / to       — 发送者/目标
to_type         — "user" | "group"
msg_type        — NORMAL | HEARTBEAT | ACK | SYSTEM
body            — 消息内容
sent_at         — 发送时间(Unix毫秒)
ack_message_id  — ACK确认的消息ID
```

### 6.2 REST API（go-zero goctl 生成）

**文件**: `cmd/gateway/gateway.api`

| Method | Path | 鉴权 | 限流 | 用途 |
|--------|------|------|------|------|
| POST | `/api/v1/login` | 无 | ✓(20/s) | 用户登录 |
| GET | `/api/v1/history?target_id=xxx` | JWT | ✓(20/s) | 拉取历史消息 |
| POST | `/api/v1/group/create` | JWT | ✓(20/s) | 创建群组 |
| GET | `/api/v1/user/groups` | JWT | ✓(20/s) | 查用户所属群 |
| GET | `/ws?token=xxx` | JWT | ✓(5/s) | WebSocket连接 |
| GET | `/healthz` | 无 | 无 | 存活探针 |
| GET | `/readyz` | 无 | 无 | 就绪探针(含 Redis 检查) |
| GET | `/metrics` | 无 | 无 | Prometheus 指标 |

### 6.3 各层接口契约关系

```
客户端 ←→ Gateway:   WebSocket (Protobuf 二进制帧)
                      HTTP REST (JSON)

Gateway ←→ Logic:    gRPC (Protobuf)
                       ├─ Login(LoginReq) → LoginReply
                       ├─ PushMessage(PushMsgReq) → PushMsgReply
                       └─ GetHistory(GetHistoryReq) → GetHistoryReply

Logic → Transfer:     Kafka (JSON 包装的 groupDispatchJob)
                        └─ {frame: WireMessage, recipients: [...], attempt: N}

Transfer → Redis:      Pub/Sub 推送 (JSON PushEnvelope)
                        └─ {target_id, payload_b64}

Logic → Redis:         直接写入 (route 查询, pending_ack, offline_msg)
```

**为什么用 Protobuf 而不是 JSON？**
1. 序列化体积小 60-80%（WebSocket 长连接每条消息都要传）
2. 强类型契约（proto 文件是 Gateway/Logic/Transfer 的唯一真相源）
3. 跨语言支持（如果将来用其他语言写服务）

**为什么 HTTP 接口用 JSON？**
1. 登录和历史查询是 RESTful 操作，JSON 方便客户端开发
2. go-zero 框架原生支持 httpx.Parse JSON/form 绑定
3. 浏览器原生可调试（DevTools Network Tab）

---

## 7. 中间件与横切关注点

### 7.1 JWT 鉴权中间件

**文件**: `cmd/gateway/internal/middleware/authmiddleware.go`
**JWT 实现**: `internal/middleware/auth.go`

流程：
1. 从 `Authorization: Bearer <token>` 或 URL `?token=xxx` 提取 token
2. `ParseToken()` → HMAC-SHA256 验证签名 + 过期检查
3. 解析出 `user_id` → 注入 `context.Context`
4. 下游逻辑通过 `UserIDFromContext(ctx)` 获取当前用户

**为什么 JWT 而不是 Session？**
- 无状态：Gateway 多实例部署，不需要共享 session 存储
- 适合微服务：token 可以在 Gateway→Logic 之间传递用户身份
- 过期时间 24h：兼顾安全和用户体验

### 7.2 令牌桶限流中间件

**文件**: `internal/middleware/ratelimit.go`（核心算法）
**Gateway 中间件**: `cmd/gateway/internal/middleware/ratelimitmiddleware.go`

算法：
```
每个 key（用户ID 或 IP）维护:
  tokens:     当前令牌数 (max=capacity)
  lastRefill: 上次补充时间

Allow(key):
  elapsed = now - lastRefill
  tokens = min(capacity, tokens + elapsed * rate)
  if tokens >= 1: tokens--, return true
  else: return false
```

两套限流器：
| 限流器 | 速率 | 容量 | 作用域 |
|--------|------|------|--------|
| RestLimiter | 20/s | 40 | REST API (/api/v1/*) |
| WsLimiter | 5/s | 10 | WebSocket 连接 (/ws) |

**为什么 REST 和 WS 分开限流？** WebSocket 是持久连接，频繁重连消耗资源更大；REST 是短连接，QPS 更高但不占用长连接资源。

### 7.3 工作池（PushWorkerPool）

**文件**: `internal/server/pool.go`

```
64 个 uid 固定 shard
每个 shard：1 个 worker + 64 容量的缓冲 channel

Submit(ctx, uid, logic, data) → FNV(uid) % 64 → 非阻塞写入对应 shard
  ├─ accepted: shard 单 worker 按 FIFO 执行 logic.PushMessage(gRPC)
  ├─ queue_full: 记录拒绝指标
  ├─ pool_closed: 关闭后拒绝新任务
  └─ context_canceled: 已取消任务不再入队
```

**为什么需要分片工作池？** 有界队列限制并发 gRPC 调用和内存增长；固定 uid 分片让同一发送者始终由同一 shard 串行处理，同时不同 shard 保持并行。当前服务端已有明确拒绝结果、指标和受控关闭，但客户端错误帧仍待补齐。

---

## 8. 技术选型原因

这里按面试追问的「为什么选 X 而不是 Y」逻辑组织。

### 8.1 为什么选 go-zero 框架？

| 能力 | 我们用到的 | 替代方案对比 |
|------|-----------|-------------|
| REST API 脚手架 | goctl 生成 handler/logic/routes | 自己手写 gin/echo 路由 → 维护成本高 |
| gRPC 集成 | zrpc 一行创建 client/server | grpc 原生 → 要手写连接池/拦截器 |
| 服务发现 | Etcd 集成（zrpc 内置） | 手写 etcd client → 代码量 3 倍 |
| 配置管理 | conf.MustLoad YAML + env override | viper → 要额外集成 |
| 中间件链 | rest.WithMiddlewares 声明式 | gin.Use → 只能全局，不能按路由组 |
| 限流熔断 | 框架内置（本项暂未启用） | hystrix-go → 额外集成 |
| 日志 | logx 结构化日志 | zap/logrus → 额外配置 |

**结论**：go-zero 的 goctl 代码生成 + zrpc 服务发现等于帮你省掉 40% 脚手架代码，同时保证项目结构统一。本项目 RPC 部分（Logic）完全用 go-zero zrpc；API 部分（Gateway）改用 goctl 生成的标准 REST 结构。

### 8.2 为什么分 Gateway、Logic、Transfer 三层？

**不是微服务分层，而是职责分离**：

- **Gateway**：连接层。管理 WebSocket 长连接，鉴权，限流，路由注册。无业务逻辑。
- **Logic**：业务层。编序、存储、分发决策（单聊直投 vs 群聊 Kafka）。不感知连接状态。
- **Transfer**：投递层。异步消费群聊消息，带重试和死信处理。不感知业务逻辑。

**如果不分层会怎样？** 所有逻辑写一起 → WebSocket 连接和 MySQL 写入在同一个进程 → 数据库慢查直接影响在线用户的消息延迟。分离后，Logic 慢了不影响 Gateway 的长连接保活，Transfer 挂了不影响单聊。

### 8.3 为什么群聊用 Kafka 而不是 Redis Pub/Sub？

| 对比项 | Redis Pub/Sub | Kafka |
|--------|--------------|-------|
| 消息可靠性 | 无持久化，消费者离线就丢 | 持久化到磁盘 + consumer group offset |
| 消费模式 | 推模式，消费者必须在线 | 拉模式，消费者按自己节奏消费 |
| 重试机制 | 无 | Offset 不回退即可自动重试 |
| 顺序保证 | 无 | 分区内有序 |
| 适用场景 | 实时推送（Gateway→客户端） | 可靠异步任务（群聊扩散） |

**群聊走 Kafka 的核心原因**：一条群聊消息要投递给 N 个人，如果 Transfer 消费时某个用户的 Redis 写入失败了，Kafka offset 不动就能自然重试。Redis Pub/Sub 做不到。

### 8.4 为什么用 Redis 做在线路由而不是 Etcd？

- **TTL 语义**：Redis `SETEX` 天然适合「心跳过期」场景。Etcd lease 也可以，但 Etcd 定位是强一致性配置存储，不适合高频写入（每次 WebSocket 连接/重连/心跳都要写 route）。
- **Pub/Sub**：需要广播推送消息到指定 Gateway 实例，Redis Pub/Sub 按 channel 过滤天然匹配。
- **Lua 脚本**：seq 原子递增需要 Lua，Redis 原生支持。

**如果做优化**，可以用 Etcd 存 Gateway 实例列表 + Redis 做 route 路由表（各取所长），但目前直接放 Redis 足够了。

### 8.5 为什么用 gorilla/websocket 而不是 nhooyr.io/websocket？

gorilla/websocket 是目前 Go 生态使用最广的 WebSocket 库，API 成熟，社区案例多。nhooyr 更新但少人用，排查问题找不到参考。

---

## 9. 运维与可观测性

### 9.1 健康检查

| 端点 | 用途 | 检查内容 |
|------|------|---------|
| GET `/healthz` | K8s livenessProbe | 进程存活即可返回 200 |
| GET `/readyz` | K8s readinessProbe | Redis Ping 通过 → 200, 否则 503 |

K8s 配置示例 (`deploy/k8s/gateway.yaml`):
```yaml
livenessProbe:
  httpGet: { path: /healthz, port: 8090 }
readinessProbe:
  httpGet: { path: /readyz, port: 8090 }
```

### 9.2 Prometheus 指标

**文件**: `internal/metrics/metrics.go`

| 指标 | 类型 | 标签 | 意义 |
|------|------|------|------|
| `linkgo_ws_connections` | Gauge | — | 当前活跃 WebSocket 连接数 |
| `linkgo_inbound_messages_total` | Counter | source(gateway/logic), type(normal/heartbeat/ack/decode_error) | 入站消息量 |
| `linkgo_outbound_messages_total` | Counter | target(gateway/logic), result(success/queue_full/error) | 出站消息量 |
| `linkgo_ack_operations_total` | Counter | result(success/miss/error) | ACK 操作量 |
| `linkgo_kafka_operations_total` | Counter | stage(consume/retry_write/dlq_write), result | Kafka 处理量 |
| `linkgo_rate_limit_hits_total` | Counter | route | 限流触发次数 |

可用的 Grafana 看板指标：
- QPS: `rate(linkgo_inbound_messages_total[1m])`
- 错误率: `rate(linkgo_outbound_messages_total{result!="success"}[1m])`
- 限流触发频率: `rate(linkgo_rate_limit_hits_total[1m])`
- Kafka 积压: `kafka_consumergroup_lag` (需 jmx_exporter)

---

## 10. 项目中的设计边界

面试官常问「你这个系统的边界在哪里」，以下需要明确回答：

### 10.1 ACK 是投递确认，不是已读确认

当前 ACK 机制是「消息到达客户端后，客户端发送 ACK 确认已收到」。这确认的是**网络投递成功**，不是**用户已读**。如果做已读回执，需要客户端在用户点开消息时发送独立的 READ ACK。

### 10.2 消息有序性仅限于会话级别

通过 `session_id + seq` 保证**一个会话内**消息有序（如 userA→userB 的聊天）。全局无序，跨会话无序。这是微信/QQ 的设计方式，不是 Bug。

### 10.3 消息存储是即发即存（不需要漫游）

当前 `saveMessage()` 在发消息时直接 INSERT。如果要做多端同步（手机/PC/Web 同时在线），需要：
- 维护一个 `消息漫游服务`
- 客户端携带 `last_seq` 拉取增量
- 服务端维护每个用户的 `received_seq` 游标

### 10.4 群组成员管理是简化版

当前用 Redis Set 存成员，没有持久化到 MySQL。生产环境需要：
- MySQL `group_members` 表 + Redis 缓存（Cache-Aside 模式）
- 成员变更时需要通知在线群成员（群成员变更事件）
- 考虑大群（万人群）时的成员查询性能

### 10.5 没有做消息撤回/删除/编辑

这些需要：
- 消息状态机（normal → recalled/deleted/edited）
- 撤回/删除/编辑的推送协议（新的 MsgType 或扩展字段）
- 客户端 UI 的状态更新逻辑

---

## 11. 当前局限与优化方向

面试官问「有什么可以优化的」时候，按以下优先级回答：

### P0 — 生产必须

1. **密码明文存储** → bcrypt 哈希
   - 当前 `users.password` 存 bcrypt；`Login()` 使用 bcrypt 校验，并在旧明文账号首次成功登录后原子升级
2. **数据库连接池参数化** → 当前 hardcode 100/10，应可配置
3. **JWT secret 不能 hardcode 默认值** → 生产环境必须从 K8s Secret 注入
4. **消息体未加密** → WebSocket 传输应有 TLS + 可选的端到端加密

### P1 — 性能优化

1. **Gateway 到 Logic 的连接池**
   - 当前每个请求创建一个 gRPC client，高频场景下应复用连接
   - 实际已通过 `LogicRouterPool` 缓存 —— 可以进一步加连接健康检查
2. **Redis Pub/Sub 消息丢失**
   - Pub/Sub 无持久化，Gateway 重启期间的消息会丢
   - 方案：引入 Redis Stream（go-zero 1.10+ 支持）
3. **消息写入 MySQL 用异步 goroutine**
   - 当前 `go saveMessage()` 没有错误重试
   - 方案：写入失败的消息放回队列重试
4. **离线消息排序**
   - 当前 `pending_ack` ZSET 按时间排序，但客户端连上后一次性回放
   - 大量离线消息时应分页推送

### P2 — 功能完善

1. **接入层负载均衡**
   - 当前靠 docker-compose 端口映射，K8s 用 Service
   - 应加 Nginx/Envoy 做 WebSocket 负载均衡（需 sticky session）
2. **消息漫游（多端同步）**
   - 维护 `user_seq:<uid>` 游标
   - 提供增量拉取接口 `GET /sync?since_seq=N`
3. **敏感词过滤**
   - 在 Logic 层 `normalizeFrame()` 后接入过滤服务
4. **文件/图片消息**
   - WireMessage 加 `media_type` 和 `media_url` 字段
   - 接入 OSS/CDN

---

## 12. 面试常见追问清单

### 架构类

**Q: 为什么分三层而不是两层？**
> Gateway 负责连接管理（无状态），Logic 负责业务处理（有状态），Transfer 负责异步投递（纯消费者）。三层可以独立扩缩容 —— 用户多了加 Gateway，消息量大了加 Transfer，数据库慢了只影响 Logic。

**Q: Gateway 挂了用户怎么办？**
> 客户端需要实现自动重连 + 换 Gateway。通过 Etcd/负载均衡器找到健康的 Gateway。重连后调用 SyncOfflineMessages 回放离线消息。

**Q: 怎么保证消息不丢？**
> 三个层面：①发送方→Gateway：WebSocket TCP 保证；②Gateway→Logic：gRPC 同步调用，失败则客户端重发；③Logic→目标：pending_ack + 离线消息队列 + Kafka 重试/DLQ。

### 数据类

**Q: seq 为什么用 Redis Lua 而不是 MySQL 自增？**
> ①IM 场景消息量大，MySQL 自增有写锁竞争；②seq 是会话级别的（不同会话独立计数），MySQL 表自增是全局的；③Lua 原子操作性能远高于数据库往返。

**Q: pending_ack 和 offline_msg 有什么区别？**
> pending_ack 是所有已发出但未确认的消息（在线和离线都有）；offline_msg 是发送时目标不在线，额外存一份等用户上线时从 SyncOfflineMessages 推送。ACK 成功后两个都清理。

**Q: 群聊消息为什么不直接写 Redis 而是走 Kafka？**
> 群聊要投递给 N 个用户。如果在 Logic 里逐个 Deliver，①阻塞 gRPC 响应；②部分投递失败难处理。走 Kafka：Logic 写一条 → Transfer 消费 N 条投递 → 失败重试 → 超限进 DLQ。

### Go 语言类

**Q: sync.Map 和普通 map+mutex 的区别？为什么 ClientManager 用 sync.Map？**
> sync.Map 适合「读多写少」且「key 集合相对稳定」的场景。ClientManager 的 Add/Remove 操作相对 GetConn（推送时查连接）少得多。但如果需要遍历所有连接（广播场景），sync.Map 的 Range 性能不如 map+mutex。

**Q: 为什么是 64 个 shard、每个 shard 队列容量 64？**
> 这样总排队容量约为 4096，同时把同一 uid 固定到单 worker，避免多个 worker 并行提交同一发送者消息。两个数字仍是当前工程默认值，必须结合队列深度、拒绝率和处理时延压测调整，不能说成通用最优值。

### 协议类

**Q: 为什么 WebSocket 用二进制帧（Protobuf）而不是文本帧（JSON）？**
> ①体积：Protobuf 比 JSON 小 60-80%，移动端弱网场景优势明显；②性能：Protobuf 序列化/反序列化比 JSON 快 5-10 倍；③类型安全：proto 文件是强契约，编译期能发现字段不匹配。

**Q: HEARTBEAT 为什么走 WebSocket 而不是独立的 TCP 连接？**
> 复用 WebSocket 连接，减少客户端连接数和移动端耗电。IM 场景下，心跳包很小（Protobuf 只有几个字节），不影响消息收发。

---

## 13. 每日学习计划（7天）

### Day 1: 跑起来 + 看结构
- [ ] `docker-compose up --build` 启动全部服务
- [ ] 打开 `public/index.html` 调试页面，登录 userA/userB，发几条消息
- [ ] 对照本文档 [架构全景图](#2-架构全景图) 理解每个容器的作用

### Day 2: 协议层
- [ ] 读懂 `api/protocol.proto` 每个 message 和 field
- [ ] 读懂 `cmd/gateway/gateway.api` 每个 endpoint
- [ ] 回答：为什么 REST 接口是 JSON 而 WebSocket 是 Protobuf？

### Day 3: 登录 + WebSocket 链路
- [ ] 从 `POST /login` Handler → Logic → LoginLogic → MySQL → JWT 签发，走一遍
- [ ] 从 `GET /ws` Handler → WebSocketHandler → StartClientLoop，走一遍
- [ ] 理解 `route:<uid>` 的路由机制

### Day 4: 消息发送链路
- [ ] 从 "客户端发送 Protobuf 帧" → Gateway → gRPC → Logic → Redis → 目标 Gateway，走一遍单聊流程
- [ ] 理解 Lua 脚本生成 seq + message_id 的过程
- [ ] 理解 pending_ack 的实现

### Day 5: 群聊 + Kafka
- [ ] 走一遍群聊流程：Logic → Kafka → Transfer → Redis → Gateway → 客户端
- [ ] 理解重试机制的 3 次限制 + DLQ
- [ ] 回答：为什么群聊和单聊的投递路径不一样？

### Day 6: 横切关注点
- [ ] JWT 鉴权中间件的实现
- [ ] 令牌桶限流算法
- [ ] PushWorkerPool 的设计
- [ ] Prometheus 指标含义

### Day 7: 模拟面试
- [ ] 不看文档，画出架构图
- [ ] 口述单聊消息的完整流程（从客户端 A 到客户端 B）
- [ ] 回答 [面试常见追问清单](#12-面试常见追问清单) 中每个问题
- [ ] 说出至少 3 个当前项目的局限和优化方向
