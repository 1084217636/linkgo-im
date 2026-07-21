# 15 分层面试题库

## 第一层：必须秒答

1. 项目是什么？2. 三服务职责？3. Redis/MySQL/Kafka 各做什么？4. 单聊链路？5. 群聊链路？6. ACK 是什么？7. client_msg_id 与 message_id？8. 为什么 Kafka？9. 为什么 Outbox？10. K8s readiness 是什么？

## 第二层：设计追问

1. 为什么 Gateway 有状态？2. 为什么 UID 分片？3. 为什么有界队列？4. Redis 和 DB 双幂等？5. pending/offline/timeline 区别？6. Kafka 提交前宕机？7. lease 为什么可恢复？8. 行锁和唯一索引？9. RBAC 如何分工？10. CI 通过说明什么、不说明什么？

## 第三层：故障追问

1. Redis 宕机。2. Logic 宕机。3. Transfer 宕机。4. Kafka 重复。5. ACK 丢失。6. DB 提交后响应丢失。7. Outbox 同步失败。8. 队列满。9. Gateway 重启。10. 慢 SQL。

## 第四层：代码所有权

1. 说 12 个结构体。2. WS 从哪个入口？3. Logic ServiceContext 有什么？4. Transfer 主循环在哪？5. ACK 清理哪些 key？6. 发布事务写哪些表？7. Grant 幂等键？8. 指标在哪里定义？9. readiness 检查什么？10. CI workflow 有哪些 job？

## 第五层：反思

1. 最难的 bug？2. 方案代价？3. 如果重做？4. 为什么没上 Mongo/ES？5. 如何扩到十万连接？6. 如何多机房？7. 哪些只是本地验证？8. AI 生成代码如何保证所有权？9. 项目最大不足？10. 上线前还要什么？

每题按“一句话、链路、理由、边界”回答。答不上就在对应专题补档，不靠临场编造。
