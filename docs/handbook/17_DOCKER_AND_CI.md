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

## Dockerfile 做什么

LinkGo 的 Dockerfile 先在构建阶段编译 Go 二进制，再把运行所需文件放入更小的运行镜像。多阶段构建能减少最终镜像体积和不必要工具。

阅读时只问：

1. 基础镜像是什么？
2. 在哪里执行 `go build`？
3. 最终复制了哪些二进制和配置？
4. 容器启动命令是什么？

## Docker Compose 做什么

Compose 用一个 YAML 描述本地需要启动的多个容器及网络：

```text
MySQL
Redis
Etcd
Kafka
Gateway
Logic
Transfer
```

它的目标是“在一台开发机快速复现多组件环境”，不是生产集群证明。

常用命令由 Makefile 包装：

```bash
make docker-up
make docker-down
make compose-config
```

先用 `compose-config` 检查 YAML 渲染，再真正启动。

## CI 是什么

CI 是 Continuous Integration，持续集成。代码 push 到 GitHub 后，由独立机器自动执行检查，避免“只在我的电脑通过”。

LinkGo 的 GitHub Actions 主要验证：

```text
格式
→ Go 测试
→ 服务构建
→ 前端静态契约
→ Compose 配置
→ Prometheus 配置
→ Docker 镜像
→ Kubernetes 清单渲染
```

CI 通过只能证明这些步骤通过，不能证明真实生产流量、跨机房容灾或绝对无故障。

## CI/CD 不要混淆

- CI：自动检查和构建。
- CD：把通过验证的版本发布到环境。

当前仓库有发布脚本和 K8s 清单，可以在受控练习环境验证滚动发布与回滚；没有理由声称已经部署到真实公司生产集群。尤其要注意：`scripts/k8s_release.sh` 固定应用 `deploy/k8s` 本地 demo base，会一并部署单节点依赖；它还没有参数切换到 app-only 的 production overlay。

## 为什么镜像标签不能用 latest

`latest` 不能明确对应哪次代码，回滚和审计困难。发布应使用不可变版本，例如 commit SHA：

```text
ghcr.io/example/linkgo-im:530d3e7
```

## 代码锚点

- `Dockerfile`
- `docker-compose.yml`
- `Makefile`
- `.github/workflows/ci.yml`
- `scripts/validate_frontend.py`
- `scripts/validate_k8s.sh`

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

下一步：[18 Kubernetes 多服务器部署](18_KUBERNETES_DEPLOYMENT.md)
