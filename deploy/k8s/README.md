# Kubernetes 部署说明

这组清单用于展示 LinkGo-IM 的基础 K8s 部署方式，覆盖 Gateway、Logic、Transfer 三个业务进程。

它适合简历和面试中说明：

- 如何把 Docker 镜像部署到 K8s。
- 如何通过 ConfigMap / Secret 管理配置。
- 如何为 HTTP 服务配置 readinessProbe / livenessProbe。
- 如何为 gRPC 服务配置 tcpSocket 探针。
- 如何用 resources requests/limits 控制资源。
- 如何配合 Prometheus annotation 暴露指标抓取入口。

## 使用方式

先构建并推送镜像，或者在本地 kind/minikube 中导入镜像：

```bash
docker build -t linkgo-im:local .
```

修改 `gateway.yaml`、`logic.yaml`、`transfer.yaml` 里的镜像地址，或者让集群可以拉取 `linkgo-im:local`。

部署：

```bash
kubectl apply -f deploy/k8s/
kubectl -n linkgo-im get pods
kubectl -n linkgo-im get svc
```

本地访问 Gateway：

```bash
kubectl -n linkgo-im port-forward svc/gateway 8090:80
curl http://127.0.0.1:8090/healthz
curl http://127.0.0.1:8090/readyz
```

清理：

```bash
kubectl delete -f deploy/k8s/
```

## 注意

这些清单默认假设 Redis、MySQL、Kafka、Etcd 已经在集群内或外部可访问，并通过 `configmap.yaml` 配置地址。生产环境里 Secret 不应提交真实密码，这里只保留演示占位值。
