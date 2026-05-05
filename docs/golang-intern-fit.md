# Golang 实习岗位对齐说明

这份说明用于对应“Golang 开发工程师实习”岗位，重点不是包装成大型公司项目，而是说明项目里真实做过哪些后端、框架、中间件、容器化和文档工作。

## 和岗位要求的对应关系

- Golang 后端开发：项目主体使用 Go 编写，包含 Gateway、Logic、Transfer 三个后端进程。
- 技术方案设计：按接入层、业务层、异步投递层拆分，分别处理 WebSocket 长连接、消息编排、Kafka 群聊扩散。
- Golang 框架使用：Gateway 使用 go-zero REST 结构，Logic 使用 go-zero zRPC 结构，配置、依赖注入、日志输出保持统一。
- 中间件使用：使用 Redis 管理在线路由、ACK、离线补偿；使用 Kafka 解耦群聊扩散；使用 Etcd 做 Logic 服务发现；使用 MySQL 存储历史消息。
- 容器化使用：通过 Dockerfile 构建三个 Go 服务，通过 docker-compose 一键启动 Redis、MySQL、Kafka、Etcd、Gateway、Logic、Transfer，并增加 healthcheck 辅助本地排查。
- Kubernetes 使用：补充 Gateway、Logic、Transfer 的 Deployment / Service / Probe / Resource / PDB 清单，能够说明服务副本、滚动发布、健康检查和配置注入的基础用法。
- CI/CD 使用：通过 GitHub Actions 自动执行 Go 测试、服务构建、Docker 镜像构建和 K8s 清单检查，形成提交后的基础质量门禁。
- 测试与可维护性：补充配置覆盖、健康检查、基础工具函数等单元测试；统一异常日志到 go-zero logx，方便定位链路问题。
- 技术文档：README、模块 README、压测报告、学习路线和面试问答文档用于说明系统结构、启动方式和核心链路。

## 可以在面试中强调的点

1. 这个项目不是只写接口，而是围绕 IM 消息链路做了分层设计。
2. go-zero 主要承担工程骨架、配置、API/RPC 分层和日志规范，核心业务设计仍然由自己实现。
3. Redis、Kafka、Etcd 分别解决在线状态、异步扩散、服务发现问题，不是为了堆技术栈。
4. Docker Compose 用于本地复现完整后端环境，K8s 清单用于说明服务部署、探针和资源限制。
5. CI/CD 不直接连生产集群，当前只做测试、构建、镜像构建和清单检查，口径更适合个人项目。
