# 12 故障、性能与演进

## 五个必背故障

### Redis 宕机

Gateway/Transfer readiness 失败，停止接新流量；MySQL 历史仍在，但在线路由和 pending 受影响。恢复后回源/重连补偿，不能承诺未持久化过程状态绝不丢。

### Logic 宕机

Logic 实例租约失效后从 Etcd 服务发现列表移除；正在执行的 RPC 可能失败，Gateway/客户端复用 client_msg_id 重试，后续由 p2c_ewma 选择其他健康 Logic。Gateway 自己的 readiness 也会因依赖不可用而停止接收新流量。

### Transfer 崩溃

未提交 Kafka 位点的任务会重投；已成功成员由 done 幂等跳过，processing lease 过期后可接管。

### Kafka 重复消费

按 message+recipient 幂等，业务设计 at-least-once，不依赖“只消费一次”。

### 客户端不 ACK

pending 保留，Gateway 有限重试；超过次数记录 exhausted，重连仍可补偿，避免无限重试拖垮系统。

## 其他故障

- MySQL 超时：事务回滚，连接池/慢 SQL 指标定位；幂等重试。
- 队列满：SERVER_BUSY + client_msg_id，指数退避+jitter。
- Redis 缓存同步失败：Outbox pending，API 202，后台重放。
- Gateway 重启：Socket 断开，客户端重连，旧 route 清理，pending/timeline 回放。

## 性能指标

吞吐 QPS/消息每秒；延迟平均值与 P50/P95/P99；错误率；并发连接；CPU/内存；goroutine；GC；队列深度；数据库连接池；Kafka lag（需 exporter）。

只报“1 万连接”不够，要说明是否有真实消息负载、机器配置、持续时间、错误率和延迟。

## Go 排查工具

- `go test -race`：数据竞争，需要 C 编译器；本机缺编译器时不能说通过。
- pprof：CPU、heap、goroutine、mutex/block profile。
- benchmark：稳定比较代码路径，不等于端到端压测。
- Prometheus：运行趋势和告警。

## 数据库优化顺序

慢 SQL 日志 -> EXPLAIN -> 查询条件/索引 -> 扫描行数/回表/排序 -> 分页与批量 -> 连接池 -> 必要时缓存或拆分。不要一开始就分库分表。

## 容量演进

本机基线只用于发现明显问题；面试容量结论必须以多实例、跨 Gateway 的消息负载测试为准。Gateway 看连接/推送队列，Logic 看数据库/Redis，Transfer 看分区和消费速率。实例数不是越多越好，每个实例都会增加数据库连接和协调成本。

## 当前未做

多机房、跨地域容灾、Service Mesh、完整 Kafka lag exporter、生产 Loki/tracing、商业级前端、真实支付、10 万真实负载连接。面试主动说边界比夸大更可信。

## 故障回答模板

> 先说明影响面，再说明系统如何检测，然后讲恢复和幂等，最后讲仍有的边界与监控证据。

## 闭卷题

1. Redis/Logic/Transfer 分别宕机会怎样？2. Kafka 重复消费如何处理？3. 不 ACK 怎么办？4. 队列满怎么背压？5. P95/P99 是什么？6. race/pprof 分别做什么？7. SQL 慢如何定位？8. 为什么不直接分库分表？9. 如何设计压测报告？10. 当前项目最重要边界？
