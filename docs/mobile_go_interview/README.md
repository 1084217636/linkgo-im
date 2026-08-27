# LinkGo 手机背诵手册

这套手册只做一件事：让你在没有电脑、不能打开源码时，也能把 Go 基础、IM 架构和项目取舍连起来讲清楚。

它和 `docs/handbook` 的用途不同。`handbook` 用来第一次读代码，这里用来直接背面试回答。正文不会让你再去提问，也不会把答案留到别处。

## 阅读顺序

1. [Go 语言与并发](01_GO_AND_CONCURRENCY.md)
2. [C/S 网络、HTTP、WebSocket 与 gRPC](02_CS_NETWORK.md)
3. [LinkGo 服务分层与登录建连](03_LINKGO_ARCHITECTURE.md)
4. [跨 Gateway 单聊与消息可靠性](04_CHAT_AND_RELIABILITY.md)
5. [MySQL、Redis、Kafka 与 Etcd](05_DATA_AND_MIDDLEWARE.md)
6. [故障处理、扩容与一致性边界](06_FAILURE_AND_SCALING.md)
7. [Docker、CI、Kubernetes 与测试](07_ENGINEERING_AND_TESTING.md)
8. [项目介绍、技术选型与高频追问](08_PROJECT_STORY.md)
9. [上场前快速复述卡](09_LAST_MINUTE_CARDS.md)

## 每道题怎么背

每道题都有六段：

1. `面试官问`：真实面试问题。
2. `30 秒回答`：面试官没有继续追问时说这一段。
3. `详细回答`：面试官感兴趣后再展开。
4. `结合 LinkGo`：把八股落到当前代码。
5. `为什么这样选，替代方案是什么`：说出判断，不只报技术名。
6. `边界与追问`：主动控制表述范围，并准备下一层问题。

走路时先只背每题的 `30 秒回答`。第二遍再背 `结合 LinkGo`。最后才练详细回答和替代方案。

## 项目事实边界

这份手册只采用当前源码、测试和部署清单能证明的事实：

- Gateway、Logic、Transfer 可以多实例运行。
- Logic 通过 Etcd 注册与发现，Gateway 的 gRPC 客户端配置 `p2c_ewma`。
- Redis 保存在线路由、短期 ACK 补偿数据，并承担 Gateway 定向 Pub/Sub。
- MySQL 保存消息历史和关系数据。
- Kafka 用于群消息异步扩散，Transfer 处理成员级投递、retry 和 DLQ。
- 仓库有 Docker、Compose、GitHub Actions、Kubernetes 清单和故障演练脚本。

下面这些不能说成已经完成：

- 真实公司生产流量或跨机房部署。
- Redis Cluster、MySQL 主从自动切换或 Kafka 生产集群运维。
- WebSocket 无损迁移。
- 全会话、全设备、永不丢失的商业级同步。
- GitHub Actions 自动把镜像推到生产并完成无人值守发布。

面试中主动说清边界不会减分。把本地原型说成生产系统，反而容易在追问中失去可信度。

