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
| 多 Logic 拓扑 | `make cluster-smoke` | 两个 Logic 注册到 Etcd，两个 Logic 与三个 Gateway 均 ready | 请求均匀分配、自动扩缩容和生产 HA |

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

### 3.2 2026-09-03 多 Logic 与持久化客户端补充

默认完整 Compose 从 1 个 Logic 调整为 2 个 Logic，并移除 Gateway 的固定 `LOGIC_ADDR`，让三台 Gateway 通过 Etcd 的 `services/logic` 前缀和 go-zero `p2c_ewma` 选择 Logic。为避免与同时运行的知忆 AI 环境端口冲突，本轮仅修改宿主机映射端口，容器网络端口保持不变。

执行 `scripts/verify_cluster_topology.sh` 后，Etcd 返回两个带 lease 的 Logic 注册键，两台 Logic 和三台 Gateway 均通过 readiness；随后执行 `scripts/demo_core_im.sh`，在线单聊与 ACK 清理、离线 pending 回放、Kafka/Transfer 群聊、MySQL 消息落库和 Prometheus 指标全部通过。两台 Logic 日志均出现 `/api.Logic/PushMessage`，说明本轮业务流量实际经过两个实例，而非只完成静态配置。

浏览器调试客户端新增 `client-store.js`：IndexedDB 按用户保存会话、消息、发送状态和 `last_seq/acked_seq`，登录后先恢复本地数据，再与服务端历史页做 ID 对账；历史接口失败时允许显示本地副本。JWT 仍只存在页面内存。`make frontend-static-check` 与 JavaScript 语法检查已通过，但本轮没有把浏览器 E2E 描述为已完成。

### 3.3 2026-09-03 投递 Outbox 与持续负载补充

消息持久化事务新增外部投递 Outbox。单元测试验证消息、会话摘要事件和投递事件原子提交，以及成功完成、失败退避和父上下文取消后的状态恢复。完整 Compose 中停掉 Kafka 后，群消息投递事件保持 pending 并记录连接错误；Kafka 恢复后第 6 次尝试自动完成，无需客户端重发。随后核心协议演示再次通过在线单聊 ACK、离线回放、Kafka 群聊和 MySQL 落库。

新版压测器在 10 Gateway 环境建立 10000 条 WebSocket，119964 次心跳全部成功，心跳 P95 5.368ms、P99 8.491ms；100 对连接按总计 500 msg/s 持续 60 秒发送 30000 条跨节点单聊，服务端接受、接收和 ACK 均为 30000，接收延迟 P95 4.324ms、P99 5.081ms。两台 Logic 分别处理 14876 和 15124 条请求；停掉一台 Logic 后重新发起 5000 条测试也全部闭环。该故障用例是在节点停止后开始发送，不应描述成飞行中 RPC 零影响切换。

### 3.4 2026-10-05 Web 客户端历史补偿口径核对

前端收到任何已知会话的实时消息时，先把正文写入 IndexedDB，再发送业务 ACK；不再仅把当前显示会话的正文落本地。新增独立 `sync_progress` 存储区：登录和 WebSocket 重连后，对页面已知会话逐个调用现有 MySQL 历史接口，沿 `before_seq` 向前翻页直到与上次完成的本地进度重叠，最后在同一 IndexedDB 事务中保存消息和新进度。首次本地同步只建立最新 100 条基线，旧历史仍需手动翻页。页面提供“补齐历史”按钮以便复核与重试。

运行 `node --test scripts/history-sync.test.cjs`，4 个用例通过：初次基线、跨两页补齐 80→250、异常游标阻止错误推进，以及旧 HTTP 历史响应与较新的 WebSocket 消息合并后仍按 seq 显示。网页实时消息也按 seq 插入当前会话，避免回放到达顺序直接决定显示顺序。用真实 Gateway 登录 userB，对 A↔B 历史接口把页大小设为 2、上次进度设为 1，补偿算法实际读取 6 页共 11 条，返回最新 seq=11、新于进度的 10 条。`python3 scripts/validate_frontend.py` 与三份 JavaScript 语法检查通过；`go test ./internal/server ./internal/logic ./cmd/gateway/internal/logic` 通过。当前未进行真实浏览器自动化 E2E，不能把 IndexedDB 跨刷新及页面渲染说成已经由自动化证明。固定 Gateway 端口不具备自动跨端口故障切换；已知会话的 HTTP 补偿仍不保证任意乱序提交后的深层低序号缺口，也不构成多设备同步协议。

2026-10-05 将当前会话历史页改为顶部滚动加载：每次用 `before_seq` 从 MySQL 取更早 50 条，按 seq 插入已有消息前面，并按新增前后的 scrollHeight 保持视口；“加载更早”按钮可手动触发。最新历史页与 IndexedDB 中落在该页范围内或更新的 WebSocket 消息合并，按 `message_id` 去重、按 seq 排序，避免历史响应重画时丢掉同期到达的消息。复跑 `node --test scripts/history-sync.test.cjs`（4/4 通过）和 `python3 scripts/validate_frontend.py`（通过）；尚未做真实浏览器滚动交互 E2E，因此这部分应说明为代码已实现、静态与合并逻辑已验证，浏览器端到端体验尚待手动验收。

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
scripts/verify_cluster_topology.sh       两个 Logic 的 Etcd 注册与 3 Gateway readiness
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
