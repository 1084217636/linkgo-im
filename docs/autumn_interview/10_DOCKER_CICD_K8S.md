# 10 Docker、CI/CD 与 Kubernetes

## 先背三句话

- Docker：把程序和运行环境打成可重复运行的镜像。
- CI/CD：代码提交后自动测试、构建和发布的流水线。
- K8s：在多台机器上管理容器副本、网络、健康、更新和恢复。

## 从代码到运行

```text
git push
-> GitHub Actions checkout
-> gofmt / go test / go build
-> 前端、Compose、Prometheus、K8s 契约检查
-> Docker build 带 git SHA 镜像
-> K8s Deployment 更新 image
-> 新 Pod 启动
-> readiness 通过后 Service 导流
-> 旧 Pod 退出
-> smoke test
-> 失败 rollout undo
```

## Docker 必懂

Dockerfile 描述如何构建镜像；image 是只读模板；container 是镜像的运行实例。项目使用多阶段构建：Go builder 编译二进制，runtime 镜像只携带运行需要的文件，减小体积和攻击面。

Docker Compose 用于本地一次启动 MySQL、Redis、Etcd、Kafka、Logic、Transfer 和多个 Gateway；它是本地编排，不等于生产 K8s。

镜像不能只用 `latest`：无法确定具体代码，也不利于回滚。项目发布脚本要求 Git SHA 等不可变标签。

## GitHub Actions 必懂

Workflow 是 YAML 流水线；job 是可并行/依赖的任务；step 是 job 内步骤；runner 是执行机器；artifact 是输出物；secret 是受保护变量。

当前 CI 包含：测试构建、Docker 镜像构建、K8s 清单渲染。CI 通过说明仓库声明的检查通过，不等于真实生产流量验证。

失败处理：先看最新 head SHA，区分旧任务取消和真实失败；定位失败 job/step；读日志第一处根因；本地复现同命令；小范围修复；push 后只以最新 CI 为准。

## K8s 六个对象

- Pod：运行容器的最小调度单位，Pod 可被替换，不能当永久机器。
- Deployment：声明副本数、镜像和滚动更新策略。
- Service：为一组动态 Pod 提供稳定访问地址和负载均衡。
- ConfigMap：非敏感配置。
- Secret：密码、Token、DSN 等敏感配置。
- HPA/PDB：按指标扩缩容；维护时保证最少可用副本。

## Probe

- liveness：程序是否活着；失败时重启容器。
- readiness：程序是否准备接流量；失败时从 Service endpoints 移除。

Gateway readiness 检查 Logic、Redis、MySQL；Transfer 检查 Redis、Kafka。不要把 liveness 写成深度依赖检查，否则依赖短暂波动会造成重启风暴。

## 滚动发布

Gateway/Logic 使用 `maxUnavailable: 0, maxSurge: 1`：先多创建一个新 Pod，通过 readiness 后再减少旧 Pod。PDB 和 topology spread 提高维护/节点故障下的可用性。

项目脚本依次更新 Gateway、Logic、Transfer，等待 rollout，端口转发访问 `/readyz`。任一步失败，对已更新 Deployment 执行 `rollout undo`。

## 配置边界

ConfigMap 放地址、Topic、超时；Secret 放 Redis 密码、JWT Secret、数据库 DSN。仓库演示 Secret 不是生产秘密；生产应由 CI Secret、External Secrets、SealedSecret 或 KMS 注入。

## 标准回答

> 我没有把“写了 YAML”说成生产运维。项目在 CI 中验证镜像构建和 K8s 清单，发布脚本实现不可变镜像、滚动等待、readiness smoke 和失败回滚；真实集群容量、证书、云存储和高可用依赖仍需环境验证。

## 闭卷题

1. image 和 container 区别？2. Compose 与 K8s 区别？3. CI 与 CD 区别？4. 为什么不用 latest？5. Pod、Deployment、Service 关系？6. readiness/liveness 区别？7. 滚动发布如何避免中断？8. ConfigMap/Secret 区别？9. CI 失败怎么查？10. 当前 K8s 的真实边界？
