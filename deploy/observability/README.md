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

当前面板聚焦 IM 核心链路：连接数、消息吞吐、ACK、Kafka 和限流。后续可继续补日志采集、链路追踪和容量基线。

部分 `CounterVec` 指标需要对应业务流量触发后才会出现在 Prometheus 中。例如 Kafka、ACK、限流指标通常要在发送群消息、收到 ACK 或触发限流后才有样本。

## 下一阶段需求：把监控变成可复现的排障闭环

这部分是实施与学习要求，**不是已完成的功能清单**。当前仓库已有 `/metrics`、Prometheus 抓取、Grafana 面板、三条 Prometheus 告警规则，以及含 `trace_id`、`message_id` 的部分结构化日志。尚无 Alertmanager 通知接收端、集中日志平台或完整分布式 Trace；现有面板也没有业务端到端 ACK 延迟。先复用已有设施，不重复造一套监控框架。

| 步骤 | 要解决的问题与具体改动 | 验收证据 |
| --- | --- | --- |
| 1. 建立可复现基线 | 在隔离 Docker 环境启动 IM 与观测栈，登录两个用户，发送、接收并 ACK 一条跨 Gateway 消息。核对三个服务的 `/metrics`、Prometheus Targets 和 Grafana 现有面板。 | 保存命令、版本 SHA、实际 Targets 状态、发送前后指标变化与 `ops_smoke_report.md`；配置解析通过不能冒充运行通过。 |
| 2. 定义业务 SLI | 在现有连接数、吞吐、队列、Kafka 错误指标之外，定义“服务端接受后收到客户端 ACK 的比例与耗时”、Outbox 待办量及最老任务年龄。耗时从服务端接受时间起算，不能直接信任客户端 `sent_at`；指标标签只放服务、节点、结果等低基数值，不放用户或消息 ID。 | 正常发送、ACK 丢失、离线和消息投递失败时，计数与延迟变化符合语义；面板说明分母、窗口及指标不覆盖的场景。 |
| 3. 让告警真正送达 | 保留现有 Prometheus 规则，在本地接入 Alertmanager 和仅监听本机的测试接收端；先验证 Gateway 不可用、队列拒绝及 Kafka 错误的 firing、resolved、分组与静默。生产通知渠道要另行配置。 | 故障注入后看到规则状态、通知内容和恢复通知；记录触发时间与误报/漏报。当前只有规则，不声称已经通知到人。 |
| 4. 能按一次消息查日志 | 统一 Gateway、Logic、Transfer 的日志字段：时间、服务、阶段、`trace_id`、`message_id`、结果和错误码；不得记录密码、JWT、消息正文。先用 `docker compose logs` 按 ID 串联一次消息及一次失败，确有跨实例检索需求时再接日志平台。 | 给定一条失败消息 ID，能找到进入 Gateway、Logic 入库、Transfer/投递和 ACK 的实际记录，并明确缺失的阶段。当前的 `trace_id` 关联日志不是 OpenTelemetry Trace。 |
| 5. 运行故障演练 | 分别模拟 Gateway 退出、Redis 不可用、Kafka 投递异常和慢客户端；按“告警 → 指标定位范围 → 日志定位阶段 → 修复/回滚 → 业务烟测”记录。 | 每例给出故障现象、受影响指标、相关日志、原因、恢复动作和消息是否补齐；不得把模拟数据写成生产事故。 |

**面试时要分清两种延迟。**当前 `linkgo_push_processing_latency_seconds` 衡量 Gateway 入站任务处理耗时，不是“用户 A 发出到用户 B 收到并 ACK”的端到端延迟。现有面板的连接数和消息吞吐是真实指标定义，但没有运行流量时不能拿面板截图上的空值说性能。Prometheus 的 `up` 只说明目标抓取是否成功；`/readyz` 检查依赖是否适合接流量，仍不能单独证明消息闭环成功。

现有指标可先用下面两条查询练习读图；第二条只代表**入站处理** P99：

```promql
sum(linkgo_ws_connections)
histogram_quantile(0.99, sum by (le) (rate(linkgo_push_processing_latency_seconds_bucket[5m])))
```

项目讲述顺序：先说当前确有 Go 指标、Prometheus、Grafana 和规则；再说一次消息正常与故障时分别看哪个指标、用哪些 ID 查日志；最后说通知接收端、端到端 SLI、集中日志和 Trace 哪些尚待实施。不要把“有告警规则”讲成“有人收到告警”，也不要把业务日志里的 `trace_id` 讲成已经建好分布式链路追踪。
