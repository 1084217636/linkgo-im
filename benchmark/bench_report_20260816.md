# LinkGo-IM 当前协议压测记录（2026-08-16）

## 结论

本机完整 Docker Compose 环境中启动 10 个 Gateway、1 个 Logic、Redis、MySQL、Kafka、Transfer 和 Etcd。本轮验证得到：

- 10000 个 WebSocket 连接全部建立成功；30 秒内 59983 次 Protobuf 心跳全部收到响应。
- 100 对跨 Gateway 单聊连接，在总计 500 msg/s 的受控速率下发送 2000 条，2000 条到达并 ACK，端到端 P95 为 5.29ms，P99 为 6.63ms。
- 同样 2000 条消息瞬时突发时到达 1934 条，成功率 96.7%；66 条被当前“每用户 5 次/秒、突发容量 10”的 WebSocket 限流保护挡住，不应写成消息可靠性随机丢失。
- 一个 100 份、总额 10000 分的红包由 200 个不同用户并发领取，恰好 100 人成功、100 人得到已抢完、总入账 10000 分且剩余金额/份数均为 0。

这些结果证明本机、当前参数和短时压力下的连接保持、跨 Gateway 单聊链路及红包核心事务守恒；不能外推为生产容量，也没有覆盖红包 HTTP 层压力、Kafka 群聊积压、断网重连和多副本中间件故障。

## 运行环境与代码状态

```text
时间：2026-08-16T01:28:58+08:00
OS：Linux 6.6.87.2-microsoft-standard-WSL2 x86_64
CPU：Intel Core i5-14600KF，20 vCPU
内存：15 GiB
Go：go1.26.1 linux/amd64
Docker Client/Server：29.3.1 / 29.3.1
基线 commit：07fe1df1a36eeff3efa47c909336eeaf5c4e5f70
状态：基线 commit 之上的待提交工作树；钱包与本轮压测修复尚未形成新 commit
```

因此，简历正式引用前应提交代码，并在该 commit 再执行一次相同命令，把新 SHA 写回本报告。

## 受控速率结果

命令等价于：

```bash
go run benchmark/current_protocol_bench.go \
  -gateways=10 \
  -per-gateway=1000 \
  -heartbeat-duration=30s \
  -pairs=100 \
  -messages=2000 \
  -send-rate=500
```

结果：

```text
gateway_ready ready=10/10
heartbeat_result total=10000 success_conn=10000 failed_conn=0 heartbeat_sent=59983 heartbeat_ok=59983 heartbeat_failed=0 latency_count=59983 avg=2.034603ms p50=1.608771ms p95=4.834269ms p99=8.246157ms elapsed=35.039887601s
single_chat_result pairs=100 sent=2000 received=2000 acked=2000 receive_errors=0 timeout=0 ack_timeout_rate=0.0000 latency_count=2000 avg=3.837965ms p50=3.676786ms p95=5.286167ms p99=6.630623ms elapsed=4.105822139s
server_metrics ack_success=2000 ack_retry_success=0 ack_retry_exhausted=0 kafka_retry_write=0 kafka_dlq_write=0
```

心跳与消息结果来自两次相同容器配置下的复验：组合测试记录心跳；随后重启 Gateway 清空进程指标，按 500 msg/s 单独复验消息。因此上面的两行可以分别引用，但不是同一个进程生命周期内连续产生的单份输出。

## 瞬时突发结果

`send-rate=0` 时，2000 条消息由 100 个发送者尽快写入，每个发送者约瞬时发送 20 条：

```text
single_chat_result pairs=100 sent=2000 received=1934 acked=1934 receive_errors=0 timeout=66 ack_timeout_rate=0.0330 latency_count=1934 avg=529.75425ms p50=537.902662ms p95=933.895768ms p99=964.540617ms elapsed=45.079004162s
server_metrics ack_success=1934 ack_retry_success=0 ack_retry_exhausted=0 kafka_retry_write=0 kafka_dlq_write=0
```

客户端 `WriteMessage` 成功只代表数据写入 WebSocket，不代表业务接受。当前 Gateway 的单用户限流是 5 次/秒、容量 10；压测器没有消费发送端的背压/限流反馈，因此 `sent=2000` 不能解释为服务端接受了 2000 条。这个口径后续可以继续改进为按响应类型统计 `accepted`、`rate_limited` 和真实异常。

## 红包并发领取结果

该测试通过 Go 服务层直接连接同一个 Compose MariaDB，绕过 HTTP/JWT/群成员校验，只验证红包行锁、领取唯一性、钱包入账和金额守恒。客户端连接池限制为 100，不超过当前 MariaDB 默认连接容量。

```text
redpacket_concurrency_result claimers=200 count=100 total=10000 success=100 finished=100 failed=0 claimed_amount=10000 remaining_count=0 remaining_amount=0 invariant_ok=true elapsed=219.108469ms
```

首次把连接池错误设置为 240 时出现 49 个 `Too many connections`，但仍未超卖；调整为 100 后无数据库异常。这个结果不能替代红包 HTTP 接口、鉴权、群成员权限以及长时间热点锁等待压测。

## 本轮压测发现并修复的问题

1. 压测器没有发送 `Origin`，在当前 WebSocket 白名单下全部握手 403；已增加 `-origin` 参数并默认使用本地允许来源。
2. 压测器在 Read Deadline 失败后继续读取同一个 gorilla/websocket 连接，触发 panic；已改为结束时关闭连接来唤醒读协程。
3. Etcd Key 使用 `/services/logic`。go-zero resolver 会把 URL 路径归一化为 `services/logic`，导致注册与订阅不一致；已统一为不带前导斜杠的 `services/logic`。
4. 旧压测用户没有好友关系，被当前单聊权限规则拒绝；`run_bench_10node.sh` 现在会幂等准备 benchmark 专用关系。
5. 10 节点 Compose 原先只等待 Logic 容器启动；现在等待 Logic `/readyz` 健康后才启动 Gateway。

## 可复现入口

```bash
bash benchmark/run_bench_10node.sh
```

脚本默认使用 `.env.docker-cn`（若存在）、准备好友数据、重启 Gateway 清空进程指标，然后运行当前协议测试。测试结束后容器仍保持运行，便于查看日志和 Prometheus 指标。
