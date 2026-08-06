# 17 Docker 与 GitHub Actions

## 本章前置

你已经理解每个服务是独立进程，也知道配置决定服务连接的地址。本章不要求 Kubernetes 知识。

## 本章目标

能解释镜像、容器、Compose、CI；知道它们分别解决“环境一致”和“自动验证”问题。

## 为什么本机能跑不代表别人能跑

Go 程序还依赖配置、数据库、Redis、Kafka 和初始化 SQL。只把源码发给别人，对方可能因为版本和环境不同启动失败。

## 镜像和容器

镜像是只读的运行模板，包含程序和所需文件；容器是镜像的一次运行实例。

```text
源码
→ Dockerfile 构建
→ image
→ docker run
→ container
```

同一个镜像可以启动多个 Gateway 容器。容器仍然是进程隔离环境，不是虚拟出一整台真实机器。

容器启动后会有自己的进程、文件视图和网络空间。这里最容易犯的错误是把容器当成“换了名字的本机进程”：

- 容器里的 `localhost` 只指当前容器自己。Logic 如果连接 `localhost:6379`，它寻找的是 Logic 容器里的 Redis，而不是旁边的 Redis 容器。
- 容器的可写文件层通常是临时的。删除并重建容器后，写在这个层里的数据可能消失；需要长期保存的数据必须放到外部数据库或持久卷。
- 镜像只打包程序和运行文件，不自动包含正在运行的 MySQL、Redis 数据，也不会把本机环境变量一起复制进去。

因此“镜像构建成功”只证明运行模板能生成，不证明整个系统已经能连上依赖并正确处理业务。

## Dockerfile 做什么

LinkGo 的 Dockerfile 先在构建阶段编译 Go 二进制，再把运行所需文件放入更小的运行镜像。多阶段构建能减少最终镜像体积和不必要工具。

问题是：编译 Go 需要编译器和模块缓存，运行二进制通常不需要。如果把整套编译环境留在最终镜像里，镜像更大、下载更慢，暴露的工具也更多。LinkGo 选择 `builder` 阶段完成三个 `go build`，运行阶段只复制 `gateway/logic/transfer`、配置和文档。代价是调试运行容器时没有完整编译工具，需要回到 builder 或开发环境排查。

三个服务共用同一个镜像，但由启动命令决定运行哪个二进制：Compose 中分别使用 `./gateway`、`./logic`、`./transfer`。这样一次构建能得到同一版本的三个程序；代价是镜像包含当前容器用不到的另外两个二进制。对本项目规模可以接受，生产中也可以拆成三个更小镜像。

运行阶段还创建固定 UID/GID 10001 的 `linkgo` 用户，并用 `USER 10001:10001` 启动应用。这样即使应用进程被利用，也不会默认拥有容器内 root 权限。它不能单独形成完整沙箱，K8s 仍需禁止提权、删除 Linux capabilities，并限制文件系统和网络权限。

阅读时只问：

1. 基础镜像是什么？
2. 在哪里执行 `go build`？
3. 最终复制了哪些二进制和配置？
4. 容器启动命令是什么？

## Docker Compose 做什么

YAML 是一种用缩进表达层级配置的文本格式。Compose 用一份 YAML 描述本地需要启动的多个容器及网络：

```text
MySQL
Redis
Etcd
Kafka
Zookeeper（当前本地 Kafka 镜像的协调依赖，LinkGo 业务代码不直连）
Gateway
Logic
Transfer
```

它的目标是“在一台开发机快速复现多组件环境”，不是生产集群证明。

### 多个容器为什么能互相找到

Compose 会为同一项目创建一张虚拟网络，并给服务名提供内部 DNS 解析。DNS 可以先理解成“把名字翻译成网络地址”的服务。因此当前配置使用：

```text
REDIS_ADDR=redis:6379
DB_DSN=...tcp(mysql:3306)...
ETCD_ENDPOINTS=etcd:2379
KAFKA_BROKERS=kafka:9092
```

这里的 `redis`、`mysql`、`etcd`、`kafka` 是 Compose 服务名，不是随便写的主机名。容器重建后内部 IP 可以变化，服务名仍由 Compose 重新解析；如果硬编码旧 IP，重建后连接就会失效。代价是这些名字只在 Compose 网络内部有效，宿主机（运行 Docker 的那台电脑）上的浏览器不能直接用 `http://gateway-a:8090` 访问它们。

### 容器端口和宿主机端口

`"8091:8090"` 的含义是：宿主机（运行 Docker 的电脑）的 `8091` 转发到容器内的 `8090`。

```text
浏览器访问 127.0.0.1:8091
→ Docker 端口映射
→ gateway-b 容器的 8090
```

容器之间通信通常直接使用服务名和容器端口，例如 `logic:9001`，不需要绕到宿主机的映射端口。若不区分这两个端口，就会出现“浏览器能访问，但另一个容器连错地址”或相反的问题。暴露端口也会扩大宿主机攻击面，所以只映射确实需要从外部访问的端口。

### volume、初始化和数据寿命

volume 可以把容器外的目录或由 Docker 管理的存储挂进容器。当前 MySQL 配置：

```text
./sql/init.sql:/docker-entrypoint-initdb.d/init.sql
```

这是把仓库里的初始化 SQL 挂进去，让 MySQL 在新数据目录第一次启动时建表和插入演示数据。它不是 MySQL 数据目录的显式命名持久卷。当前 Compose 没有为 MySQL `/var/lib/mysql` 声明命名卷，数据目录是否复用会受容器重建方式和匿名卷影响，不能依赖它保存业务数据；执行 `docker compose down -v` 删除相关卷后，下次将在新数据目录重新初始化。

问题、选择和边界可以归纳为：没有挂载，容器看不到初始化 SQL；只挂初始化 SQL，可以稳定重建演示环境；若要保留真实数据，还要为数据目录配置持久卷，并另外设计备份、恢复和数据库高可用。持久卷也不是备份，因为误删或错误写入可能同步保存在卷里。

### environment、healthcheck 和 depends_on

环境变量把“同一份镜像”连接到不同地址和凭据，避免为每个环境重新改源码构建镜像。代价是配置来源变多，排障时必须先确认容器实际收到的值，敏感变量也不能写进公开仓库。

`depends_on` 主要表达启动顺序，不天然证明依赖已经可以处理请求。当前 Logic 对 MySQL 使用 `condition: service_healthy`，会等待 MySQL 的 `healthcheck` 成功；对 Redis、Etcd、Kafka 主要只等待容器开始运行。进程启动和服务 ready 是两回事，例如 Kafka 进程刚启动时仍可能在初始化。因此应用仍要设置连接超时、重试或在依赖不可用时明确失败，不能把 `depends_on` 当作可靠性机制。

`healthcheck` 是在容器内周期执行的小检查。没有它，Compose 只能知道进程是否存在，不知道服务能否响应。检查过浅可能把故障服务判为健康，检查过深又可能因短暂依赖抖动反复判错；第 16 章的 `healthz/readyz` 区分仍然适用。

常用命令由 Makefile 包装：

```bash
make docker-up
make docker-down
make compose-config
```

先用 `compose-config` 检查 YAML 渲染，再真正启动。

## CI 是什么

CI 是 Continuous Integration，持续集成。代码 push 到 GitHub 后，由独立机器自动执行检查，避免“只在我的电脑通过”。

如果 Git 还不熟，先只记四个动作：commit 是保存一次有唯一编号的代码版本，这个编号通常叫 SHA；branch 是一条独立版本线；push 是把本地提交上传到远程仓库；Pull Request 是请求把一条版本线的改动审查后合入另一条版本线。CI 检查的对象必须能对应到一个明确 commit，否则检查结果无法和代码版本对上。

先认识 GitHub Actions 的五个词：

- workflow：`.github/workflows/ci.yml` 中描述的一整套自动流程。
- event：触发流程的事件。当前是推送到 `main/master`，或者创建、更新 Pull Request。
- runner：GitHub 提供的临时执行机器；它不会继承你电脑上偷偷安装的工具和未提交文件。
- job：一组可以在同一 runner 上执行的步骤。不同 job 通常在不同干净机器上运行。
- step：job 中的一步，例如拉取代码、安装 Go、运行测试。

Kubernetes 会在下一章从零解释；这里暂时把“Kubernetes 清单”理解为描述应用准备怎样部署的一组 YAML，“渲染”就是把分散配置合并成最终输出。CI 只检查这些描述能否正确组合，不要求你现在背其中字段。

当前流程的真实顺序是：

```text
push / Pull Request
→ GitHub 创建干净的 ubuntu runner
→ Checkout 把这次 commit 的代码拉下来
→ setup-go 按 go.mod 准备 Go
→ test-build 依次检查格式、go vet、普通测试、race 测试、构建、前端、文档和配置
→ 只有 test-build 成功
   ├→ docker-build 构建本次 SHA 标签的镜像
   └→ manifest-check 渲染 Kubernetes 清单
→ 任一步失败，整次检查显示失败并保留该步日志
```

`needs: test-build` 表示后两个 job 不会在基础检查失败时继续浪费时间。两个后续 job 之间没有依赖，可以并行运行。CI 的选择解决了“每个人是否执行了同一组检查”；代价是 runner 环境与真实生产环境仍不同，测试本身漏掉的场景 CI 也发现不了。

LinkGo 的 GitHub Actions 主要验证：

```text
格式
→ Go 测试
→ 服务构建
→ 前端静态契约
→ 文档结构与链接
→ Compose 配置
→ Prometheus 配置
→ Docker 镜像
→ Kubernetes 清单渲染
```

CI 通过只能证明这些步骤通过，不能证明真实生产流量、跨机房容灾或绝对无故障。

每一步存在的原因不同：

| 检查 | 不做可能留下什么问题 | 通过后仍不能证明什么 |
| --- | --- | --- |
| 格式检查 | 提交未统一格式，代码审查出现无意义差异 | 代码逻辑正确 |
| Go 测试 | 已覆盖行为回归后无人发现 | 未编写测试的并发、故障和真实依赖场景 |
| 服务构建 | 测试包通过但某个 main 或依赖无法编译 | 二进制启动后一定可用 |
| 前端静态契约 | 页面控件或请求字段与后端约定明显漂移 | 真实浏览器端到端交互完全正确 |
| 文档检查 | 学习目录、仓库内链接或运行时知识路径失效 | 文档中的每个技术判断都永远正确 |
| Compose/Prometheus 配置 | YAML 或告警规则语法错误 | 所有容器和告警通知真实可用 |
| Docker 构建 | Dockerfile 复制路径或构建阶段出错 | 镜像已推送、已扫描、已部署 |
| Kubernetes 渲染 | 清单无法合并或缺少关键资源种类 | 有真实集群、权限、存储和云负载均衡可运行 |

## CI/CD 不要混淆

- CI：自动检查和构建。
- CD：把通过验证的版本发布到环境。

当前仓库有发布脚本和 K8s 清单，可以在受控练习环境验证滚动发布与回滚；没有理由声称已经部署到真实公司生产集群。尤其要注意：`scripts/k8s_release.sh` 固定应用 `deploy/k8s` 本地 demo base，会一并部署单节点依赖；它还没有参数切换到 app-only 的 production overlay。

镜像仓库是保存和分发镜像的服务，作用类似“二进制版本仓库”。完整可追溯发布链应该是：

```text
Git commit SHA
→ CI 对这次 SHA 测试
→ 构建 image:<同一个 SHA>
→ 推送到镜像仓库
→ 部署清单明确引用这个不可变标签
→ 记录发布结果；失败时回到前一个标签
```

这样看到正在运行的镜像标签，就能反查源码 commit、测试结果和变更内容。如果只使用 `latest`，同一个名字会不断指向不同内容，出问题时无法确定运行的代码。

当前 GitHub Actions 的 `docker-build` 使用 `push: false`，只在临时 runner 上验证镜像能构建，并没有登录或推送到真实镜像仓库。`scripts/k8s_release.sh` 要求调用者另外提供一个已可拉取的不可变镜像地址。因此当前链路停在“CI 构建验证 + 发布脚本示例”，不是自动生产 CD。

## 为什么镜像标签不能用 latest

`latest` 不能明确对应哪次代码，回滚和审计困难。发布应使用不可变版本，例如 commit SHA：

```text
ghcr.io/example/linkgo-im:530d3e7
```

## 本章代码阅读任务

| 顺序 | 打开位置 | 这次只看什么 |
| --- | --- | --- |
| 1 | `Dockerfile` 的 `builder` 阶段、三个 `go build`、runtime COPY、`USER 10001:10001` | 画源码到三个二进制再到 non-root 运行镜像的过程，确认最终镜像没有 Go 编译器 |
| 2 | `docker-compose.yml` 的 `mysql`、`redis`、`logic`、`gateway-a`、`gateway-b`、`transfer` service | 对每个 service 写 command、内部服务名地址、宿主机端口和依赖；注意本地 MySQL root 只用于演示 |
| 3 | `Makefile` 的 `fmt-check`、`vet`、`test`、`test-race`、`build`、`compose-config`、`docs-check`、`frontend-static-check` | 把每个 target 展开成实际命令，不把 Make 当成隐藏的自动魔法 |
| 4 | `.github/workflows/ci.yml` 的 trigger、`test-build`、`docker-build`、`manifest-check` | 找到 `needs`、独立 runner、SHA 镜像标签、`push:false` 和 K8s render |
| 5 | `scripts/validate_frontend.py`、`scripts/validate_docs.py`、`scripts/validate_k8s.sh` | 每个脚本只读入口和失败条件，写出它检查什么、漏掉什么 |

看到这个程度就停：你能从一次 commit/push 讲到 runner 拉取该 SHA、执行 vet/test/race/build、构建但不推送镜像、渲染清单；也能解释容器以 10001 而非 root 运行。暂时不必学习 Docker overlayfs、镜像 registry 实现和 GitHub Actions 自建 runner 运维。

## 动手练习

按顺序执行：

```bash
make test
make build
make compose-config
make frontend-static-check
make k8s-check
```

逐项写出“它证明什么、不证明什么”。

## 闭卷检查

1. 镜像和容器是什么关系？
2. Compose 为什么不是生产多服务器证明？
3. CI 与 CD 有什么区别？
4. 为什么发布镜像应使用 commit SHA？

## 动手练习与闭卷检查参考答案

### 动手练习答案

1. `make test` 证明当前 Go 单元/包测试通过，不证明没写到的并发和真实依赖场景。
2. `make build` 证明三个 main 及依赖可编译，不证明二进制启动后依赖可用。
3. `make compose-config` 证明 Compose 变量替换和 YAML 合并可解析，不会启动容器，也不证明网络链路正确。
4. `make frontend-static-check` 证明页面关键控件、函数和接口契约没有明显漂移，不是真实浏览器端到端测试。
5. `make k8s-check` 运行仓库的 K8s 规则和渲染检查，证明清单结构可合并；它不创建集群、不拉镜像，也不证明云 LB、存储和 Secret 已准备好。

CI 还单独执行 `make vet` 与 `make test-race`。vet 是静态可疑用法检查，race 只在测试实际执行到的并发路径上探测数据竞争，都不能替代业务正确性测试。

### 闭卷检查答案

1. 镜像是只读运行模板，容器是该镜像的一次运行实例；同一镜像可启动多个容器。
2. Compose 在一台开发机的 Docker 网络中复现多组件，单节点依赖、宿主机端口和演示凭据都不是多服务器生产高可用证据。
3. CI 自动检查和构建一个明确 commit；CD 把通过检查的制品发布到环境并处理验证、回滚。当前 Actions 不推镜像，仓库只有发布脚本示例。
4. SHA 标签不可变地对应源码和 CI 结果，运行版本可追溯，回滚能指向上一明确镜像；`latest` 会不断改变指向。

下一步：[18 Kubernetes 多服务器部署](18_KUBERNETES_DEPLOYMENT.md)
