# 11 可观测性、指标、日志与告警

## 三根支柱

- Metrics：数字趋势，适合告警和容量观察。
- Logs：一次事件的详细上下文。
- Traces：请求跨服务的完整路径。当前项目以指标和结构化日志为主，未完成完整分布式追踪。

## Prometheus/Grafana

服务 `/metrics` 暴露指标，Prometheus 定时抓取和存储时间序列，PromQL 查询，Grafana 展示。告警规则由 Prometheus 计算；生产通常再接 Alertmanager 通知。

## 核心指标

- WS 当前连接数。
- 入站/出站消息量与结果。
- push queue depth、queue_full、处理延迟。
- ACK success/retry/exhausted。
- Kafka fetch/handle/retry/DLQ/commit 结果。
- GameOps operation、延迟、缓存同步、道具条目。
- AI 请求、provider 延迟、fallback/attempt。

标签必须低基数：operation/result 可以；user_id、message_id 不应作为 label，否则时间序列爆炸。

## 四个黄金信号

延迟、流量、错误、饱和度。面试定位顺序：先看错误率和 P95/P99，再看队列、连接池、CPU/内存和下游指标。

## 当前告警

服务 down、push queue 持续拒绝、Kafka 操作失败、活动缓存同步失败、运营操作失败和高延迟。当前没有 Kafka exporter，不能声称已有 consumer lag 指标。

## 结构化日志

重要字段：timestamp、level、service、trace_id、user_id（必要时脱敏）、operation、resource、message_id、result、duration、error。日志用字段而不是拼接散文，便于过滤聚合。

日志不能记录：密码、完整 JWT、AI Key、敏感聊天全文。审计日志和调试日志用途不同，高风险管理操作要写专门审计表。

## 故障定位示例

群聊延迟：先看 Kafka handle/commit error 与 Transfer 指标，再看 retry/DLQ，再用 trace/message_id 查日志，最后检查 Redis/DB 和消费者实例。不能一上来猜代码。

## 边界

仓库提供 Prometheus/Grafana 和告警规则；Loki 日志集中查询、Alertmanager 通知和完整 tracing 仍属于可演进项。

## 闭卷题

1. metrics/logs/traces 区别？2. Prometheus 和 Grafana 分工？3. 什么是 P95？4. 为什么不能用 user_id 标签？5. 四个黄金信号？6. 群聊变慢怎么查？7. 告警和日志区别？8. 当前监控缺什么？
