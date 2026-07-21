# Kubernetes 部署说明

这组清单用于展示 LinkGo-IM 的基础 K8s 部署方式，覆盖 Gateway、Logic、Transfer 三个业务进程，以及本地演示所需的 Redis、MariaDB、Etcd、Zookeeper、Kafka 依赖。

它适合简历和面试中说明：

- 如何把 Docker 镜像部署到 K8s。
- 如何通过 ConfigMap / Secret 管理配置。
- 如何为 HTTP 服务配置 readinessProbe / livenessProbe。
- 如何为 gRPC 服务配置 tcpSocket 探针。
- 如何用 resources requests/limits 控制资源。
- 如何配合 Prometheus annotation 暴露指标抓取入口。
- 如何通过 HPA、PDB、topologySpreadConstraints、NetworkPolicy 补齐工程化部署面。

## 使用方式

先构建并推送镜像，或者在本地 kind/minikube 中导入镜像：

```bash
docker build -t linkgo-im:local .
```

修改 `gateway.yaml`、`logic.yaml`、`transfer.yaml` 里的镜像地址，或者让集群可以拉取 `linkgo-im:local`。

渲染并检查清单：

```bash
make k8s-render
make k8s-check
make k8s-dry-run
```

部署：

```bash
make k8s-apply
kubectl -n linkgo-im get pods
kubectl -n linkgo-im get svc
kubectl -n linkgo-im get hpa
```

带不可变版本号发布并等待三类工作负载滚动完成：

```bash
make k8s-release IMAGE=ghcr.io/your-org/linkgo-im:<git-sha>
```

发布脚本拒绝 `:latest`，依次更新 Gateway、Logic、Transfer，等待 rollout，再通过 Gateway `/readyz` 做发布后 smoke test。任一步失败会对已经更新的 Deployment 执行 `rollout undo` 并等待回滚结束。Deployment annotation 会保存本次镜像和发布时间，便于审计。

本地访问 Gateway：

```bash
kubectl -n linkgo-im port-forward svc/gateway 8090:80
curl http://127.0.0.1:8090/healthz
curl http://127.0.0.1:8090/readyz
```

清理：

```bash
make k8s-delete
```

## 注意

这组清单偏本地演示和面试展示，内置了单副本依赖组件，方便在 kind/minikube 中一键跑起来。生产环境建议把 Redis、MySQL、Kafka、Etcd 替换成云厂商托管服务或独立高可用集群，再通过 `configmap.yaml` 和 `secret.yaml` 指向外部地址。

`secret.yaml` 里的密码和完整数据库 DSN 是演示值，生产环境必须通过 CI/CD Secret、SealedSecret、External Secrets Operator 或云 KMS 注入，不能提交真实密码。非敏感 ConfigMap 不再保存数据库口令。

`kustomization.yaml` 会把 `sql/init.sql` 生成成 `linkgo-im-mysql-init` ConfigMap，用于演示环境初始化数据库。由于该文件在 `deploy/k8s` 目录外，Makefile 使用 `kubectl kustomize --load-restrictor LoadRestrictionsNone` 渲染。
