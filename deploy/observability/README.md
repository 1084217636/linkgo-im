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

`rules/linkgo-alerts.yml` 覆盖服务不可用、推送队列背压、Kafka 处理/提交失败、运营操作失败、活动缓存同步失败和运营接口高延迟。可用以下命令检查规则语法：

```bash
make prometheus-check
```

告警阈值是本地演示基线，上线前应根据压测报告调整。当前没有引入 Kafka exporter，因此不声称已经监控 consumer group lag；现阶段用 Fetch/Handle/Commit 失败率证明消费链路异常，Kafka lag 需要在部署 exporter 后另行补充。

## 说明

当前面板聚焦 IM 核心链路：连接数、消息吞吐、ACK、Kafka 和限流。后续可继续补日志采集、链路追踪和容量基线。

部分 `CounterVec` 指标需要对应业务流量触发后才会出现在 Prometheus 中。例如 Kafka、ACK、限流指标通常要在发送群消息、收到 ACK 或触发限流后才有样本。
