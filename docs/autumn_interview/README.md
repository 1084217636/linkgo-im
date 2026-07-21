# LinkGo 秋招主项目背诵面经

这套材料不是源码说明书，而是“从零基础到能经受项目追问”的学习主线。先背结论，再理解链路，最后定位到代码和故障证据。

## 学习顺序

### 第一轮：只背整体

1. `01_PROJECT_AND_ARCHITECTURE.md`
2. `02_LOGIN_JWT_WEBSOCKET.md`
3. `03_SINGLE_CHAT_RELIABILITY.md`
4. `04_GROUP_CHAT_KAFKA.md`
5. `05_MYSQL_REDIS_DATA_MODEL.md`

### 第二轮：背差异化业务

6. `06_RED_PACKET_AND_CONSISTENCY.md`
7. `07_GAMEOPS_CONTROL_PLANE.md`
8. `08_AI_INTEGRATION.md`
9. `09_SECURITY_AND_AUTHORIZATION.md`

### 第三轮：背工程化

10. `10_DOCKER_CICD_K8S.md`
11. `11_OBSERVABILITY_AND_LOGGING.md`
12. `12_FAILURE_PERFORMANCE_EVOLUTION.md`

### 第四轮：形成自己的项目

13. `13_CODE_OWNERSHIP_STRUCTS.md`
14. `14_RESUME_AND_PROJECT_PITCH.md`
15. `15_INTERVIEW_QUESTION_BANK.md`
16. `16_MOCK_INTERVIEW_PROTOCOL.md`

## 每个专题的四层标准

每个问题都按四层学习：

1. 一句话：先能直接回答。
2. 链路：知道请求和数据经过哪里。
3. 设计理由：知道为什么这样做。
4. 边界：知道项目没有做到什么，避免夸大。

## 覆盖矩阵

| 面试方向 | 主文档 | 必须掌握 |
| --- | --- | --- |
| 项目介绍 | 01、14 | 定位、架构、个人工作、亮点 |
| Go 服务拆分 | 01、13 | Gateway/Logic/Transfer、ServiceContext |
| 登录鉴权 | 02、09 | bcrypt、JWT、WS 握手、Origin |
| 单聊 | 03 | client_msg_id、message_id、seq、ACK、重连 |
| 群聊 | 04 | Kafka、Fetch/Commit、retry/DLQ、lease 幂等 |
| MySQL | 05、06、07 | 表职责、事务、唯一索引、锁、EXPLAIN |
| Redis | 03、05 | route、pending、offline、timeline、缓存 |
| 红包 | 06 | 超卖、重复领取、事务边界 |
| 游戏运营 | 07 | RBAC、版本、审批、灰度、Outbox、回滚、审计 |
| AI | 08 | provider、timeout、fallback、审计、边界 |
| 安全 | 09 | 密码、JWT、RBAC、Origin、权限、Secret、限流 |
| Docker/CI/CD/K8s | 10 | 镜像、流水线、Deployment、Service、Probe、回滚 |
| 监控 | 11 | metrics、Prometheus、Grafana、告警、日志字段 |
| 故障与性能 | 12 | Redis/Logic/Transfer/Kafka/ACK 故障、压测指标 |
| 代码所有权 | 13 | 结构体、字段、方法、入口、调用链 |
| 面试实战 | 15、16 | 分层追问、纠错、缺口补档 |

## 真实性红线

可以说：本地/CI 验证、可重复脚本、设计容量、故障恢复机制。

不能说：真实生产部署、零丢失、无限扩容、百万连接、真实支付、完整商业后台、AI 自主修复全部代码。

## 与旧资料的关系

旧文档继续作为源码索引和证据库：

- `../CODE_MAP.md`：文件地图。
- `../CORE_LINKS.md`：函数级调用链。
- `../INTERVIEW_QA.md`：旧题库。
- `../TEST_EVIDENCE.md`：测试与演示证据。
- `../AI_HANDOFF_PROJECT_FINAL.md`：给其他 AI 的完整交接。

本目录是新的唯一背诵顺序。旧资料与本目录冲突时，以当前代码、本目录和测试证据为准。
