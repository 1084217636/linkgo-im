# LinkGo-IM 性能测试报告

## 0. 当前协议实测补充（2026-05-14）

本轮测试使用 `benchmark/local_core_bench.go`，在本机启动 10 个轻量 Gateway HTTP/WebSocket 入口，Redis 使用本地 `redis-server 7.0.15`，Logic 使用内存中的 `LogicHandler`，覆盖 JWT 握手、WebSocket 长连接、Protobuf 心跳、Redis route、Redis Pub/Sub、单聊投递、pending ACK 和 ACK 清理。

说明：由于 Docker Hub 镜像拉取超时，本轮没有跑完整 Docker Compose 里的 MySQL/Kafka/Transfer 容器链路；下面结果是连接层和核心投递链路的本地最小实测，不等同于完整生产部署容量。

### 0.1 10 Gateway / 1w WebSocket 心跳

命令：

```bash
.bench/bin/local_core_bench \
  -redis-addr=127.0.0.1:6380 \
  -start-port=18090 \
  -gateways=10 \
  -per-gateway=1000 \
  -heartbeat-duration=30s \
  -heartbeat-every=5s \
  -pairs=100 \
  -messages=1000
```

结果：

- WebSocket 连接：`10000 / 10000` 成功，失败 `0`
- 心跳发送：`59957`
- 心跳成功：`59957`
- 心跳失败：`0`
- 心跳成功率：`100%`
- 心跳平均延迟：`2.10ms`
- 心跳 P50：`1.79ms`
- 心跳 P95：`4.57ms`
- 心跳 P99：`6.78ms`
- 心跳最大延迟：`13.02ms`
- 进程最大 RSS：`577288KB`，约 `563MB`

### 0.2 单聊端到端投递（100 对连接 / 1000 条）

- 发送：`1000`
- 接收：`1000`
- ACK：`1000`
- 超时：`0`
- ACK 超时率：`0%`
- 平均投递延迟：`89.90ms`
- P50：`89.95ms`
- P95：`166.91ms`
- P99：`174.78ms`
- 最大延迟：`177.55ms`

### 0.3 单聊端到端投递（100 对连接 / 2000 条）

- 发送：`2000`
- 接收：`2000`
- ACK：`2000`
- 超时：`0`
- ACK 超时率：`0%`
- 平均投递延迟：`185.56ms`
- P50：`184.26ms`
- P95：`328.47ms`
- P99：`342.83ms`
- 最大延迟：`346.31ms`
- 进程最大 RSS：`46560KB`，约 `45MB`

### 0.4 突发退化观察（100 对连接 / 5000 条）

- 发送：`5000`
- 接收：`4225`
- ACK：`4225`
- 30s 内超时：`775`
- ACK 超时率：`15.50%`
- 已收到消息平均延迟：`368.30ms`
- P95：`657.45ms`
- P99：`684.56ms`

结论：当前核心链路在 1w WebSocket 心跳与 100 对连接 / 2000 条单聊消息下表现稳定；5000 条突发时开始出现积压和超时，说明后续需要继续优化推送 worker、ACK 索引读取、批量写 Redis 和背压策略。

## 1. 测试环境
- OS: Linux (WSL2 容器)
- CPU/内存: 16 核 / 16GB
- Docker Compose 服务: gateway-a (8090), gateway-b (8091), logic (9001), redis, mysql
- 代码版本: 当前 workspace 主分支

## 2. 测试目标
- 验证本机 Docker Compose 多 Gateway 场景下的连接承载能力。
- 验证 Gateway -> Logic -> Redis/MySQL 基础链路和跨 Gateway 单聊链路可用性。
- 记录 HTTP/WebSocket 压测现象，为后续定位连接层、RPC 调用和中间件瓶颈提供依据。

## 2.1 测试口径
- 1w WebSocket 指标表示本机 10 个 Gateway 进程、每个进程 1000 个连接的模拟结果，不等同于生产集群容量。
- 30s 心跳测试主要验证连接保持和心跳收发，不代表每个连接都持续发送业务消息。
- HTTP QPS 来自本机 `hey` 压测，主要用于观察接口链路吞吐，不等同于 IM 消息投递 QPS。
- 当前报告已记录成功率和部分延迟，P95/P99、失败原因分类、Redis/Kafka/MySQL 瓶颈还需要后续更细化采集。

## 3. 调度结果
### 3.1 服务基础连通
- `GET /api/v1/history` 返回 200 (gateway-a / gateway-b 均可)
- 说明基本服务链路可用（gateway -> logic -> db）

### 3.2 HTTP 压测 (hey)
- `-c 100 -z 20s`
  - QPS 约 500
  - 99% 延迟 < 100ms
  - 200 成功率 100%

- `-c 300 -z 20s`
  - 99% 延迟 164ms
  - 200 成功 ~104683
  - 500 错误 424

- `-c 500 -z 20s`
  - 99% 延迟 355ms
  - 200 成功 ~112934
  - 500 错误 358

结论: 300 并发稳定，500 以上开始出现 `500` 失败，说明单机或单网关存在瓶颈。

### 3.3 WebSocket 压测
- 自定义 300 连接，持续 30s：`done success=300 failed=0`
- 数据表明 300 长连接稳定；此项目已具备 WebSocket 承载能力

### 3.4 多网关跨节点通信验证
- 网关 A(8090) 连接 user1，网关 B(8091) 连接 user2
- user1 发送消息到 user2，user2 收到 `"cross-gateway test"`
- 结论：跨网关路由与 Redis Pub/Sub 通信链路可用（跨网关可达）

### 3.5 资源消耗
- gateway-a: CPU 0.06%, MEM 59MB
- gateway-b: CPU 0.06%
- logic: CPU 0% , MEM 23MB
- redis/mysql 正常负载

## 4. 结论
- 单网关测试：LinkGo-IM 单个 gateway 在本机环境中 300 并发稳定，500 并发开始出现 500 错误。
- 三网关测试：gateway-a/b/c（8090/8091/8092）组合，等价压力：2400 并发（3×800）所有三节点均可 20s 内无 500 失败，且响应稳定在 90-110ms 左右。说明多网关扩展可线性提高吞吐。
- 十网关测试：gateway-a~j（8090-8099）组合，等价压力：10000 并发（10×1000）结果为 98.6% 成功率，说明本机多进程模拟下 Gateway 横向扩展链路可用；该结果需要结合机器配置、连接行为和失败原因一起解释。
- WebSocket 长连接测试（10k）：10k WebSocket 客户端连接（每个网关 1000，跨 10 个端口）执行 30s 心跳发/收，结果 `created=10000 success=10000 failed=0`，说明连接保持和心跳链路在当前测试条件下可达。
- 关键结论：多 Gateway 模式可以缓解单进程连接压力，跨网关 Redis Pub/Sub 路由链路验证可达；后续需要继续补充断连重连、ACK 丢失、消息补偿、Kafka 积压和 P95/P99 延迟指标。

## 4.1 已验证与未验证
- 已验证：本地多 Gateway 启动、基础 HTTP 链路、跨 Gateway 单聊可达、WebSocket 连接保持、心跳收发。
- 部分验证：多网关压力下的成功率和接口延迟，失败原因尚未完全分类。
- 未充分验证：弱网断连重连、ACK 丢失补偿、Gateway 节点下线恢复、Kafka 消费积压、Redis 热 key、MySQL 写入瓶颈、端到端消息 P95/P99。

## 5. 优化建议
1. 将 `clients` map 从 `sync.RWMutex` 升级为 `sync.Map`/sharded map，减少锁竞争
2. WebSocket 推送逻辑异步队列，避免单线程处理阻塞
3. gRPC 长连接池 + 批量 push 减少压力
4. 增加 `Prometheus` / `Grafana` 监控 p99 / goroutines / lock contention
5. Redis 增加 cluster + Redis 哨兵/主从以提高 pub/sub 并发吞吐

## 6. 操作方式
- 运行测试：`bash benchmark/run_bench.sh`
- 查看日志：`cat benchmark/logs/hey_300.log` / `cat benchmark/logs/ws_bench.log` / `cat benchmark/logs/docker_stats.log`
