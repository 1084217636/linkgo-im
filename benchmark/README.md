# Benchmark

`benchmark/` 用于保存压测脚本、运行日志和压测报告，支撑“项目是否真的能抗并发”这一点的证明材料。

## 文件说明

- `run_bench.sh`：基础多节点压测脚本。
- `run_bench_10node.sh`：十网关并发压测脚本。
- `current_protocol_bench.go`：当前 JWT + Protobuf 协议兼容的压测工具，可测 10 Gateway WebSocket 心跳、单聊端到端延迟、ACK 统计和服务端指标。
- `local_core_bench.go`：绕开 Docker 的本地核心链路压测工具，使用本地 Redis + 内存 LogicHandler + 轻量 Gateway，覆盖 JWT、WebSocket、Protobuf 心跳、Redis route/PubSub、单聊投递与 ACK。
- `bench_report.md`：压测结果总结。
- `logs/`：执行压测后生成的日志。

## 使用场景

- 验证单网关 HTTP 压力和 WebSocket 长连接稳定性。
- 验证多网关横向扩容后的吞吐提升。
- 收集 `docker stats` 作为 CPU、内存占用的辅助证明。
- 配合 Prometheus/Grafana 观察连接数、消息吞吐、ACK、Kafka 和限流指标。
- 针对红包这类热点资源，额外观察并发抢时是否出现超卖、重复领取或锁等待异常。

## 观测闭环

压测前先启动完整环境和观测栈：

```bash
make observability-cn-up
```

执行压测或功能测试后跑一次 smoke 报告：

```bash
make ops-smoke
```

报告会写入 `artifacts/ops_smoke_report.md`，Grafana 面板地址为 `http://127.0.0.1:3000`。

## 注意

旧版 shell 脚本仍保留用于参考，但它们使用的是早期 `?user_id=` 与文本 `PING` 口径；当前实现已经切换到 JWT 鉴权和 Protobuf 心跳，正式对外展示前请优先使用 `current_protocol_bench.go` 重新跑一遍。

示例：

```bash
docker compose -f docker-compose.10node.yml up --build -d
go run benchmark/current_protocol_bench.go \
  -gateways=10 \
  -per-gateway=1000 \
  -heartbeat-duration=30s \
  -pairs=100 \
  -messages=1000
```
