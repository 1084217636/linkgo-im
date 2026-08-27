# 07 Docker、CI、Kubernetes 与测试

## 1. Docker 镜像、容器和 Compose 分别是什么？

### 面试官问

你为什么需要 Docker？Compose 算不算多服务器部署？

### 30 秒回答

镜像是只读运行模板，容器是镜像的一次运行实例。LinkGo 用多阶段 Dockerfile 编译 Gateway、Logic、Transfer，再用非 root 用户运行。Compose 在一台开发机上把应用和 MySQL、Redis、Etcd、Kafka 组织到同一虚拟网络，用于复现依赖和演示，不是多台服务器或生产高可用证明。

### 详细回答

Docker 解决环境和制品一致性。Builder 阶段有 Go 编译器，运行阶段只复制二进制、配置和必要文档，减少最终镜像内容。三个服务共用同一镜像，通过不同 command 启动不同二进制。

Compose 给服务名提供内部 DNS。容器中的 `localhost` 只指当前容器，因此 Logic 连接 Redis 要用 `redis:6379`，连接 MySQL 要用 `mysql:3306`。宿主机端口映射只用于电脑外部访问，比如浏览器通过宿主端口进入 Gateway。

### 结合 LinkGo

运行镜像使用 UID/GID 10001。Compose 启动 MySQL、Redis、Etcd、Kafka、Zookeeper、Gateway、Logic 和 Transfer。初始化 SQL 的挂载用于新演示数据库建表，不等于业务数据已经有备份。

### 为什么这样选，替代方案是什么

直接在电脑安装所有依赖更容易出现版本和环境差异。虚拟机隔离更完整但更重。容器适合当前可重复演示。代价是要理解容器网络、volume 和配置注入。

### 边界与追问

镜像 build 成功只证明制品能生成，不证明依赖可用和业务正确。Compose 的单节点数据库与 broker 也不能代表生产集群。

## 2. CI 和 CD 有什么区别？当前 GitHub Actions 做到了哪一步？

### 面试官问

你的项目说有 CI/CD，真的自动部署了吗？

### 30 秒回答

CI 是对明确 commit 自动执行格式、静态检查、测试和构建。CD 是把通过验证的不可变制品发布到环境并处理 smoke 与回滚。LinkGo 当前 GitHub Actions 会测试、race、构建服务、检查前端和文档、验证配置、构建但不推送 Docker 镜像，并渲染 Kubernetes 清单。它没有自动推镜像或部署真实生产集群，所以准确说法是 CI 完成，CD 有脚本和清单示例。

### 详细回答

一次 push 或 Pull Request 触发 workflow。GitHub 在干净 runner 上 checkout 当前 commit，准备 Go 环境并执行检查。基础 job 成功后，Docker build 和 manifest render 才继续。`needs` 控制依赖，彼此无依赖的 job 可以并行。

可追溯发布应该把 Git commit SHA、测试结果、镜像标签和部署版本关联起来。`latest` 会不断改变指向，出问题时难以确认正在运行哪一版。当前 Actions 使用 SHA 标签做构建验证，但配置 `push: false`，没有登录镜像仓库。

### 结合 LinkGo

CI 包括 `fmt-check`、`vet`、普通测试、race、build、前端静态契约、文档链接和布局、Compose/Prometheus 配置、Docker build 与 K8s render。发布脚本能执行 `apply`、`set image`、等待 rollout、smoke 和失败 undo，但仍需要调用者提供已经可拉取的不可变镜像。

### 为什么这样选，替代方案是什么

先把自动检查做扎实，比为了写“CD”接一个无法维护的公网集群更可信。完整 CD 需要镜像仓库凭据、目标集群权限、环境审批和 Secret 管理，这些不该硬编码到个人公开仓库。

### 边界与追问

CI 通过不代表生产流量验证。当前高级代码所在分支要以仓库实际分支为准，面试前必须确认远程分支和 commit，不能默认 `main` 就是最新功能。

## 3. Kubernetes 的 Pod、Deployment 和 Service 是什么关系？

### 面试官问

用你的 Gateway 举例解释 Kubernetes，不要只背定义。

### 30 秒回答

Pod 是最小调度单位，里面运行 Gateway 容器；Deployment 声明期望多少个 Gateway Pod，并在 Pod 退出或版本变化时协调副本；Service 用 label selector 找到 ready 的 Gateway Pod，提供稳定入口并把新连接分给它们。Pod IP 会变化，所以客户端不能硬编码 Pod 地址。

### 详细回答

Kubernetes 的核心是声明期望状态。Deployment 发现实际副本少于 replicas，会创建新 Pod，由 scheduler 选择 Node。Service 不创建 Pod，只根据标签维护网络端点。readiness 未通过的 Pod 不应进入 Service 端点。

Pod 可以被替换，本机内存和 socket 会随之消失。MySQL、Redis 等持久依赖不能因为应用 Deployment 滚动而一起当作普通无状态 Pod 随便重建。production overlay 因此只部署应用，并通过稳定地址连接外部依赖。

### 结合 LinkGo

Gateway、Logic、Transfer 各有 Deployment。Logic 通过 Downward API 获取 POD_IP 并向 Etcd 注册。三个服务使用同一镜像、不同启动命令。生产 overlay 中 `*.example.internal` 只是外部依赖占位符。

### 为什么这样选，替代方案是什么

少量进程可以用 systemd 和手工脚本管理。Kubernetes 适合多实例的调度、探活、扩缩和滚动发布，代价是平台复杂度高。个人项目保留可渲染清单用于学习，不把它包装成线上经验。

### 边界与追问

`replicas: 3` 不等于三台物理机器。当前清单有拓扑分散倾向，但不是节点级硬保证。K8s 清单渲染成功也不代表有真实集群、云 LB、持久卷和权限可用。

## 4. Liveness、readiness 和业务 smoke 有什么区别？

### 面试官问

进程能返回 200，为什么还要三种检查？

### 30 秒回答

Liveness 判断进程是否陷入需要重启的状态，readiness 判断实例现在是否应该接新流量，业务 smoke 验证一条真实用户链路。LinkGo 的 readyz 会检查各服务真正需要的 Redis、MySQL、Kafka 或 Logic gRPC 依赖。依赖恢复后还要重新跑登录和消息，不应只看进程活着。

### 详细回答

把所有依赖都绑进 liveness 会造成级联重启。Redis 短暂抖动时，所有应用 Pod 同时被杀会扩大故障。更合理的做法是进程事件循环还正常时保持 liveness，通过 readiness 摘掉新流量。

Readiness 也只是依赖和服务状态检查，可能漏掉协议、权限和数据错误。业务 smoke 会真的登录、建立 WebSocket、发送和 ACK，覆盖面更接近用户，但仍只是少量固定场景。

### 结合 LinkGo

Gateway readyz 检查 Logic gRPC、Redis 和 MySQL；Logic 检查 Redis、MySQL 和 Kafka；Transfer 检查 Redis 和 Kafka。本地故障演练停止组件后观察 readyz 失败，恢复后再跑业务脚本。

### 为什么这样选，替代方案是什么

只有 TCP 端口检查成本低但语义太浅。每次 probe 都执行完整业务又会制造数据、放大依赖压力。分层检查让高频 probe 保持轻量，把真实业务放到发布后 smoke 和故障验收。

### 边界与追问

readyz 成功不能证明所有用户、所有分区和所有 SQL 都正常。当前没有正式生产 SLO 和告警通知闭环。

## 5. 滚动发布和回滚怎样工作？

### 面试官问

Gateway 升级如何减少中断？回滚能恢复数据库吗？

### 30 秒回答

Gateway 和 Logic 的滚动策略使用 `maxUnavailable: 0`、`maxSurge: 1`，先创建新 Pod，readiness 通过后再终止旧 Pod。旧 Gateway 摘流并经过 preStop 窗口，现有 WebSocket 仍会断开，由客户端重连。rollout 失败时脚本尝试 `kubectl rollout undo`，它只能恢复旧 Pod 模板和镜像，不能回滚 MySQL、Redis 或不兼容的数据迁移。

### 详细回答

不可变镜像先经过 CI 验证并被推到可访问仓库，发布脚本再修改 Deployment image。Kubernetes 创建新 ReplicaSet，逐步扩大新版本、缩小旧版本。readiness 决定新 Pod 何时进入流量。

`maxUnavailable: 0` 降低计划内可用副本减少，`maxSurge: 1` 允许临时多一个 Pod。它需要集群有额外资源。WebSocket 连接仍绑定旧 Pod，终止时不能无损迁移。

### 结合 LinkGo

仓库发布脚本包含 apply、set image、rollout status、smoke 和 ERR trap undo。当前脚本固定使用本地 demo base，尚未支持一键选择 production overlay。

### 为什么这样选，替代方案是什么

蓝绿发布能保留整套旧版本并快速切流，但资源成本更高，长连接切换仍需重连。金丝雀发布能逐步观察指标，但需要流量治理。当前滚动发布符合项目规模，重点是诚实说明边界。

### 边界与追问

当前只有脚本逻辑和清单静态验证，不是线上发布记录。第一次发布可能没有可用旧 revision，undo 也可能失败。

## 6. HPA 怎样扩容？为什么只看 CPU 不够？

### 面试官问

你的 Gateway 会自动扩容吗？根据什么指标？

### 30 秒回答

当前清单中的 HPA 根据 Pod CPU 相对 requests 的平均利用率调整 Deployment replicas，需要集群有 metrics-server。它只是示例。Gateway 属于长连接服务，连接很多时 CPU 未必高，真正校准还应看 WebSocket 连接数、队列深度、拒绝率和网络写延迟。HPA 新建 Pod 后也只承接新连接。

### 详细回答

HPA 周期读取指标，计算期望副本数，修改 Deployment 的 replicas。Deployment 创建 Pod，scheduler 分配 Node，新 Pod readiness 成功后才进入 Service。

扩容有延迟，包括采样、计算、拉取镜像和启动。因此不能等队列完全满了才扩。缩容还要避免频繁抖动，并处理连接 draining。

### 结合 LinkGo

Gateway、Logic 和 Transfer 都有 CPU HPA 示例。对 Logic，CPU 和 RPC 延迟可能更有相关性；对 Transfer，还应看 Kafka lag；对 Gateway，还应看连接数和 push queue depth。仓库已经暴露部分 Prometheus 指标，但 HPA 尚未接自定义指标。

### 为什么这样选，替代方案是什么

CPU 指标容易获取，适合清单示例。基于 Prometheus Adapter 的自定义指标更贴业务，但部署与阈值校准复杂。当前不伪造压测阈值，面试中把它作为下一步演进。

### 边界与追问

HPA 不能让容量线性增长，最终受 Redis、MySQL、Kafka、LB 和网络限制。当前 HPA 只经静态渲染检查，没有真实集群自动扩缩证据。

## 7. 你现在到底有哪些测试？

### 面试官问

这些可靠性设计是真实测试过，还是只写在文档里？

### 30 秒回答

项目有 Go 单元和包测试，覆盖队列顺序与背压、路由所有权、ACK、回放写失败、幂等和部分 SQL/Lua 边界；CI 运行普通测试与 race。`core_im_demo` 通过真实 HTTP 和 WebSocket 模拟用户，验证登录、JWT、单聊、ACK、离线重连和最小两成员 Kafka 群聊。故障脚本会停止 Redis、MySQL、Logic、Kafka、Transfer 和 Gateway，检查 readyz 和恢复后的业务 smoke。当前没有 Playwright 浏览器 E2E，也没有生产规模压测。

### 详细回答

测试要分层说明。单元测试定位快，适合纯函数、状态机和并发边界，但 fake 依赖不能证明真实协议。race 只检查测试实际执行路径中的数据竞争，不证明没有所有竞争。

协议 demo 会启动真实服务依赖，通过 HTTP 登录和 WebSocket 收发。它模拟的是两个认证用户，并创建最小两成员群。它能证明 Kafka 主链路跑通，但不能证明大群 fanout、多个接收者部分失败或生产 broker 容灾。

故障注入验证依赖停止时 readiness 失败、重启后恢复和业务 smoke。它在本地 Compose 环境运行，不能等同跨机器、跨可用区容灾。

### 结合 LinkGo

`core_im_demo` 不是跨 Gateway 测试，它使用单个 Gateway 入口完成真实协议链路。跨 Gateway 能力由双 Gateway Compose 配置、路由代码、定向频道与相关单元测试支持，但若要声称端到端验证，还应增加明确连接到不同 Gateway 端口的测试。

### 为什么这样选，替代方案是什么

Playwright 能验证按钮、DOM、浏览器 WebSocket 和页面状态，价值明确，但维护成本也高。当前项目把时间投入服务端协议与故障路径，只在文档中保留 Playwright 方案，不把未实现测试说成已完成。

### 边界与追问

不要报没有保存证据的并发量或吞吐。简历写测试时应列“测试了什么”，例如 ACK 丢失、队列满、旧连接误清理和 Kafka 重复消费，不要只写“完善测试体系”。

