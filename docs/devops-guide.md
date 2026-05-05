# Docker / Kubernetes / CI-CD 使用说明

这份文档说明项目里 Docker、Kubernetes 和 CI/CD 分别解决什么问题，以及如何使用。

## Docker 负责什么

Docker 用来把 Gateway、Logic、Transfer 三个 Go 服务打成同一个镜像，保证运行环境一致。

本地完整联调使用 Docker Compose：

```bash
docker compose up --build
docker compose ps
docker compose down
```

Compose 会启动：

- Redis
- MySQL
- Kafka + Zookeeper
- Etcd
- Logic
- Transfer
- 3 个 Gateway

Gateway / Transfer 已配置 healthcheck，Compose 会定时访问：

- Gateway: `http://localhost:8090/healthz`
- Transfer: `http://localhost:9102/healthz`

单独构建镜像：

```bash
docker build -t linkgo-im:local .
```

## Kubernetes 负责什么

Kubernetes 用来在集群中管理服务副本、滚动发布、健康检查、配置注入和资源限制。

项目清单在：

```text
deploy/k8s/
```

包含：

- `namespace.yaml`：隔离命名空间
- `configmap.yaml`：Redis、Kafka、MySQL、Etcd 等连接配置
- `secret.yaml`：Redis/JWT 等敏感配置占位
- `gateway.yaml`：Gateway Deployment + Service + PDB
- `logic.yaml`：Logic Deployment + Service + PDB
- `transfer.yaml`：Transfer Deployment + Service

部署：

```bash
kubectl apply -f deploy/k8s/
kubectl -n linkgo-im get pods
kubectl -n linkgo-im get svc
```

访问 Gateway：

```bash
kubectl -n linkgo-im port-forward svc/gateway 8090:80
curl http://127.0.0.1:8090/healthz
curl http://127.0.0.1:8090/readyz
```

清理：

```bash
kubectl delete -f deploy/k8s/
```

当前 K8s 清单默认 Redis、MySQL、Kafka、Etcd 已经存在。真实环境中通常使用云服务、独立 StatefulSet 或运维团队提供的中间件地址。

## CI/CD 负责什么

CI/CD 用来让每次提交自动完成测试、构建和交付前检查，避免代码只在本地可用。

项目已新增：

```text
.github/workflows/ci.yml
```

当前 CI 做三件事：

1. Go 单元测试：`go test ./...`
2. 三个服务构建：`gateway / logic / transfer`
3. Docker 镜像构建：验证 Dockerfile 可用
4. Kubernetes 清单检查：确认基础 manifest 文件存在

触发方式：

- push 到 `main` / `master`
- 提交 Pull Request

当前 workflow 默认只 build，不 push 镜像，也不自动部署到真实集群。这样更适合个人项目展示 CI/CD 基础能力，避免误操作线上环境。

## 真实 CD 可以怎么扩展

后续如果要接真实部署，可以在 GitHub Actions 里增加：

1. 登录镜像仓库，例如 Docker Hub / 阿里云 ACR / GitHub Container Registry。
2. 给镜像打 tag：`linkgo-im:${{ github.sha }}`。
3. push 镜像。
4. 使用集群 kubeconfig 执行 `kubectl apply -f deploy/k8s/`。
5. 通过 `kubectl rollout status deployment/gateway -n linkgo-im` 检查发布结果。

这部分需要真实镜像仓库和 K8s 集群凭证，所以当前没有默认开启。
