# Docker、CI 与 Kubernetes 操作手册

> 这是一份查命令的操作手册，不是入门教材。第一次学习请先读手册第 [17 章](../handbook/17_DOCKER_AND_CI.md) 和第 [18 章](../handbook/18_KUBERNETES_DEPLOYMENT.md)。

## 1. 三个工具分别做什么

```text
Docker / Compose   在一台开发机复现多个进程和依赖
GitHub Actions     每次 push/PR 在干净机器上自动检查代码
Kubernetes         在一个集群中管理应用 Pod、探针、扩缩和滚动发布
```

当前 GitHub Actions 只做 CI 检查，不推送镜像，也不连接真实 Kubernetes 集群执行 CD。`deploy/k8s/production` 是可渲染的应用部署样例，不是线上运行证明。

## 2. 第一次只做本地静态检查

在仓库根目录依次执行：

```bash
make fmt-check
make test
make build
make frontend-static-check
make docs-check
```

这些命令分别检查 Go 格式、单元测试、三个 Go 服务能否编译、网页与接口契约、文档结构。它们不启动 Redis、MySQL 或 Kafka。

检查 Compose 配置是否能正确合并：

```bash
make compose-config
make compose-light-config
```

这里只解析 YAML，不代表容器已经启动。

## 3. Docker Compose 本地环境

### 3.1 轻量环境

```bash
make docker-light-up
```

轻量环境用于登录、好友、单聊、历史、ACK、红包和 AI 基础联调，不启动 Kafka/Transfer，因此不能用来证明完整群聊异步扩散。

停止：

```bash
make docker-light-down
```

### 3.2 完整环境

```bash
make docker-up
```

完整 Compose 启动 Redis、MariaDB（MySQL 兼容）、Etcd、Kafka/Zookeeper、Logic、Transfer 和三个 Gateway。三个 Gateway 端口是 `8090/8091/8092`。

查看状态和日志：

```bash
docker compose ps
docker compose logs gateway-a logic transfer
```

停止但保留数据卷：

```bash
make docker-down
```

不要在有重要本地数据时随意执行带 `-v` 的删除命令，因为它会删除数据卷。

## 4. GitHub Actions 当前执行什么

工作流文件是 `.github/workflows/ci.yml`，在 push 到 `main/master` 或提交 Pull Request 时触发。

```text
test-build
  ├── Go 格式
  ├── Go 单元测试
  ├── 三个服务构建
  ├── 前端静态契约
  ├── 文档结构
  ├── Compose 渲染
  └── Prometheus 配置与告警规则

docker-build（等待 test-build）
  └── 构建镜像，但 push=false

manifest-check（等待 test-build）
  └── 校验并渲染 Kubernetes 清单
```

CI 失败时先点开失败 job，再看第一个失败 step。不要只看最后一行 `Process completed with exit code 1`。在本地运行同一个 `make` 命令复现后再修改。

## 5. Kubernetes 本地演示清单

`deploy/k8s/` 包含应用和单节点依赖，目的是在 kind/minikube 等本地集群演示。先安装 `kubectl`，仅渲染和静态校验不要求集群在线：

```bash
make k8s-check
make k8s-render
```

有可访问集群后，先做客户端 dry-run：

```bash
make k8s-dry-run
```

确认当前 `kubectl` 上下文确实指向你的练习集群，再部署：

```bash
kubectl config current-context
make k8s-apply
kubectl -n linkgo-im get pods
kubectl -n linkgo-im get svc
```

不要使用 `kubectl apply -f deploy/k8s/`：项目通过 Kustomize 生成初始化 SQL ConfigMap，并需要允许读取目录外的 `sql/init.sql`；Makefile 已封装正确渲染命令。

本地访问 Gateway：

```bash
kubectl -n linkgo-im port-forward svc/gateway 8090:80
curl http://127.0.0.1:8090/healthz
curl http://127.0.0.1:8090/readyz
```

删除练习环境清单：

```bash
make k8s-delete
```

## 6. production overlay 是什么

```bash
kubectl kustomize deploy/k8s/production \
  --load-restrictor LoadRestrictionsNone
```

它只渲染 Gateway、Logic、Transfer 等应用工作负载，要求 Redis、MySQL、Kafka、Etcd 已由外部提供稳定入口。示例中的 `example.internal` 必须换成实际地址，真实 Secret 不能提交到 Git。

当前应用边界：

- Redis 使用一个稳定入口，不原生实现 Redis Cluster/Sentinel 客户端。
- MySQL 使用一个 `DB_DSN`，不在应用内实现读写分离。
- 清单没有 Ingress/TLS 和前端 Deployment。
- 仓库没有真实公司生产集群的部署证据。

## 7. demo base 的不可变镜像发布与回滚

已有练习集群和可拉取镜像时才运行：

```bash
make k8s-release IMAGE=ghcr.io/your-org/linkgo-im:<git-sha>
```

脚本拒绝 `latest`，更新三类 Deployment，等待 rollout，并请求 Gateway readiness；失败时对已更新的 Deployment 执行 `rollout undo`。这验证的是发布流程，不保证 WebSocket 客户端无感迁移，长连接仍需要断线重连和消息恢复。

当前脚本固定应用包含单节点依赖的 `deploy/k8s` demo base，不支持选择 `deploy/k8s/production`。production overlay 目前要单独渲染、应用并更新不可变镜像，不能把上面命令描述为公司拓扑的一键生产发布。

## 8. 故障排查顺序

```text
1. 命令是否在仓库根目录执行
2. docker/kubectl 是否安装，daemon 或集群是否可用
3. 配置是否能渲染
4. 容器或 Pod 是否 Running/Ready
5. healthz/readyz 是否通过
6. 再看对应 Gateway、Logic、Transfer 日志
```

面试时只陈述自己实际运行并保存证据的步骤。能渲染清单不等于已经完成真实集群发布。
