# Benchmark

`benchmark/` 用于保存压测脚本、运行日志和压测报告，支撑“项目是否真的能抗并发”这一点的证明材料。

## 文件说明

- `run_bench.sh`：基础多节点压测脚本。
- `run_bench_10node.sh`：启动完整十网关环境、准备专用好友关系并运行当前协议压测。
- `current_protocol_bench.go`：当前 JWT + Protobuf 协议兼容的压测工具，可测 10 Gateway WebSocket 心跳、单聊端到端延迟、ACK 统计和服务端指标。
- `redpacket_concurrency_bench.go`：直接连接测试 MySQL，并发验证红包行锁、唯一领取和钱包入账守恒。
- `local_core_bench.go`：绕开 Docker 的本地核心链路压测工具，使用本地 Redis + 内存 LogicHandler + 轻量 Gateway，覆盖 JWT、WebSocket、Protobuf 心跳、Redis route/PubSub、单聊投递与 ACK。
- `bench_report_20260816.md`：当前完整容器栈的可复现压测结果。
- `bench_report.md`：旧版本历史压测记录，不能当作当前容量结论。
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

`run_bench.sh` 仍是早期 `?user_id=` 与文本 `PING` 口径，只用于参考。十节点验证请运行当前脚本；它会使用 JWT、Origin 白名单、Protobuf 心跳和好友权限规则。

示例：

```bash
bash benchmark/run_bench_10node.sh
```

默认消息速率是总计 `500 msg/s`，对应 100 个发送者平均每人 `5 msg/s`，与当前 WebSocket 单用户限流一致。若要观察突发限流退化：

```bash
SEND_RATE=0 bash benchmark/run_bench_10node.sh
```
