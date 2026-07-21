# 14 简历表达与项目讲述

## 简历三条

- 基于 Go/go-zero 设计多 Gateway、Logic、Transfer 集群化 IM，使用 Etcd 服务发现与 p2c_ewma 调度 Logic，通过共享 Redis 在线路由及 MySQL 最终历史实现跨 Gateway 至少一次投递和断线补偿。
- 使用 Kafka 解耦群聊 fanout，采用手动位点提交、retry/DLQ 和 message+recipient lease 幂等处理重复消费与消费者宕机恢复。
- 将并发红包与 AI 虚拟好友接入 IM：红包使用 MySQL 事务、行锁和唯一索引处理超卖与重复领取；AI 支持总结、问答、超时降级和调用审计。

## 三分钟讲述顺序

1. 20 秒定位。
2. 40 秒三服务和数据组件。
3. 60 秒单聊可靠性。
4. 40 秒 Kafka 群聊。
5. 30 秒红包与 AI 差异化。
6. 20 秒工程化与边界。

第二步必须主动设定场景：A 在 Gateway-1，B 在 Gateway-3，Gateway-1 经 Etcd 选择 Logic-2，Logic 再通过共享 Redis 定向通知 Gateway-3。不要等面试官提醒你“它们不在同一台服务器”。

## 个人贡献

不要说“我用了很多组件”。说“我解决了什么”：

- 修复密码、Origin、历史越权。
- UID 分片有界队列和客户端背压。
- Kafka Fetch/Commit、retry/DLQ、lease 幂等。
- 补指标告警、K8s 发布回滚、故障注入和 CI。

## STAR 模板

场景：群聊同步扩散阻塞 Logic。任务：把扩散做成可恢复异步链路。行动：Kafka+Transfer、手动提交、成员 lease、retry/DLQ、指标。结果：主链路与 fanout 解耦，并能解释重复消费和宕机恢复；结果以测试/CI 为证，不虚构生产规模。

## 避免用词

避免“完全不丢、绝对有序、百万并发、生产级、真正支付、完整 RAG”。改为“至少一次、会话 seq、设计并验证、演示环境、事务红包、轻量 FAQ/RAG”。
