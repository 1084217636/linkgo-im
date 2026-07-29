# 多服务器 production overlay 说明

> 这是公司拓扑的应用清单示例，不是已经部署到生产环境的证据。Kubernetes 入门先读 [`docs/handbook/18_KUBERNETES_DEPLOYMENT.md`](../../../docs/handbook/18_KUBERNETES_DEPLOYMENT.md)。

这套 overlay 只渲染 LinkGo 的 Gateway、Logic、Transfer 等应用工作负载，不部署本地 demo 使用的单节点 Redis、MySQL、Kafka 和 Etcd。

使用前需要：

1. 把 `configmap.yaml` 中所有 `example.internal` 换成实际中间件稳定入口。
2. 通过 Secret 管理系统，根据 `secret.example.yaml` 创建 `linkgo-im-secret`，不要把真实密码提交到 Git。
3. 确保每个 Logic Pod 能把自己的 Pod IP 注册到共享 Etcd。
4. 先渲染检查，再应用清单并把三个 Deployment 更新为不可变镜像标签。

渲染命令：

```bash
kubectl kustomize deploy/k8s/production \
  --load-restrictor LoadRestrictionsNone
```

`LOGIC_ADDR` 必须保持未设置，使 Gateway 从 Etcd `/services/logic` 获取存活 Logic，并由 zRPC `p2c_ewma` 选择实例。

Redis 通过托管服务或 Sentinel-aware 代理/VIP 暴露一个稳定地址；MySQL 通过 primary/proxy 地址写入，复制和故障切换位于该入口后。当前应用不原生实现 Redis Cluster 分片、Sentinel 客户端，也不实现 MySQL 应用层读写分离。

当前 `scripts/k8s_release.sh` 固定应用包含单节点依赖的 demo base，不能直接用于本 overlay。production overlay 目前需要单独渲染、应用、更新镜像并逐个等待 rollout；不要描述成已经完成一键生产发布。
