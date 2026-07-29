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
| 单元测试 | `make test` | 当前自动化用例通过 | 生产流量和所有故障路径 |
| 服务构建 | `make build` | Gateway/Logic/Transfer 可编译 | 容器和依赖实际可启动 |
| 文档结构 | `make docs-check` | 主教材章节、链接和知识源一致 | 文档中的每个架构判断绝对正确 |
| 前端契约 | `make frontend-static-check` | 关键控件和接口字段存在 | 浏览器端到端交互全部正常 |
| Compose 渲染 | `make compose-config` | 完整 Compose YAML 有效 | 镜像可拉取、服务已健康 |
| K8s 静态检查 | `make k8s-check` | 关键清单和发布脚本满足项目约束 | 已部署到真实集群 |

## 3. 本次文档重组后的结果

2026-07-29 在本次待提交工作树执行：

```bash
make fmt-check
make test
make build
make docs-check
make frontend-static-check
make compose-config
make k8s-check
git diff --check
```

本地结果：以上检查全部通过，其中 K8s 校验确认 demo/production 两套清单可渲染、Logic 发现配置与 Secret 边界满足脚本约束。Compose 只完成配置渲染，本轮没有凭这一步声称所有容器已经启动。最终远程结果仍以本文件所在 commit 的 GitHub Actions 页面为准。

本轮没有执行需要真实运行栈的浏览器 E2E、故障注入和重新压测，这些项目保持“未验证”，不能沿用旧版本截图。

`make prometheus-check` 本地因 Docker Hub 拉取 `prom/prometheus:v2.55.1` 时连接被重置而未完成；已用本机 PyYAML 解析 `prometheus.yml` 和告警规则通过，但完整 promtool 语义检查仍交给本 commit 的 GitHub Actions，不能把网络失败记成通过。

## 4. 关键自动化用例位置

```text
internal/logic/handler_test.go          消息幂等、落库和权限
internal/logic/conversation_test.go     会话状态
internal/logic/redpacket_test.go        红包 SQL 事务流程
internal/server/*_test.go               连接、分片队列、ACK 和恢复
internal/delivery/*_test.go             Redis 投递状态
cmd/transfer/main_test.go               Kafka 消费与收件人幂等
internal/ai/*_test.go                   Provider、检索、审计和脱敏
scripts/validate_frontend.py            浏览器调试页静态契约
scripts/validate_docs.py                文档结构与旧路径拦截
scripts/validate_k8s.sh                 Kubernetes 清单约束
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
