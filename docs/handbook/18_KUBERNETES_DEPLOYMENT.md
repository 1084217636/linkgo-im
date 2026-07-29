# 18 Kubernetes 多服务器部署

## 本章前置

你已经理解多 Gateway、多 Logic、共享中间件，以及镜像和容器。本章才正式引入 Kubernetes。

## 本章目标

能从零解释 Pod、Deployment、Service、ConfigMap、Secret、Probe、HPA；能画出 LinkGo 公司单集群拓扑。

## Kubernetes 解决什么问题

容器解决“程序怎样打包”；Kubernetes 解决“很多容器怎样被调度、发现、扩缩、探活和发布”。通常简称 K8s。

如果不用统一编排平台，运维人员要手工决定每个容器放在哪台服务器、进程退出后在哪里补一份、地址变化后怎样更新流量入口、发布时先停哪一份。实例越多，这些人工操作越慢，也越容易不一致。Kubernetes 选择声明“我希望系统保持什么状态”，再由控制程序持续靠近这个目标。代价是引入了集群组件、YAML、权限和网络等额外复杂度；一个项目有 K8s 文件不等于它自动获得生产高可用。

## 先区分 Cluster、Node 和 Pod

- Cluster（集群）：由一组机器和 Kubernetes 管理组件组成的整体。
- Node（节点）：集群里真正提供 CPU、内存和网络的物理服务器或虚拟机。
- control plane（控制面）：保存期望状态并运行控制器、调度器等管理组件，可以先理解为集群的“大脑”。
- scheduler（调度器）：为新 Pod 选择一个满足资源和规则的 Node。
- Pod：被放到 Node 上运行的最小调度单位，里面通常运行一个主要容器。

关系是：

```text
Cluster
├── control plane
└── 多个 Node
    └── 每个 Node 可以运行多个 Pod
        └── Pod 内运行容器
```

所以 `replicas: 3` 表示三个 Pod，不等于三台物理服务器。若三个 Pod 恰好落在同一 Node，该 Node 故障时仍会一起中断。当前清单使用 `topologySpreadConstraints` 尽量按主机分散 Gateway/Logic/Transfer，但 `ScheduleAnyway` 表示资源不足时仍允许不均匀放置，它不是“三台独立服务器”的硬保证。

## 六个最小概念

### Pod

K8s 的最小调度单位。一个 LinkGo Gateway 容器通常运行在一个 Pod 中。Pod 可能被销毁并重新创建，IP 也可能变化。

问题是容器实例会退出、机器也会故障。直接把固定 Pod IP 写进客户端，重建后地址就失效。K8s 允许把 Pod 当成可替换实例；代价是本机内存和本机 WebSocket 连接也随旧 Pod 消失，持久状态不能只放在 Pod 里。

### Deployment

描述某类 Pod 需要运行多少份、使用哪个镜像、怎样滚动更新。例如 Gateway Deployment 的 `replicas: 3` 表示期望三份 Gateway Pod。

Deployment 背后的控制器会反复比较“期望状态”和“实际状态”：

```text
期望 Gateway Pod = 3
→ 某个 Pod 退出，实际只剩 2
→ 控制器创建 1 个新 Pod
→ scheduler 为它选择 Node
→ 实际重新接近 3
```

这叫 reconciliation（协调循环）。没有它，退出后要人工登录机器重启。它能补回实例数量，却不能把旧 Pod 内存中的连接搬进新 Pod，也不能自动修复业务数据错误。

### Service

给一组会变化的 Pod 提供稳定网络入口。Gateway Service 让外部入口能把新连接送到 Gateway Pod。

Pod 会带 label（键值标签），例如 `app: gateway`；Service 的 selector（选择器）使用同样的标签找出目标 Pod。当前 Gateway Service 的过程是：

```text
Service selector: app=gateway
→ 找到带 app=gateway 的 Pod
→ 只把可接收流量的端点放入转发表
→ 新连接转发到其中一个 Pod 的 8090
```

如果 label 和 selector 写错，Pod 即使健康运行，Service 后面也没有目标。Service 提供的是稳定入口和转发，不保存 WebSocket 对象；一个已经建立的长连接仍固定在当时选中的 Gateway Pod。

还要区分内部 Service 与外部入口：Service 通常先解决集群内稳定访问；云负载均衡或 Ingress（按域名、路径把外部流量转给 Service 的入口规则）再承接公网请求。TLS 是在网络传输中加密并校验服务身份的协议。当前 production overlay 没有创建云负载均衡、Ingress 或 TLS，因此只是一组应用工作负载清单。

Logic 的项目主路径不是只依赖一个 Service 长连接做负载均衡，而是让各 Logic 实例注册 Etcd，Gateway 获得实例列表后进行客户端选择。

### ConfigMap

保存非敏感配置，例如 Kafka Topic、超时时间和服务地址。

当前 Pod 用 `envFrom` 把 ConfigMap 的键值注入环境变量，Go 进程启动时读取它们。这样同一镜像可以在本地和公司环境使用不同地址；若把地址写死进镜像，每换环境都要重新构建。代价是环境变量在进程启动后不会因为 ConfigMap 被修改就自动刷新，通常还要滚动重启 Pod，并确认新值兼容旧版本。

### Secret

保存密码、JWT Secret、数据库 DSN 等敏感配置。Secret 仍需结合权限和加密管理，不能因为名字叫 Secret 就认为绝对安全。

Secret 也通过环境变量注入当前容器，但应由部署平台或秘密管理系统创建，而不是把真实值提交到 Git。它解决的是“把敏感值与普通源码配置分开管理”；代价是还要管理谁能读取、怎样轮换以及存储时是否加密。基础 Secret 的内容常见只是 Base64 编码，编码不是加密。

### Probe

K8s 周期请求健康接口：readiness 决定是否接收新流量，liveness 判断是否需要重启容器。

如果没有 readiness，新 Pod 进程刚启动、依赖尚未准备好时就可能收到请求；选择 readiness 后，失败 Pod 会从 Service 的可用端点中移除。代价是检查条件过严会把仍能提供部分能力的 Pod 摘掉。

如果没有 liveness，已经卡死但进程未退出的容器可能一直占着资源；选择 liveness 后，连续失败会触发重启。代价是把短暂 Redis 抖动写进 liveness 可能造成重启风暴，所以依赖检查通常放 readiness。当前 Logic 使用 `tcpSocket` 探针，只能证明 gRPC 端口能建立 TCP 连接，不等于每条业务链路和依赖都健康。

## 公司场景的目标单集群拓扑

overlay 可以先理解成“在基础清单上替换或补充一部分配置”。本章的 production overlay 专门表达公司拓扑，只保留应用工作负载并替换外部依赖地址。下面是用来理解多服务器部署的参考目标，不是仓库已经创建的全部资源：当前 production overlay 没有云负载均衡、Ingress/TLS，也只通过地址引用外部 Redis/MySQL/Kafka/Etcd。

```text
客户端
  ↓
云负载均衡 / Ingress
  ↓
Gateway Deployment（多 Pod）
  ↓ gRPC，Etcd 发现 + p2c_ewma
Logic Deployment（多 Pod）
  ├── Redis HA（High Availability，高可用）稳定入口
  ├── MySQL primary/proxy 稳定入口
  └── Kafka Broker 集群

Kafka → Transfer Deployment（多 Pod）→ Redis → 目标 Gateway
```

本项目 production overlay 只部署应用工作负载，不假装用单 Pod Redis/MySQL 代表公司高可用数据库。

Gateway、Logic、Transfer 可以从同一镜像重建，主要持久数据却不能随便丢弃。若把单 Pod MySQL 当作公司数据库，Pod 或 Node 损坏时不仅是“少一个实例”，还可能失去磁盘中的消息与关系数据。真正的有状态服务还要处理持久卷、复制、主从切换、备份、恢复和版本升级。

因此 production overlay 选择让 Redis、MySQL、Kafka、Etcd 位于应用发布边界之外，通过稳定地址连接托管服务、代理或独立高可用集群。好处是应用滚动发布不会顺便重建数据组件；代价是高可用和故障切换并没有消失，只是由外部平台负责。当前仓库没有这些外部集群的真实部署证据。

这份 production overlay 本身也只是模板：`*.example.internal` 都是必须替换的占位地址，`secret.example.yaml` 不在 production overlay 的包含文件清单中（这份清单写在 `kustomization.yaml`），真实 `linkgo-im-secret` 必须由秘密管理系统另行创建。所以它可以直接渲染检查，但不能原样应用成可运行的公司环境。

## Logic 为什么注入 POD_IP

Logic 监听 `0.0.0.0:9001`，但其他 Pod 不能把 `0.0.0.0` 当目标地址。K8s Downward API 将实际 Pod IP 注入 `POD_IP`；go-zero 注册 Etcd 时使用 `POD_IP:9001`。

Gateway 不设置 `LOGIC_ADDR` 时，通过 `/services/logic` 获取存活实例并使用 `p2c_ewma` 选择。它在两个候选实例中结合负载估计做选择，目标是减少热点。

## 扩容 Gateway 会不会搬迁旧连接

不会。WebSocket 是长连接：新增 Gateway-4 后，负载均衡主要把新连接送过去，旧连接仍留在原 Pod。

公司级连接服务通常希望下线旧 Pod 时完成：

```text
readiness 失败，停止新连接
→ 进入 draining：停止接收新连接，给旧连接退出时间
→ 客户端自然断开或收到重连提示
→ 客户端连接新 Gateway
→ 新 route 覆盖旧 route
→ 根据现有 Redis 状态补偿，并可主动查询 MySQL 历史
```

这是目标流程，不是当前完整实现。仓库有 readiness probe 和滚动策略，但没有显式 `preStop`（容器终止前执行的钩子）来完成 draining、服务端重连提示，也没有网页自动重连循环；Pod 终止时连接会断开，需要用户手动重连。因此不能把完整优雅迁移说成已经实现的产品体验。

## HPA 是什么

HPA 是 Horizontal Pod Autoscaler，即 Pod 水平自动扩缩器。它根据 CPU 或自定义指标调整副本数。连接型服务只看 CPU 不一定合理，还应关注连接数、队列深度和事件循环压力。当前三类 HPA 都只使用 CPU，并要求集群已有 metrics-server；真实阈值仍需压测校准。

metrics-server 是收集 Pod CPU、内存等资源使用情况的集群组件。清单中的 CPU `requests` 是调度时为容器声明的基准需求，当前 HPA 的 `averageUtilization` 是实际 CPU 相对这个基准的百分比。当前扩容因果链是：

```text
metrics-server 提供各 Pod CPU 使用率
→ HPA 周期计算平均利用率
→ 超过目标且未达到 maxReplicas 时，提高 Deployment 的期望副本数
→ Deployment 创建 Pod，scheduler 放到 Node
→ 新 Pod readiness 成功后进入 Service 端点
→ 新连接才可能被分到新增 Gateway
```

如果没有 HPA，突发时只能人工修改副本数，响应更慢；选择 HPA 可以自动调整，但它不是瞬时动作，新 Pod 拉取镜像和启动需要时间。对 Gateway 只看 CPU 也可能漏掉“连接很多但 CPU 暂时不高”或队列积压，所以后续应结合连接数、队列深度和压测确定指标。缩容同样不会迁移旧 WebSocket，终止 Pod 前仍需要 draining 和客户端重连。

## 滚动发布和回滚

`kubectl` 是操作 Kubernetes 的命令行工具。本节先会遇到四个命令：`kubectl apply` 把清单交给集群，`kubectl set image` 修改 Deployment 应使用的镜像，`kubectl rollout status` 等待滚动发布结果，`kubectl rollout undo` 尝试切回历史 revision。它们任何一步都可能因权限、配额、镜像或应用健康问题失败。

Gateway 和 Logic 显式配置 `maxUnavailable: 0`、`maxSurge: 1`，表示发布时先增加新 Pod，确认 ready 后再减少旧 Pod；Transfer 当前没有显式使用同一策略。发布脚本等待 rollout，并在失败时执行 `rollout undo`。

Deployment 内部会用 ReplicaSet 管理“某一个 Pod 模板版本”的实例集合。这里第一次出现 ReplicaSet，只需要知道：修改 Deployment 镜像后，K8s 创建代表新版本的 ReplicaSet，并逐步缩小旧版本的 ReplicaSet。

从第 17 章的不可变镜像到运行版本，完整因果链是：

```text
CI 已验证并由调用者推送 image:<commit-sha>
→ 发布脚本 apply 清单并 set image
→ Deployment 创建新版本 ReplicaSet 和第一个新 Pod
→ readiness 成功后，新 Pod 才进入 Service
→ 再逐步终止旧 Pod，直到新版本达到期望副本数
→ rollout 超时或 smoke test（发布后的最小连通性检查）失败时，rollout undo 尝试恢复上一个 Pod 模板
```

如果直接停掉全部旧 Pod 再启动新 Pod，启动失败会导致整个服务没有可用实例。`maxUnavailable: 0` 与 `maxSurge: 1` 选择用额外一份临时资源换取更低的发布中断风险；代价是集群必须有容量容纳额外 Pod，长连接仍会在旧 Pod 终止时断开。

回滚只能尝试恢复 Deployment 保存的旧 Pod 模板和镜像，而且依赖仍有可用的历史 revision（修订版本）及可运行旧镜像。首次发布没有旧 revision，脚本中的 undo 命令也容忍失败，所以自动回滚后仍必须检查 rollout 和服务状态。它不会反向恢复 MySQL 数据、Redis 状态、外部 Kafka 消息或不兼容的数据库迁移。因此发布前仍要设计向前/向后兼容的数据变更，不能把 `rollout undo` 理解成整个系统时光倒流。

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

`kubectl` 是操作 Kubernetes 的命令行工具。`kustomize` 按 `kustomization.yaml` 合并基础清单和 overlay，只把最终 YAML 输出到屏幕；`--load-restrictor LoadRestrictionsNone` 允许当前 production overlay 引用上级目录里的共享清单。渲染成功只说明文件能合并，不等于已经连接集群或成功部署。

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
