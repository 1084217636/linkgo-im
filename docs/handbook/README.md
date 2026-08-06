# LinkGo 从零学习手册

> 唯一学习主线：只按本目录 `00_START_HERE.md` 到 `21_CHECKLIST.md` 的编号顺序阅读。此前新增的 `docs/learning/` 摘要已合并到本手册，不再单独维护。

这是一套给“只会 Go 基本语法、第一次接触后端项目”的教材。它不是复习提纲，也不要求你提前知道 Redis、MySQL、Kafka、WebSocket、Docker 或 Kubernetes。

## 唯一阅读顺序

必须按编号阅读。后面的章节可以使用前面已经解释过的概念，前面的章节不会要求你先理解后面的内容。

### 第一阶段：先看懂程序为什么存在

1. [00 使用方法](00_START_HERE.md)
2. [01 从用户角度认识 IM](01_WHAT_IS_IM.md)
3. [02 服务器与网络最小基础](02_SERVER_AND_NETWORK.md)
4. [03 看懂 Go 项目目录](03_GO_PROJECT_MAP.md)
5. [04 第一次 HTTP 请求](04_HTTP_FIRST_REQUEST.md)

### 第二阶段：完成登录和长连接

6. [05 MySQL 与最小数据模型](05_MYSQL_AND_DATA.md)
7. [06 登录、密码与 JWT](06_LOGIN_AND_JWT.md)
8. [07 从 HTTP 升级为 WebSocket](07_WEBSOCKET_CONNECTION.md)
9. [08 先看单台 Gateway 的单聊](08_SINGLE_GATEWAY_CHAT.md)

### 第三阶段：从单机走向公司多服务器场景

10. [09 Redis 基础与在线状态](09_REDIS_BASICS.md)
11. [10 跨 Gateway 单聊完整链路](10_MULTI_GATEWAY_CHAT.md)
12. [11 ACK、重试与离线恢复](11_RELIABILITY_AND_OFFLINE.md)
13. [12 Kafka 与群聊扩散](12_GROUP_CHAT_AND_KAFKA.md)

### 第四阶段：理解业务功能

14. [13 好友、群组与会话](13_RELATIONSHIPS_AND_CONVERSATIONS.md)
15. [14 红包并发一致性](14_RED_PACKET.md)
16. [15 AI 虚拟好友](15_AI_BOT.md)

### 第五阶段：理解工程化

17. [16 安全、日志、指标和故障](16_SECURITY_OBSERVABILITY.md)
18. [17 Docker 与 GitHub Actions](17_DOCKER_AND_CI.md)
19. [18 Kubernetes 多服务器部署](18_KUBERNETES_DEPLOYMENT.md)

### 第六阶段：形成面试表达

20. [19 完整调用链与代码地图](19_COMPLETE_CODE_WALK.md)
21. [20 面试讲述与逐层追问](20_INTERVIEW_PREP.md)
22. [21 学习检查表](21_CHECKLIST.md)

## 每章怎么学

每章固定回答八类问题：

1. `本章前置`：只允许使用哪些已学知识。
2. `本章目标`：学完必须能回答什么。
3. `新概念`：第一次出现时用普通语言解释。
4. `项目链路`：概念怎样落在 LinkGo 中。
5. `选择理由`：原问题是什么、不处理会怎样、为什么选当前方案、最近替代是什么、新代价和边界是什么。
6. `本章代码阅读任务`：给出真实文件、函数/结构体/字段、建议顺序、要看到的程度和暂时不用掌握的内容。不要只点开文件扫一遍，要完成表格中的定位动作。
7. `动手练习与闭卷检查`：先关闭文档作答或画图，答不上时先回到本章正文和代码任务，不要立即抄答案。
8. `同文件参考答案`：答案紧跟在本章题目后。完成自己的版本再展开核对，逐题标记“正确、漏步骤、把边界说大”；答案不是要求逐字背诵。

每章达到下面标准再继续：能用自己的话讲原因，能按阅读任务找到指定符号，能在不看答案时完成至少 80% 的题。链接只负责打开位置；真正的阅读范围和停止条件以该章“本章代码阅读任务”为准。

## 两种环境不要混淆

- 教学前半段会暂时画成一台 Gateway、一个 Logic，目的是先看懂一条消息。
- 从第 10 章开始进入面试默认场景：A、B 连接不同 Gateway，Logic 是集群，Redis/MySQL/Kafka/Etcd 是共享服务。
- Docker Compose 是个人电脑上的复现方法；Kubernetes 是多实例部署方法。两者不会在前面章节提前混讲。

## 真实性规则

看到“已实现”时必须能在代码或测试中定位；看到“演进方案”时不能在简历里写成已经上线。这个仓库是可运行、可测试的秋招项目，不代表经历过真实商业流量。
