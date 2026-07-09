# 项目一重设计：LinkGo Chat

## 1. 为什么要重设计

参考项目的阶段图和总架构图说明了一件事：

```text
IM 项目要像公司项目，不能只停留在“能聊天”。
```

但它也同时暴露出另一个问题：

```text
模块太多时，很容易变成“什么都做了一点，但没有一个点特别打人”。
```

对当前仓库来说，继续沿着“朋友圈 / 对象存储 / Elasticsearch / MCP / 多 Agent”一路扩功能，不适合秋招。

更合理的做法是：

```text
保住 IM 主链路
  +
把 AI 做成真正进入消息系统的 Bot
  +
保留红包作为高并发业务亮点
  +
给出一个能演示的微信风前端
```

这样项目一和项目二的边界也更清楚：

```text
项目一：实时通信系统 + AI Bot 接入
项目二：DeerFlow 二开 + Agent 任务流平台
```

## 2. 参考项目里值得借的东西

我们借的是思路，不是全量功能。

### 2.1 值得借的设计方式

```text
1. 阶段化推进：每一阶段有明确目标、技术收获和结果物。
2. 分层架构：客户端 / 接入层 / 服务治理层 / 业务层 / 中间件 / 数据层。
3. 业务模块清晰：用户、网关、长连接、联系人、消息、AI。
4. 每个功能都能回到“为什么要这样设计”。
```

### 2.2 不建议照搬的内容

```text
1. 朋友圈
2. 红包进一步扩复杂玩法
3. Elasticsearch
4. MinIO
5. MCP / Tool Calling / Memory
6. 太多业务模块并行推进
```

原因：

```text
1. 技术路线重复，讲起来不够新。
2. 容易稀释项目主线。
3. 和项目二的 DeerFlow / Agent 方向重叠。
4. 秋招更需要“少而硬”的项目点，而不是堆面。
```

## 3. 我们自己的最终项目定位

项目名建议统一改成：

```text
LinkGo Chat：AI 好友与红包协同 IM 系统
```

一句话定位：

```text
基于 Go-Zero 实现分布式 IM 后端，在不阻塞实时消息主链路的前提下，将 AI 好友和红包业务接入消息系统，支持单聊/群聊、红包并发领取、AI 私聊问答与群聊总结。
```

它不是：

```text
DeerFlow 接进 IM
复杂多 Agent 平台
朋友圈 + 搜索 + 大而全社交系统
```

它应该是：

```text
一个能证明你会：
长连接
消息可靠性
群聊异步扩散
AI 进入消息链路
红包并发一致性
简单产品化演示
的 IM 项目。
```

## 4. 当前仓库里已经有的基础

不用重做的部分：

```text
Gateway / Logic / Transfer 分层
登录、JWT、WebSocket、好友、群聊、历史消息
ACK / offline / session_timeline / route 等可靠性结构
Kafka + Transfer 群聊异步扩散
AI SummaryService / AskService / Provider / audit / metrics
public/index.html 单文件联调台
```

对应路径：

```text
cmd/gateway/
cmd/logic/
cmd/transfer/
internal/logic/
internal/server/
internal/ai/
public/index.html
```

当前真正缺的是：

```text
AI 还是独立 HTTP 接口，不是消息系统里的 Bot 用户。
```

## 5. 新项目只做 4 个差异化功能

只做这 4 个，最适合秋招。

### 5.1 功能一：AI Bot 私聊

效果：

```text
好友列表里固定有一个 AI 助手
用户可以直接给 AI 助手发消息
AI 以普通消息形式回复
历史消息、ACK、离线重连仍然成立
```

为什么它有价值：

```text
这不是“在 IM 上加个 AI 接口”
而是“把 AI 作为虚拟用户接进消息链路”
```

这会让项目一下子和普通聊天 demo 拉开差距。

### 5.2 功能二：群聊 @AI 总结 / 待办提取

效果：

```text
群里发送 @AI 总结一下
或发送 /summary
AI 读取最近 N 条群消息
返回：
  1. 总结
  2. 待办
  3. 风险
```

为什么它有价值：

```text
数据源来自消息存储，而不是文档知识库
它能说明“消息链路数据如何被 AI 二次利用”
```

### 5.3 功能三：微信风简洁 Web 界面

效果：

```text
登录
会话列表
好友 / 群聊 / AI 助手
聊天窗口
群聊 @AI 演示
历史消息回放
```

为什么它有价值：

```text
项目能被直观看懂
你面试时能直接演示
它让后端项目从“接口集合”变成“完整产品原型”
```

### 5.4 功能四：红包并发业务闭环

效果：

```text
支持单聊 / 群聊发红包
支持抢红包、重复领取保护、抢完结束、过期失效
前端能看到红包详情和领取结果
```

为什么它有价值：

```text
这不是简单接口，而是一个高并发一致性题
它能讲事务、行锁、唯一索引、状态流转和防超卖
```

它和 AI Bot 完全不是一条技术路线，所以很适合放在项目一里当业务亮点。

## 6. 新架构怎么设计

### 6.1 总体分层

```text
客户端层
  public/index.html（微信风调试台）

接入层
  Gateway：登录、JWT、WebSocket、HTTP API

业务层
  Logic：消息归一化、seq、落库、路由、联系人、群聊
  Transfer：Kafka 群聊异步扩散
  Bot Worker：异步消费 AI 任务并回写消息

AI 层
  Bot Service
  SummaryService
  AskService
  Provider / Fallback / Audit

数据层
  MySQL：messages / conversations / ai_* / bot config
  Redis：route / pending_ack / offline_msg / bot job queue
  Kafka：群聊 fanout
```

### 6.2 按模块和分层真正落地

参考图里的层次是对的，但我们只保留最值得做的部分。

```text
客户端层
  1. 登录
  2. 会话列表
  3. 好友列表
  4. 群聊窗口
  5. AI 助手私聊
  6. 红包创建/领取/详情

接入层
  1. JWT 鉴权
  2. WebSocket 建连
  3. HTTP API（好友、群组、红包、AI）
  4. 限流与健康检查

服务治理层
  1. Etcd 服务注册与发现
  2. Gateway -> Logic 路由
  3. Transfer 独立扩容

业务层
  1. 用户与登录
  2. 联系人/好友关系
  3. 长连接与 ACK
  4. 单聊/群聊消息
  5. 红包并发业务
  6. AI Bot 私聊
  7. 群聊 @AI 总结

中间件层
  1. Kafka 群聊异步扩散
  2. Redis 路由、离线、幂等、会话缓存
  3. Prometheus 指标

数据层
  1. MySQL 事务与最终存储
  2. Redis 高频状态存储
```

### 6.3 我们保留什么，不保留什么

保留：

```text
1. MySQL
2. Redis
3. Kafka
4. Etcd
5. Prometheus / Grafana
6. public/index.html 轻演示页
7. AI 助手私聊
8. 群聊总结
9. 红包
```

不保留：

```text
1. 朋友圈
2. Elasticsearch
3. MinIO
4. 向量数据库
5. MCP / Tool Calling / 多 Agent
6. UniApp / 小程序 / 原生移动端
7. 拼手气红包 / 排行榜 / 支付链路
```

原因：

```text
项目一的重点是“实时通信 + AI 进入消息链路 + 红包并发一致性”。
这些东西再加进去不会让主线更强，只会让项目变散。
```

### 6.4 Bot 不应该怎么做

不要这样做：

```text
用户发给 AI
-> 当前 HTTP 请求里直接调模型
-> 等 AI 回完再返回
```

问题：

```text
1. 阻塞消息主链路
2. 响应时间不可控
3. 模型失败会影响实时通信
4. 讲不出工程边界
```

### 6.5 Bot 正确链路

应该这样：

```text
用户消息进入正常消息链路
-> MySQL 落库
-> Redis / WebSocket 正常投递
-> 如果命中 AI Bot 或 @AI
-> 额外写一条 bot job
-> Bot Worker 异步消费
-> 调用 Summary / Ask / Provider
-> 以 ai_bot 身份再发一条普通消息
```

这样能讲清楚：

```text
AI 不阻塞主链路
AI 回复也是正常消息
Bot 不是外挂接口，而是消息系统成员
```

## 7. 为什么项目一不做 MinIO

后面如果被问到“IM 项目为什么没有对象存储”，建议这样回答：

```text
我这版项目故意把范围收在文本消息可靠投递、红包并发一致性和 AI Bot 接入上。
MinIO 更适合解决图片/文件上传、预签名 URL、带宽卸载和对象权限问题。
当前版本没有把图片/文件消息作为核心卖点，所以没有为了堆技术栈强行接 MinIO。
如果后续扩展图片/文件消息，我会把消息体里只保留对象 key，文件本身交给 MinIO 或云 OSS。
```

一句话版本：

```text
MinIO 不是不能做，而是它不是当前这个版本价值最高的能力。
```

## 8. 为什么这里只选 MySQL / Redis / Kafka / Etcd

### 8.1 MySQL

```text
1. 历史消息最终要有可靠落库
2. 会话、好友、群成员、红包都需要事务能力
3. 红包领取很适合讲 InnoDB 行锁和唯一索引
```

### 8.2 Redis

```text
1. 在线路由 route:<uid>
2. pending_ack / offline_msg 临时状态
3. session_timeline / message_payload 热数据补偿
4. client_msg_id 发送幂等
```

### 8.3 Kafka

```text
1. 单聊链路不必走 Kafka，直接投递更短
2. 群聊 fanout 成本高，异步解耦价值最大
3. Transfer 可以独立扩容，也能讲 retry / DLQ
```

### 8.4 Etcd

```text
1. 适合 go-zero 服务发现
2. 能支撑多 Gateway / Logic 的真实微服务口径
3. 足够你讲服务注册、发现和动态路由
```

### 8.5 为什么不继续加更多组件

```text
因为我想把关键链路做硬，而不是把中间件名单拉长。
面试时真正加分的是：为什么这里需要 Redis、为什么群聊才上 Kafka、为什么红包放 MySQL。
```

## 9. 功能一详细设计：AI Bot 私聊

### 7.1 用户形态

新增虚拟用户：

```text
user_id: ai_bot
username: ai_bot
display_name: AI助手
```

启动时默认：

```text
给演示账号自动建立与 ai_bot 的好友关系
```

### 7.2 数据来源

私聊问答优先用：

```text
最近 10~20 条当前私聊会话消息
```

如果用户问题更像项目问答，再补：

```text
KnowledgeBase 检索结果
```

第一版不要做：

```text
长期 memory
向量库
复杂画像
```

### 7.3 回复策略

第一版只支持两种模式：

```text
1. 通用答复：基于最近聊天记录生成回复
2. 项目问答：基于项目文档回答
```

不用做成智能路由系统，只做简单规则：

```text
如果消息包含：
Kafka / ACK / WebSocket / seq / Redis / 项目 / 架构 / 为什么
优先走 AskService + KnowledgeBase

否则走 ChatBotService（近期会话上下文）
```

## 10. 功能二详细设计：群聊 @AI

### 8.1 触发方式

只保留 2 个入口：

```text
@AI 总结一下
/summary
```

### 8.2 数据来源

```text
直接查当前群 conversation_id 下最近 N 条消息
```

复用现有：

```text
internal/ai/summary_service.go
```

### 8.3 输出形式

群里回一条普通文本消息：

```text
今日总结：
待办：
风险：
```

不需要一上来回结构化卡片。

## 11. 功能三详细设计：微信风前端

### 9.1 技术选择

不新开 React / Vue 工程。

直接基于：

```text
public/index.html
```

改成：

```text
左侧会话列表
中间聊天区
AI 助手单独头像
输入框支持快捷触发 /summary
群消息里支持 @AI 提示
```

### 9.2 为什么不新建复杂前端

因为项目一的核心不是前端框架，而是：

```text
消息链路
AI Bot 接入
演示闭环
```

单文件前端足够承担演示职责。

## 12. 为什么红包要保留，但不要做复杂玩法

当前仓库里红包已经有比较好的基础：

```text
red_packets
red_packet_claims
Create / Claim / Detail API
事务 + SELECT ... FOR UPDATE
唯一索引防重复领取
条件 UPDATE 防超卖
public/index.html 已有演示入口
```

对应代码：

```text
internal/logic/redpacket.go
cmd/gateway/internal/logic/redpacketlogic.go
public/index.html
```

它对秋招有价值，因为面试官很容易沿着这些问题继续问：

```text
1. 怎么防超卖？
2. 怎么防一个人重复抢？
3. 抢到最后一份时怎么保证状态一致？
4. 红包过期怎么处理？
5. 为什么这里用 MySQL 事务，而不是一上来全放 Redis？
```

但是不要做成“大红包项目”。只保留最能讲的版本：

```text
等额红包
并发领取
重复领取保护
过期与完成状态流转
前端可演示
```

明确不做：

```text
随机红包算法
拼手气分配
红包排行榜
复杂财务结算
消息队列异步发奖
支付链路
```

这样红包是业务亮点，不会反客为主。

## 13. 我们自己的四阶段路线

参考原项目的四阶段方式，但改成更适合当前仓库的内容。

### 第一阶段：最小可用 IM 演示台

包含内容：

```text
1. 保持登录、好友、群聊、历史消息可演示
2. 整理 public/index.html 为微信风简洁界面
3. 保留红包入口，但不扩玩法
```

重点目标：

```text
1. 让项目先有一个完整、直观的演示面
2. 不再继续扩散基础模块
3. 为 AI Bot 和红包亮点提供统一入口
```

阶段成果：

```text
1. 登录后能看到会话、好友、群聊
2. 能正常发送消息、查看历史、建群
3. 红包入口和消息入口在同一界面
```

### 第二阶段：AI Bot 基础接入

包含内容：

```text
1. ai_bot 虚拟用户与好友关系
2. 私聊 AI 最小闭环
3. AI 回复以普通消息回写
```

当前落地方式：

```text
1. 默认 AI 好友 user_id = 9001，username = ai_assistant。
2. 用户向 9001 发送普通私聊消息后，Logic 在原消息落库和投递完成后触发 BotResponder。
3. BotResponder 复用 AskService / KnowledgeBase 生成答案。
4. AI 回复以 9001 身份再次走 Logic.PushMessage，所以仍然拥有 seq、落库、ACK、离线补偿和历史查询。
```

前端验收方式：

```text
1. 运行 make frontend-smoke。
2. 打开 http://127.0.0.1:8088/。
3. 两个标签页分别登录 userA / userB，点击“打开对聊”验证普通私聊。
4. 单个标签页登录 userA，点击“AI 助手”后发送“项目里 Redis 用来做什么？”验证 AI 私聊。
```

DeepSeek 接入方式：

```text
当前仓库不会保存真实 API key。
如果要使用 DeepSeek，把本机环境变量设置为：
AI_PROVIDER=deepseek
AI_MODEL=deepseek-v4-flash
AI_BASE_URL=https://api.deepseek.com
DEEPSEEK_API_KEY=<真实 key>

Logic 服务里的 AI 好友和 Gateway 的 AI HTTP 接口都会读取这些变量。
```

重点目标：

```text
1. 让 AI 真正进入 IM 体系，而不是停留在 HTTP 接口
2. 保持原有消息主链路不被破坏
3. 强化“AI 是系统内虚拟好友”这个卖点
```

阶段成果：

```text
1. 登录后可以看到 AI 助手
2. 可以像微信一样给 AI 发消息
3. AI 以普通消息形式异步回复
```

### 第三阶段：群聊 AI + 红包业务亮点

包含内容：

```text
1. 群聊 @AI / /summary
2. 总结 / 待办 / 风险输出
3. 红包创建 / 领取 / 详情链路整理
4. 补红包并发测试与文档
```

重点目标：

```text
1. 让 AI 不只是私聊工具，而是群助手
2. 把红包明确收成项目业务亮点
3. 形成“AI 接入 + 并发业务”两条不同的面试追问线
```

阶段成果：

```text
1. 群里触发 AI 总结
2. 红包链路能在前端直接演示
3. 红包并发设计可以单独讲清楚
```

### 第四阶段：工程边界与秋招收口

包含内容：

```text
1. Bot Worker 异步执行
2. audit / attempt / metrics 统一收口
3. 最终演示脚本
4. 简历、面试稿、项目口径统一
```

重点目标：

```text
1. AI 明确不阻塞主链路
2. 红包和 AI 都能有独立面试卖点
3. 形成项目一和项目二的清晰分工
```

阶段成果：

```text
1. Bot 异步 job 与审计闭环
2. 最终 demo runbook
3. 新版简历项目描述
```

## 11. 具体到代码该改哪些地方

### 11.1 低风险复用

优先复用这些：

```text
internal/ai/summary_service.go
internal/ai/ask_service.go
internal/ai/provider.go
internal/ai/mock_provider.go
internal/ai/openai_provider.go
public/index.html
internal/logic/handler.go
internal/server/client.go
```

### 11.2 需要新增的模块

建议新增：

```text
internal/bot/
  bot_service.go         识别 Bot 触发规则
  worker.go              异步执行 Bot job
  queue.go               Redis / 内存 job 队列封装
  reply_builder.go       把 AI 结果转成普通消息文本
  chat_service.go        最近会话上下文问答
```

### 11.3 需要补的 SQL

建议新增：

```text
1. ai_bot 用户初始化 SQL
2. 默认好友关系初始化
3. 可选 bot_jobs / bot_settings 表
```

第一版也可以先不落表，用 Redis 队列 + 现有 ai_* 结果表。

## 13. 这次明确不做什么

为了保证秋招可收口，明确不做：

```text
1. 朋友圈
2. 随机红包 / 拼手气等复杂红包玩法
3. ElasticSearch
4. MinIO
5. 多 Agent
6. MCP / Tool Calling
7. 向量库
8. 完整移动端 App
```

## 14. 最终简历怎么讲

项目名：

```text
LinkGo Chat：AI 好友与红包协同 IM 系统
```

简历描述建议：

```text
- 基于 Go-Zero 构建企业协同 IM 后端，拆分 Gateway、Logic、Transfer 三层，完成登录鉴权、WebSocket 长连接、单聊/群聊、ACK 补偿、离线重放和历史消息查询等核心链路。
- 基于 Redis 在线路由、会话级 Lua seq 与 Kafka + Transfer 异步 fanout 实现跨节点消息投递、弱网补偿与群聊削峰，并暴露 Prometheus 指标支撑问题定位。
- 设计红包业务闭环，基于 MySQL 事务、红包主行锁、条件 UPDATE 与 `red_packet_id + user_id` 唯一索引约束重复领取和并发超卖，形成可独立讲解的一致性业务场景。
- 将 AI Bot 作为虚拟好友与群助手接入消息系统，支持私聊问答、群聊总结和待办提取；AI 回复通过异步 Worker 回写普通消息链路，不阻塞实时通信主流程。
- 基于现有消息存储和项目文档复用 Summary / Ask / Provider 模块，补充调用审计、attempt 留痕、演示脚本和微信风前端联调台，形成可演示、可复盘的业务闭环。
```

## 15. 下一步实施顺序

真正开始改代码时，建议严格按这个顺序：

```text
第 1 步：整理 public/index.html，先把演示台变成微信风简洁页面
第 2 步：做 ai_bot 虚拟好友私聊闭环
第 3 步：做群聊 @AI 总结
第 4 步：整理红包链路与并发测试证据
第 5 步：做 Bot Worker 异步化
第 6 步：更新 demo / docs / resume 口径
```

不要跳着做。

前两步做成，项目气质就会明显变对；红包收口后，项目一就有了第二条很强的面试追问线。
