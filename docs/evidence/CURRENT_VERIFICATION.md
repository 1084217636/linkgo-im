# 当前验证记录

> 这是验证索引，不是学习教材。第一次学习请从 [`docs/handbook/README.md`](../handbook/README.md) 开始。结论只对运行命令时的当前 checkout 有效；GitHub 上以对应 commit 的 Actions 结果为最终远程证据。

## 1. 为什么需要这份记录

“代码里有测试”“仓库里有 K8s 文件”都不等于已经验证。每个证据必须同时写清楚：

```text
执行了什么
结果是什么
它能证明什么
它不能证明什么
```

## 2. 当前必跑检查

| 检查 | 命令 | 能证明 | 不能证明 |
|---|---|---|---|
| Go 格式 | `make fmt-check` | 已提交 Go 文件符合 gofmt | 业务逻辑正确 |
| Go 静态检查 | `make vet` | vet 能识别的当前代码问题未出现 | 所有并发和业务缺陷 |
| 单元测试 | `make test` | 当前自动化用例通过 | 生产流量和所有故障路径 |
| 并发竞态 | `make test-race` | 被测试路径未被 race detector 发现数据竞争 | 未执行路径和跨进程一致性 |
| 服务构建 | `make build` | Gateway/Logic/Transfer 可编译 | 容器和依赖实际可启动 |
| 文档结构 | `make docs-check` | 主教材章节、链接和知识源一致 | 文档中的每个架构判断绝对正确 |
| 前端契约 | `make frontend-static-check` | 关键控件和接口字段存在 | 浏览器端到端交互全部正常 |
| Compose 渲染 | `make compose-config` | 完整 Compose YAML 有效 | 镜像可拉取、服务已健康 |
| Prometheus | `make prometheus-check` | Prometheus 配置与告警规则可被 promtool 解析 | 告警一定会在真实故障中触发 |
| Docker 镜像 | `make docker-build` | 三个服务可进入同一运行镜像 | 服务依赖已联调和持续稳定 |
| K8s 静态检查 | `make k8s-check` | 关键清单和发布脚本满足项目约束 | 已部署到真实集群 |
| 故障处理契约 | `make fault-check` | 故障矩阵脚本、依赖探针、替换钩子、观测目标和 runbook 保持一致 | 已停止真实容器并完成全部恢复演练 |

## 3. 当前待发布版本的本地结果

2026-08-12 在本次待提交工作树执行：

```bash
make fmt-check
make vet
make test
make build
make docs-check
make fault-check
make frontend-static-check
make compose-config
make compose-cn-config
make compose-light-config
make compose-light-cn-config
make observability-config
make observability-cn-config
make k8s-check
PROMETHEUS_IMAGE=docker.m.daocloud.io/prom/prometheus:v2.55.1 make prometheus-check
docker build \
  --build-arg GO_BUILDER_IMAGE=docker.m.daocloud.io/library/golang:1.25-alpine \
  --build-arg RUNTIME_IMAGE=docker.m.daocloud.io/library/alpine:3.22 \
  -t linkgo-im:final .
git diff --check
```

本地结果：以上检查全部通过。K8s 校验确认 demo/production 两套清单可渲染，并检查 Logic 发现、Secret 边界、非 root 容器和不可变镜像标签；Promtool 确认一份配置文件和三条告警规则有效；Docker 镜像构建成功，镜像默认用户实测为 `10001:10001`。Compose 只完成配置渲染，本轮没有凭这一步声称所有容器已经启动。最终远程结果仍以本文件所在 commit 的 GitHub Actions 页面为准。

2026-08-12 的该轮检查没有执行浏览器 E2E 和重新压测；当时已在完整 Compose 上执行：

```bash
FAULT_INJECTION_CONFIRM=1 \
COMPOSE_FILE_PATH=docker-compose.yml \
COMPOSE_ENV_FILE=.env.docker-cn \
WAIT_SECONDS=45 SMOKE_TIMEOUT=60 \
ARTIFACT_DIR=artifacts/fault-injection-final \
bash scripts/fault_injection.sh
```

结果为通过：Redis、Logic、Kafka、Transfer、MySQL 依赖故障，Gateway-a 替换为 Gateway-b，所有场景均观察到 readyz 摘流量、恢复后 readyz 成功，并重新完成单聊、离线回放、ACK、AI、群聊和指标 smoke。报告位于本地 `artifacts/fault-injection-final/fault_injection_report.md`；`artifacts/` 被 Git 忽略，不能把未提交的本地报告当成远程证据。脚本仍不证明 Redis Cluster、MySQL 主从或 Kafka 多副本 HA。

另外在同一完整栈上直接停止 Kafka 验证了 Gateway、Logic、Transfer 的 `/readyz` 均返回 HTTP 503，恢复 Kafka 后三者均恢复 HTTP 200。这一条额外验证来自 Gateway 调用 Logic gRPC Health Check 的依赖感知实现。

主机 WSL 没有 gcc，且 sudo 安装需要交互密码；因此没有把主机直接执行的 `make test-race` 记为通过。已使用带 GCC 的 Go Docker 工具链完成等价验证：

```bash
docker run --rm --network host \
  -v "$PWD:/src" \
  -v /home/xiaobin/go/pkg/mod:/go/pkg/mod:ro \
  -v /home/xiaobin/.cache/go-build:/root/.cache/go-build \
  -w /src docker.m.daocloud.io/library/golang:1.25-alpine \
  sh -lc 'export PATH=/usr/local/go/bin:$PATH; apk add --no-cache gcc musl-dev; CGO_ENABLED=1 go test -race -count=1 ./...'
```

该命令已通过全部 Go 包的 race 检测。主机永久安装 gcc 仍需用户在 WSL 终端执行带 sudo 的安装命令；GitHub Actions 也会继续执行 `make test-race`。

### 3.1 2026-08-16 钱包红包与当前协议压测补充

在本次待提交工作树上完成了最小钱包红包闭环联调：模拟微信充值 1000 分（相同 request_id 重试不重复入账），发 300 分红包并由另一用户领取（重复领取结果幂等），再发 200 分短期红包并触发过期退款（重复退款结果幂等）。最终发送者相对充值后净支出 300 分、接收者入账 300 分，流水包含 `mock_recharge`、`red_packet_send`、`red_packet_claim`、`red_packet_refund`。这不包含真实微信/支付宝签名回调、提现、渠道对账和风控。

同一工作树在完整十 Gateway Compose 上重新运行当前 JWT + Origin + Protobuf 协议压测：

- 10000/10000 WebSocket 连接成功，59983/59983 次心跳成功；心跳 P95 4.83ms，P99 8.25ms。
- 100 对连接按总计 500 msg/s 发送 2000 条，2000 条到达并 ACK；P95 5.29ms，P99 6.63ms。
- 不限速瞬时发送 2000 条时，到达并 ACK 1934 条，96.7%；其余请求触发单用户 WebSocket 限流，不能描述为“可靠链路随机丢失”。
- 200 个用户并发领取 100 份、总额 10000 分的红包：100 个成功、100 个已抢完、异常 0，总领取 10000 分，剩余金额和份数均为 0；该基准直连服务层和 MariaDB，不覆盖 HTTP/JWT。

压测同时发现并修复了 Origin 缺失、压测器错误重读 WebSocket、Etcd 注册/订阅 Key 前导斜杠不一致、benchmark 用户缺少好友关系，以及 Gateway 未等待 Logic ready 的问题。完整机器配置、原始结果和边界见 [`benchmark/bench_report_20260816.md`](../../benchmark/bench_report_20260816.md)。基线 SHA 是 `07fe1df1a36eeff3efa47c909336eeaf5c4e5f70`，以上改动尚未提交，因此正式简历引用前仍应在提交后的 SHA 再跑一次。

## 4. 关键自动化用例位置

```text
internal/logic/handler_test.go          消息幂等、落库和权限
internal/logic/conversation_test.go     会话状态
internal/logic/redpacket_test.go        红包 SQL 事务流程
internal/server/*_test.go               连接、分片队列、ACK 和恢复
internal/delivery/redis_test.go         pending 先写、离线标记、无订阅者条件清路由
cmd/transfer/main_test.go               Kafka 消费与收件人幂等
internal/ai/*_test.go                   Provider、检索、审计和脱敏
scripts/validate_frontend.py            浏览器调试页静态契约
scripts/validate_docs.py                文档结构与旧路径拦截
scripts/validate_k8s.sh                 Kubernetes 清单约束
scripts/validate_fault_handling.py      故障演练脚本、依赖探针和 runbook 静态契约
```

## 5. 当前最重要的未覆盖边界

- 单元测试不能替代真实 Redis/MySQL/Kafka 的长时间稳定性测试。
- 钱包红包用例主要验证充值、扣款、领取入账、退款的 SQL 事务顺序和约束，不是真实支付系统或生产级并发压测。
- K8s render/check 不等于真实集群 rollout、扩缩和故障恢复已经完成。
- 当前没有浏览器 E2E 测试证明断线重连的全部交互。
- 2026-08-16 已在待提交工作树重跑当前协议 benchmark；具体数字仍只代表报告中的机器、参数和代码状态，提交后应再绑定新 commit SHA。
- production overlay 没有 Ingress/TLS、前端 Deployment 或真实中间件高可用集群验证。

## 6. 面试证据口径

可以说：

> 我把格式、单测、构建、前端契约、文档结构、Compose 配置、Prometheus 配置和 K8s 清单渲染放进 GitHub Actions；每项都能定位到命令和失败 step。

不能只因为 CI 绿色就说：

> 系统已经达到生产级高可用，所有消息绝不丢，Kubernetes 已在线上自动发布。

CI 证明的是被执行检查的结果，不会自动扩大证明范围。
