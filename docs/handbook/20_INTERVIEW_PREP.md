# 20 面试讲述与逐层追问

## 本章前置

只有当你能独立画出第 19 章链路时才读本章。本章负责压缩表达，不负责替代学习。

## 本章目标

能用 20 秒、1 分钟和 3 分钟介绍项目；能在面试官追问时从架构进入代码和失败路径；不夸大未完成能力。

## 20 秒版本

> LinkGo 是我使用 Go 和 go-zero 实现的分布式 IM 项目，采用 Gateway、Logic、Transfer 分层。单聊通过 MySQL 持久化、Redis 在线路由和跨 Gateway 通知完成实时投递，并用 client_msg_id、会话 seq、ACK 和短期重连补偿处理重复、顺序与断线；群聊使用 Kafka 和 Transfer 解耦逐成员投递，业务上还实现了事务红包和 AI 虚拟好友。

## 1 分钟版本

> 项目默认按多服务器场景设计：A、B 可以连接不同 Gateway，每台 Gateway 只保存本机 WebSocket，在线路由放在共享 Redis。Gateway 通过 Etcd 发现 Logic 集群并使用 p2c_ewma 调用。A 发单聊后，Logic 做好友权限和 client_msg_id 幂等，用 Redis Lua 生成会话 seq，同步写入 MySQL，再查询 B 的 route，把实时通知发布到 B 所在 Gateway；B 收到后返回 ACK。Redis Pub/Sub 只负责低延迟通知，当前断线自动恢复依赖 Redis pending 和单会话 timeline，MySQL 历史通过独立接口查询。群聊由 Logic 解析成员后写 Kafka，Transfer 逐收件人投递并处理 lease 幂等、retry 和 DLQ。红包使用 MySQL 事务、行锁和唯一索引，AI 作为普通账号异步回复。

## 3 分钟顺序

不要随机列功能，固定按下面讲：

1. 20 秒定位和多服务器拓扑。
2. 30 秒三个服务职责。
3. 60 秒 A 在 Gateway-1、B 在 Gateway-3 的单聊链路。
4. 30 秒可靠性：ID、seq、ACK、当前重连边界。
5. 30 秒群聊 Kafka 与 Transfer。
6. 20 秒红包和 AI。
7. 10 秒验证与真实性边界。

## 简历三条

- 基于 Go/go-zero 实现 Gateway、Logic、Transfer 分层 IM；Logic 通过 Etcd 注册，Gateway 使用 zRPC `p2c_ewma` 选择实例，并利用共享 Redis 在线路由和定向 Pub/Sub 完成跨 Gateway 实时投递。
- 使用 `client_msg_id` Redis/MySQL 双层幂等、Redis Lua 会话 seq、MySQL 同步消息落库、客户端 ACK 和短期 pending/timeline 构建可重试、可短期恢复的投递链路，并以 UID 分片有界队列暴露背压；当前 MySQL 提交到首次投递之间仍有未使用 Outbox 的故障窗口。
- 通过 Kafka/Transfer 解耦群聊逐成员投递，实现成员级 lease 幂等、retry/DLQ 和手动位点提交；补充 MySQL 事务红包、AI 虚拟好友、Prometheus 指标及 Docker/Kubernetes 验证清单。

不要在没有重新压测报告时写具体连接数、QPS 或 P99。

## 面试官的追问树

### 第一层：用户链路

- A、B 不在一台 Gateway，消息怎样传？
- B 离线时发生什么？
- 群聊为什么不和单聊完全相同？

### 第二层：技术选择

- Redis Pub/Sub 会丢通知，为什么还用？
- 为什么消息放 MySQL，不增加 MongoDB？
- 为什么群聊使用 Kafka？
- Etcd 和 p2c_ewma 分别解决什么？

### 第三层：一致性和故障

- MySQL 写成功、发布 Redis 前 Logic 宕机怎么办？
- 消息已到 B 但 ACK 丢了怎么办？
- Kafka 投递成功、提交位点前宕机怎么办？
- Redis、Logic、Transfer 分别宕机会影响什么？

### 第四层：代码所有权

- `WireMessage` 有哪些关键字段？
- `ClientManager` 保存什么？
- `RedisDelivery.Deliver` 写哪些状态？
- 群成员在哪里解析？
- 红包事务在哪里加锁？

## 必须诚实回答的缺口

### MySQL 提交到实时通知的窗口

当前没有完整消息 Outbox。如果 MySQL 消息已提交、Logic 在 RedisDelivery 前宕机，客户端不会立即收到实时通知，需要主动历史查询才能发现。演进方案是消息事务 Outbox，但不能说已经实现。

### 离线同步

当前不是微信式“自动扫描所有会话并从 MySQL 按游标完整同步”。重连自动使用 Redis 短期状态，历史 HTTP 接口固定最近 50 条。

### 多端登录

当前 route 和本机 uid 连接都是单值，不是同账号手机、电脑多端并存模型。

### 群规模

Logic 仍同步解析完整 recipients，所以 Kafka 只解耦逐成员投递，不能说发送请求成本与群规模无关。

### Redis/MySQL 集群

当前连接稳定单入口；高可用代理可以位于入口后，但代码不原生支持 Redis Cluster/Sentinel 和 MySQL 应用层读写分离。

### 前端和 K8s

网页是调试客户端；production K8s 是可渲染示例，没有真实生产集群、Ingress/TLS 或前端 Deployment。

## 不知道答案时怎样处理

不要猜结构体或制造生产数据。可以说：

> 我先区分当前实现和演进方案。当前代码在某文件完成某步骤，边界是某故障窗口；如果继续生产化，我会先通过某指标验证，再引入某方案。

## 模拟面试评分

每次总分 100：

```text
架构与多机链路       20
单聊和可靠性         25
Kafka 群聊           15
MySQL/Redis          15
红包/AI              10
工程化与故障         10
真实性               5
```

低于 70 回到对应教材；70–84 继续源码定位；85 以上再做压力追问。

## 闭卷检查

1. 不看资料完成 1 分钟介绍。
2. 解释 MySQL 成功、Redis 通知前宕机的当前结果。
3. 说出三个绝对不能夸大的边界。
4. 从 `WireMessage` 讲到 B 的 ACK，至少指出五个代码位置。

下一步：[21 学习检查表](21_CHECKLIST.md)
