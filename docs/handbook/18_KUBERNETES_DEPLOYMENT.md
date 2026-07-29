# 18 Kubernetes 多服务器部署

## 本章前置

你已经理解多 Gateway、多 Logic、共享中间件，以及镜像和容器。本章才正式引入 Kubernetes。

## 本章目标

能从零解释 Pod、Deployment、Service、ConfigMap、Secret、Probe、HPA；能画出 LinkGo 公司单集群拓扑。

## Kubernetes 解决什么问题

容器解决“程序怎样打包”；Kubernetes 解决“很多容器怎样被调度、发现、扩缩、探活和发布”。通常简称 K8s。

## 六个最小概念

### Pod

K8s 的最小调度单位。一个 LinkGo Gateway 容器通常运行在一个 Pod 中。Pod 可能被销毁并重新创建，IP 也可能变化。

### Deployment

描述某类 Pod 需要运行多少份、使用哪个镜像、怎样滚动更新。例如 Gateway Deployment 的 `replicas: 3` 表示期望三份 Gateway Pod。

### Service

给一组会变化的 Pod 提供稳定网络入口。Gateway Service 让外部入口能把新连接送到 Gateway Pod。

Logic 的项目主路径不是只依赖一个 Service 长连接做负载均衡，而是让各 Logic 实例注册 Etcd，Gateway 获得实例列表后进行客户端选择。

### ConfigMap

保存非敏感配置，例如 Kafka Topic、超时时间和服务地址。

### Secret

保存密码、JWT Secret、数据库 DSN 等敏感配置。Secret 仍需结合权限和加密管理，不能因为名字叫 Secret 就认为绝对安全。

### Probe

K8s 周期请求健康接口：readiness 决定是否接收新流量，liveness 判断是否需要重启容器。

## 公司场景的目标单集群拓扑

下面是用来理解多服务器部署的参考目标，不是仓库已经创建的全部资源：当前 production overlay 没有云负载均衡、Ingress/TLS，也只通过地址引用外部 Redis/MySQL/Kafka/Etcd。

```text
客户端
  ↓
云负载均衡 / Ingress
  ↓
Gateway Deployment（多 Pod）
  ↓ gRPC，Etcd 发现 + p2c_ewma
Logic Deployment（多 Pod）
  ├── Redis HA 稳定入口
  ├── MySQL primary/proxy 稳定入口
  └── Kafka Broker 集群

Kafka → Transfer Deployment（多 Pod）→ Redis → 目标 Gateway
```

本项目 production overlay 只部署应用工作负载，不假装用单 Pod Redis/MySQL 代表公司高可用数据库。

## Logic 为什么注入 POD_IP

Logic 监听 `0.0.0.0:9001`，但其他 Pod 不能把 `0.0.0.0` 当目标地址。K8s Downward API 将实际 Pod IP 注入 `POD_IP`；go-zero 注册 Etcd 时使用 `POD_IP:9001`。

Gateway 不设置 `LOGIC_ADDR` 时，通过 `/services/logic` 获取存活实例并使用 `p2c_ewma` 选择。它在两个候选实例中结合负载估计做选择，目标是减少热点。

## 扩容 Gateway 会不会搬迁旧连接

不会。WebSocket 是长连接：新增 Gateway-4 后，负载均衡主要把新连接送过去，旧连接仍留在原 Pod。

公司级连接服务通常希望下线旧 Pod 时完成：

```text
readiness 失败，停止新连接
→ 进入 draining
→ 客户端自然断开或收到重连提示
→ 客户端连接新 Gateway
→ 新 route 覆盖旧 route
→ 根据现有 Redis 状态补偿，并可主动查询 MySQL 历史
```

这是目标流程，不是当前完整实现。仓库有 readiness probe 和滚动策略，但没有显式 `preStop` draining、服务端重连提示，也没有网页自动重连循环；Pod 终止时连接会断开，需要用户手动重连。因此不能把完整优雅迁移说成已经实现的产品体验。

## HPA 是什么

HPA 根据 CPU 或自定义指标调整副本数。连接型服务只看 CPU 不一定合理，还应关注连接数、队列深度和事件循环压力。当前三类 HPA 都只使用 CPU，并要求集群已有 metrics-server；真实阈值仍需压测校准。

## 滚动发布和回滚

Gateway 和 Logic 显式配置 `maxUnavailable: 0`、`maxSurge: 1`，表示发布时先增加新 Pod，确认 ready 后再减少旧 Pod；Transfer 当前没有显式使用同一策略。发布脚本等待 rollout，并在失败时执行 `rollout undo`。

这降低停机风险，但不能保证所有长连接无感；客户端仍必须具备重连和恢复能力。

还要区分两套清单：`scripts/k8s_release.sh` 当前固定应用包含单节点依赖的 `deploy/k8s` demo base；`deploy/k8s/production` 只渲染应用工作负载，但发布脚本尚未支持选择该 overlay。因此可以演示脚本逻辑，不能把它说成已完成 production overlay 的一键发布。

## 代码锚点

- `deploy/k8s/gateway.yaml`
- `deploy/k8s/logic.yaml`
- `deploy/k8s/transfer.yaml`
- `deploy/k8s/configmap.yaml`
- `deploy/k8s/production/`
- `scripts/k8s_release.sh`
- `scripts/validate_k8s.sh`

## 动手练习

不需要先拥有集群，也能渲染检查：

```bash
kubectl kustomize deploy/k8s --load-restrictor LoadRestrictionsNone
kubectl kustomize deploy/k8s/production --load-restrictor LoadRestrictionsNone
```

比较两份输出：本地演示版包含单节点依赖，production overlay 不包含它们。

## 闭卷检查

1. Docker 与 Kubernetes 分别解决什么？
2. Pod、Deployment、Service 的关系是什么？
3. ConfigMap 和 Secret 有什么区别？
4. Logic 为什么要注册 Pod IP？
5. 新增 Gateway 为什么不会自动搬迁旧 WebSocket？
6. production overlay 为什么不部署单节点 MySQL/Redis？

下一步：[19 完整调用链与代码地图](19_COMPLETE_CODE_WALK.md)
