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

本轮没有执行需要真实运行栈的浏览器 E2E 和重新压测。2026-08-12 已在完整 Compose 上执行：

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

本机没有 C 编译器且 `CGO_ENABLED=0`，所以没有在本地把 `go test -race` 记为通过。GitHub Actions 的 Ubuntu runner 会单独执行 `make test-race`；远程检查结束前不能声称本 commit 的 race 检测已通过。

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
- 红包用例主要验证 SQL 顺序和约束，不是资金系统或生产级并发压测。
- K8s render/check 不等于真实集群 rollout、扩缩和故障恢复已经完成。
- 当前没有浏览器 E2E 测试证明断线重连的全部交互。
- 历史 benchmark 只代表当时机器和代码；没有在当前 commit 重跑就不能引用具体 QPS、连接数或 P99。
- production overlay 没有 Ingress/TLS、前端 Deployment 或真实中间件高可用集群验证。

## 6. 面试证据口径

可以说：

> 我把格式、单测、构建、前端契约、文档结构、Compose 配置、Prometheus 配置和 K8s 清单渲染放进 GitHub Actions；每项都能定位到命令和失败 step。

不能只因为 CI 绿色就说：

> 系统已经达到生产级高可用，所有消息绝不丢，Kubernetes 已在线上自动发布。

CI 证明的是被执行检查的结果，不会自动扩大证明范围。
