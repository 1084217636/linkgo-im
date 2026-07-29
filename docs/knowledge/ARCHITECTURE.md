# LinkGo 当前架构事实

## 服务

- Gateway：HTTP、WebSocket、本机连接、ACK、推送队列；部分好友、群、红包和 AI HTTP 接口当前也直接访问 MySQL。
- Logic：登录、历史和 WebSocket 消息的 gRPC 处理，消息 ID/seq、同步消息落库、单聊投递和群聊任务生产。
- Transfer：消费已带完整 recipients 的 Kafka 群任务，逐收件人投递，处理 retry/DLQ 和成员级 lease 幂等。

## 多实例

Logic Pod 把 Pod IP 注册到 Etcd `/services/logic`。Gateway 未配置 `LOGIC_ADDR` 时取得实例列表，并由 go-zero zRPC `p2c_ewma` 选择。当前主链路不是一致性哈希。

客户端 A、B 可以位于不同 Gateway。每台 Gateway 只持有本机 WebSocket；共享 Redis 的 `route:<uid>` 指出目标用户当前 Gateway，定向 Pub/Sub 频道把在线通知送到目标 Gateway。

## 存储

- MySQL：消息最终历史、关系数据、红包和 AI 记录。
- Redis：在线路由、会话 seq、短期 pending/ack payload/timeline 和热点索引。
- Kafka：群聊逐成员投递任务的异步缓冲、重试与死信链路。

当前应用通过单个 Redis 稳定地址和单个 MySQL DSN 接入，不原生实现 Redis Cluster/Sentinel 客户端或 MySQL 应用层读写分离。production K8s 目录是可渲染示例，不是实际生产上线证明。
