# Observability

这组配置用于把本地 Docker 环境接入 Prometheus 和 Grafana，方便在功能测试、压测和面试展示时说明系统是否真的可观测。

## 启动

国内镜像环境优先使用：

```bash
make observability-cn-up
```

如果 Docker 可以直接访问默认镜像：

```bash
make observability-up
```

启动后访问：

- Prometheus: http://127.0.0.1:9090
- Grafana: http://127.0.0.1:3000
- Grafana 账号: `admin`
- Grafana 密码: `linkgo`

Grafana 会自动加载 `LinkGo IM / LinkGo IM Overview` 面板。

## 本地闭环

启动完整环境和观测栈后，执行：

```bash
make ops-smoke
```

脚本会检查 Gateway、Transfer、Prometheus、Grafana，并把结果写入 `artifacts/ops_smoke_report.md`。

建议压测时同步打开 Grafana 面板，观察：

- `linkgo_ws_connections`
- `linkgo_inbound_messages_total`
- `linkgo_outbound_messages_total`
- `linkgo_ack_operations_total`
- `linkgo_kafka_operations_total`
- `linkgo_rate_limit_hits_total`
- `linkgo_red_packet_operations_total`

## 告警闭环

`rules/linkgo-alerts.yml` 覆盖服务不可用、推送队列背压和 Kafka 处理/提交失败。可用以下命令检查规则语法：

```bash
make prometheus-check
```

告警阈值是本地演示基线，上线前应根据压测报告调整。当前没有引入 Kafka exporter，因此不声称已经监控 consumer group lag；现阶段用 Fetch/Handle/Commit 失败率证明消费链路异常，Kafka lag 需要在部署 exporter 后另行补充。

## 说明

当前面板聚焦 IM 核心链路：连接数、消息吞吐、ACK、Kafka 和限流。社区环境还通过 Alloy 收集 Docker 日志写入 Loki，并将 Prometheus 告警交给 Alertmanager 的本地审计接收端。尚无 OpenTelemetry Span 全链路追踪或生产容量基线。

部分 `CounterVec` 指标需要对应业务流量触发后才会出现在 Prometheus 中。例如 Kafka、ACK、限流指标通常要在发送群消息、收到 ACK 或触发限流后才有样本。

## 排障闭环与待完善项

下面的表格记录历史改造思路，其中本地 Alertmanager 审计接收端和 Alloy/Loki 集中日志已经落地；是否运行通过应看 `scripts/verify_community_observability.py` 与 `docs/evidence/resume-verification-20261102.md`。目前仍没有完整分布式 Trace、业务端到端 ACK 延迟指标或生产通知渠道。不要把待完善项说成已实现。

| 步骤 | 要解决的问题与具体改动 | 验收证据 |
| --- | --- | --- |
| 1. 建立可复现基线 | 在隔离 Docker 环境启动 IM 与观测栈，登录两个用户，发送、接收并 ACK 一条跨 Gateway 消息。核对三个服务的 `/metrics`、Prometheus Targets 和 Grafana 现有面板。 | 保存命令、版本 SHA、实际 Targets 状态、发送前后指标变化与 `ops_smoke_report.md`；配置解析通过不能冒充运行通过。 |
| 2. 定义业务 SLI | 在现有连接数、吞吐、队列、Kafka 错误指标之外，定义“服务端接受后收到客户端 ACK 的比例与耗时”、Outbox 待办量及最老任务年龄。耗时从服务端接受时间起算，不能直接信任客户端 `sent_at`；指标标签只放服务、节点、结果等低基数值，不放用户或消息 ID。 | 正常发送、ACK 丢失、离线和消息投递失败时，计数与延迟变化符合语义；面板说明分母、窗口及指标不覆盖的场景。 |
| 3. 让告警真正送达 | 社区环境已接入 Alertmanager 本地审计入口；继续补充生产通知渠道、分组与静默策略验证。 | `verify_community_observability.py` 已验证暂停 Transfer 后 firing、恢复后 resolved；生产通知渠道仍待建设。 |
| 4. 能按一次消息查日志 | 当前已有部分跨服务 `trace_id`、`message_id` 结构化日志，Alloy 收集 Docker 日志并写 Loki；继续统一更多阶段的字段与脱敏规则。 | 证据文件记录过一条消息在 Logic、Transfer、Gateway 日志中的关联；这不等于 OpenTelemetry Trace。 |
| 5. 运行故障演练 | 分别模拟 Gateway 退出、Redis 不可用、Kafka 投递异常和慢客户端；按“告警 → 指标定位范围 → 日志定位阶段 → 修复/回滚 → 业务烟测”记录。 | 每例给出故障现象、受影响指标、相关日志、原因、恢复动作和消息是否补齐；不得把模拟数据写成生产事故。 |

**面试时要分清两种延迟。**当前 `linkgo_push_processing_latency_seconds` 衡量 Gateway 入站任务处理耗时，不是“用户 A 发出到用户 B 收到并 ACK”的端到端延迟。现有面板的连接数和消息吞吐是真实指标定义，但没有运行流量时不能拿面板截图上的空值说性能。Prometheus 的 `up` 只说明目标抓取是否成功；`/readyz` 检查依赖是否适合接流量，仍不能单独证明消息闭环成功。

现有指标可先用下面两条查询练习读图；第二条只代表**入站处理** P99：

```promql
sum(linkgo_ws_connections)
histogram_quantile(0.99, sum by (le) (rate(linkgo_push_processing_latency_seconds_bucket[5m])))
```

项目讲述顺序：先说当前确有 Go 指标、Prometheus、Grafana、Alertmanager 本地审计入口和 Alloy/Loki；再说一次消息正常与故障时分别看哪个指标、用哪些 ID 查日志；最后说生产通知渠道、端到端 SLI 和完整 Trace 哪些尚待实施。不要把本地审计告警讲成真实值班通知，也不要把业务日志里的 `trace_id` 讲成已经建好分布式链路追踪。
