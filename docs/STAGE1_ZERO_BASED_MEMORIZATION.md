# 第一阶段零基础背诵手册：先建立全局地图

这份资料写给“只听过技术名字，还不能独立解释项目”的学习者。第一阶段不要求读源码，不要求会写 K8s YAML，也不要求解释 Kafka 位点。目标只有三个：

1. 能用 1 分钟介绍项目。
2. 能画出系统整体结构和三条核心链路。
3. 听到一个技术名词，能说出它在本项目负责什么。

## 一、先背项目定位

### 20 秒版本

> LinkGo 是一个使用 Go 和 go-zero 开发的实时通信与游戏运营平台。系统拆成 Gateway、Logic、Transfer 三个服务，使用 MySQL 保存最终业务数据，Redis 保存在线状态和待确认消息，Kafka 处理群聊异步扩散，并通过 Docker、K8s、GitHub Actions、Prometheus 完成部署和监控闭环。

### 60 秒版本

> LinkGo 的主体是一个 Go 分布式 IM。Gateway 负责登录接口、JWT、WebSocket 长连接和 ACK；Logic 负责消息校验、生成顺序号、幂等和 MySQL 落库；Transfer 消费 Kafka，异步完成群消息的成员投递。MySQL 是最终事实来源，Redis 保存在线路由、待确认消息和离线补偿索引，Kafka 用于把耗时的群聊扩散从主链路拆出去。项目还实现了并发红包、AI 群聊总结和一条游戏运营控制链路，包括活动版本、审核、灰度发布、道具发放、审计与回滚。工程层使用 GitHub Actions 自动测试构建，Docker 打包程序，K8s 负责运行、探活、滚动发布和失败回滚，Prometheus/Grafana 负责指标与告警。

先每天朗读三遍。现阶段不要擅自增加“百万连接”“生产落地”“完全不丢消息”等代码没有证明的说法。

## 二、只记住这张系统地图

```text
浏览器 / App
    |
    | HTTP 登录、WebSocket 消息
    v
Gateway（接待员）
    |
    | gRPC 内部调用
    v
Logic（业务大脑）
    |             |
    |             | 群聊任务
    v             v
MySQL / Redis   Kafka（任务传送带）
                  |
                  v
              Transfer（群聊派送员）
                  |
                  v
                Redis
                  |
                  v
             目标 Gateway -> 接收方
```

把三个服务先记成人：

- Gateway 是接待员：面对客户端，管理连接。
- Logic 是业务大脑：决定消息怎么处理和保存。
- Transfer 是群聊派送员：从 Kafka 领取任务后给群成员投递。

## 三、三个服务必须背会

### Gateway

关键词：`HTTP、JWT、WebSocket、连接、ACK、限流`。

背诵句：

> Gateway 是客户端入口，负责登录等 HTTP API、JWT 鉴权、WebSocket 长连接、消息收发、ACK、断线补偿、限流和在线连接管理。它不承担复杂消息业务，而是把消息交给 Logic。

### Logic

关键词：`业务、幂等、seq、落库、路由、Kafka 生产者`。

背诵句：

> Logic 是核心业务层，负责校验消息、根据 client_msg_id 防止重复发送、生成会话递增 seq、写入 MySQL，并决定单聊直接投递还是群聊写入 Kafka。

### Transfer

关键词：`Kafka 消费者、群聊扩散、retry、DLQ、幂等`。

背诵句：

> Transfer 是群聊异步消费者，从 Kafka 读取群消息任务，再向每个群成员投递。失败任务进入重试或死信链路，处理成功后才提交 Kafka 位点。

### 三秒自测

- 谁管 WebSocket？Gateway。
- 谁写核心消息业务？Logic。
- 谁消费 Kafka？Transfer。

如果这三题不能立即回答，先不要往下学。

## 四、四个存储和基础设施组件

### MySQL：长期档案柜

本项目保存：用户、消息历史、会话、群成员、红包、活动版本、道具和审计日志。

背诵句：

> MySQL 是最终事实来源，适合保存需要长期存在、支持事务和关系查询的业务数据。

### Redis：高速临时状态板

本项目保存：用户在哪个 Gateway、待 ACK 消息、离线索引、会话 seq、热点消息和缓存。

背诵句：

> Redis 速度快，用来保存在线路由和消息投递过程中的临时状态；Redis 不是最终聊天历史库，重要历史最终保存在 MySQL。

### Kafka：异步任务传送带

本项目主要处理群聊扩散。

背诵句：

> Kafka 把“Logic 接收群消息”和“Transfer 给所有群成员派送”解耦，避免大群同步循环拖慢主链路，并提供消费、重试和削峰能力。

### Etcd：服务通讯录

背诵句：

> Etcd 用于服务发现，让 Gateway 能找到 Logic 实例；它不是业务数据库。

### 最容易混淆的一组答案

| 问题 | 答案 |
| --- | --- |
| 聊天历史放哪里？ | MySQL |
| 用户当前在线位置放哪里？ | Redis |
| 群聊异步任务放哪里？ | Kafka |
| Gateway 去哪里发现 Logic？ | Etcd |

## 五、先背三条业务链路

### 1. 登录和建连

```text
用户提交账号密码
-> Gateway HTTP 登录接口
-> Logic 查询 MySQL 验证账号
-> 返回 JWT
-> 客户端携带 JWT 建立 WebSocket
-> Gateway 把用户在线位置写入 Redis
```

一句话：

> 用户先登录获得 JWT，再用 JWT 建立 WebSocket；建连后 Gateway 把用户到网关的映射保存到 Redis。

### 2. 单聊

```text
A 客户端
-> Gateway A
-> Logic
-> MySQL 保存消息
-> Redis 查询 B 在哪个 Gateway
-> Gateway B
-> B 客户端
-> B 返回 ACK
-> 服务端清理 pending
```

一句话：

> 单聊先由 Logic 做幂等、顺序号和落库，再根据 Redis 在线路由推送到接收方；接收方 ACK 后才清理待确认状态。

### 3. 群聊

```text
A 客户端
-> Gateway
-> Logic 落库
-> Kafka
-> Transfer 消费
-> 查询群成员
-> 逐成员幂等投递
-> Redis / 各 Gateway
-> 群成员客户端
```

一句话：

> 群聊与单聊最大的区别，是成员扩散交给 Kafka 和 Transfer 异步完成，避免阻塞 Logic。

## 六、ACK 和离线补偿先这样理解

不要一开始背 Redis Key，先理解问题：服务端调用 WebSocket 写成功，不代表客户端一定处理成功。

```text
服务端准备投递
-> 先记录 pending_ack
-> 推送给客户端
-> 客户端收到后发送 ACK
-> 服务端删除 pending
```

如果客户端中途断线：

```text
pending 还在
-> 用户重新连接
-> Gateway 回放未确认消息
```

背诵句：

> 项目中的 ACK 是客户端收件确认，不是用户已读回执。消息在收到 ACK 前保留 pending，断线重连后可以继续补偿。

## 七、红包、AI 和游戏运营只背定位

### 红包

> 红包用于展示 MySQL 事务、行锁和唯一索引解决并发超卖与重复领取，不是支付系统。

### AI

> AI 是 IM 上层增强，提供群聊总结和知识问答；它有超时、降级和审计，不放进实时消息关键链路阻塞聊天。

### 游戏运营控制面

```text
operator 创建草稿并提交
-> reviewer 审核
-> admin 灰度发布
-> Redis 配置生效
-> 批量发放道具
-> 审计
-> 必要时恢复历史版本
```

> 这条链路证明项目不只是聊天 CRUD，还包含游戏运营中的配置版本、职责分离、幂等发放、审计和回滚。

## 八、Docker、CI/CD、K8s 先背关系

### Docker 是什么

> Docker 把程序、运行环境和依赖打包成镜像，让同一个程序在不同机器上以接近一致的方式运行。

记忆：Docker 像“装好应用的标准集装箱”。

### CI/CD 是什么

- CI：代码提交后自动检查格式、运行测试、构建程序。
- CD：把验证通过的版本发布到运行环境。

本项目 GitHub Actions 当前完成：

```text
push 代码
-> Go 测试
-> 构建三个服务
-> 检查前端契约
-> 检查 Compose / Prometheus
-> 构建 Docker 镜像
-> 检查 K8s 清单
```

背诵句：

> GitHub Actions 是自动化流水线。每次 push 后自动测试和构建，避免只在开发者电脑上“看起来能跑”。

### K8s 是什么

> Kubernetes 用于管理多个容器实例，负责期望副本数、服务发现、健康检查、滚动更新、故障重启和回滚。

先记六个词：

| 名词 | 零基础解释 |
| --- | --- |
| Pod | K8s 中运行容器的基本单位 |
| Deployment | 管理一组 Pod 和版本更新 |
| Service | 给一组 Pod 提供稳定访问地址 |
| ConfigMap | 保存非敏感配置 |
| Secret | 保存密码、Token 等敏感配置 |
| Probe | 检查程序是否活着、是否可以接流量 |

### 一次发布怎么走

```text
代码 push
-> GitHub Actions 测试
-> 构建带 Git SHA 的 Docker 镜像
-> K8s 更新 Deployment 镜像
-> 创建新 Pod
-> readiness 通过
-> 旧 Pod 逐步退出
-> smoke test
-> 失败则 rollout undo
```

背诵句：

> 项目使用不可变版本镜像进行 K8s 滚动发布，新 Pod readiness 通过后才接流量；发布后 smoke test 失败会执行 rollout undo。当前是在本地和 CI 验证清单与脚本，没有声称运行在真实生产集群。

## 九、监控先记三个问题

Prometheus 负责采集数字指标，Grafana 负责把指标画成图，告警规则负责在异常时提示。

看监控时先问：

1. 服务活着吗？
2. 请求是否变慢或失败？
3. 队列是否堆积或拒绝？

背诵句：

> Gateway 和 Transfer 暴露 `/metrics`，Prometheus 定时抓取，Grafana 展示，告警规则关注服务不可用、队列背压、Kafka 失败和运营缓存同步失败。

## 十、第一阶段禁止钻研的内容

暂时不要深入：

- Redis Lua 脚本每一行。
- Kafka rebalance 和位点协议细节。
- K8s 调度器和网络插件原理。
- Prometheus PromQL 复杂语法。
- go-zero、gRPC 源码。
- 具体结构体的每个字段。

这些会在第二、第三阶段学习。现在只要会解释“谁负责什么、数据怎么流动、为什么引入”。

## 十一、七天背诵计划

### 第 1 天：项目定位和三个服务

背 20 秒、60 秒介绍；默写 Gateway、Logic、Transfer 职责。

### 第 2 天：四个组件

默写 MySQL、Redis、Kafka、Etcd 的作用和区别。

### 第 3 天：登录与单聊

不看资料画两条链路，口述 ACK 的意义。

### 第 4 天：群聊

背会为什么引入 Kafka、为什么需要 Transfer。

### 第 5 天：红包、AI、游戏运营

每个功能只准备一句定位和一条链路。

### 第 6 天：Docker、CI/CD、K8s

背六个 K8s 名词，口述一次发布过程。

### 第 7 天：完整复述

完成 1 分钟介绍、整体架构、三条链路和下面 20 道题。

## 十二、第一阶段 20 道闭卷题

1. LinkGo 一句话定位是什么？
2. 为什么拆 Gateway、Logic、Transfer？
3. 谁维护 WebSocket？
4. 谁生成消息 seq 并落库？
5. 谁消费群聊 Kafka 消息？
6. MySQL 保存什么？
7. Redis 保存什么？
8. 为什么 Redis 不是最终历史库？
9. Kafka 在群聊中解决什么问题？
10. Etcd 用来做什么？
11. 登录后为什么还要建立 WebSocket？
12. 单聊从 A 到 B 经过哪些核心组件？
13. 群聊和单聊最大的链路区别是什么？
14. ACK 表示已读吗？
15. 客户端断线后如何补偿？
16. 红包主要证明什么技术能力？
17. AI 为什么不应阻塞实时消息链路？
18. CI 和 CD 分别是什么？
19. Docker 和 K8s 的关系是什么？
20. readiness 失败时为什么不能接收流量？

答题标准：每题先用一句话回答，暂时不要展开超过 30 秒。

## 十三、完成标准

满足以下条件再进入第二阶段：

- 不看资料讲完 60 秒项目介绍。
- 5 分钟画出整体结构。
- 说清登录、单聊、群聊三条链路。
- 20 道题至少答对 16 道。
- 不再混淆 MySQL、Redis、Kafka、Etcd。
- 能用自己的话解释 Docker、CI/CD 和 K8s 的关系。

第二阶段才开始学习核心名词、Redis Key、MySQL 表、Kafka 可靠消费和常用 K8s YAML。
