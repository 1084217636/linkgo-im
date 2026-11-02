# IM-P1 现状发现与差异清单

审计基线：2026-10-28，工作区 HEAD `d5f8664`，**同时包含既有未提交改动**。本轮只读取代码并运行已有定向测试；下文不把 IM-P1 设计写成已实现。按《项目升级双工具评审_Codex执行方案.md》的 A（已验证具备）/B（部分具备）/C（待实现）标记。

## 源码核对

| 事实 | 状态 | 代码依据与边界 |
| --- | --- | --- |
| 登录后的受保护 HTTP 路由、群创建、群成员查询 | A | `api/gateway.api` 的 `/group/create`、`/group/members`；`cmd/gateway/internal/handler/routes.go` 注册 Auth 中间件。任务相关路由尚无。 |
| MySQL 群与角色 | A | `sql/20260611_contacts_groups.sql` 的 `im_groups`、`group_members`：`owner/admin/member`、`active/left/removed`。`cmd/gateway/internal/logic/groupcreatelogic.go` 在事务中建群和成员；`groupmemberslogic.go` 对非活跃成员拒绝列表访问。当前 API 没有群成员移除/角色调整入口，变更场景要用受控测试夹具。 |
| 群消息投递与持久化 | A | `internal/logic/handler.go:66` 的 `PushMessage` 分配会话 seq、校验发送者、解析群收件人；`saveMessageWithOutbox` 同事务保存消息、会话摘要 Outbox、可选投递 Outbox。`cmd/logic/internal/svc/kafka_dispatcher.go` 和 `cmd/transfer/main.go` 负责群扩散。 |
| 可靠通知可直接复用为业务事件 | B | 现有 `conversation_outbox` 与 `delivery_outbox` 都由**已经存在的 IM `messages` 行**驱动；`delivery_outbox` 通过 `JOIN messages` 读取消息。任务状态变更不能直接插入其中任一表作为独立业务事件。`delivery_outbox.go` 及 `sql/20260903_delivery_outbox.sql` 当前尚未提交；实施前须固定基线。 |
| 任务权限基础 | B | `group_members` 是可复用的事实源，但 `internal/logic/relations.go` 的群发权限把 `mute_until` 也算进去；任务阅读/提交/审核需要独立定义权限，不能直接调用“能否发群消息”的判断。 |
| Web 客户端基础 | B | `public/index.html` 可登录、看群、聊天、历史、WebSocket；`public/client-store.js`/`history-sync.js` 维护消息本地缓存。没有任务列表、表单、审核、进度页面。现有页面是零构建联调客户端，适合扩展成 IM-P1 可操作验收页。 |
| 学习群任务领域模型、状态与 API | C | `api/gateway.api`、`sql/`、`cmd/gateway/internal/logic/` 未发现 `group_tasks`/提交/审核/进度；聊天正文不能替代这些业务事实。 |

## 按七个维度的当前评价

| 维度 | 当前 | 依据 | IM-P1 的目标 |
| --- | --- | --- | --- |
| 业务闭环 | 弱 | 群里可发消息，但没有任务发布、作答、审核与进度 | 一条完整用户旅程 |
| 状态流转 | 中 | 消息与 Outbox 有状态；学习任务没有状态机 | 任务、提交、通知三类状态分开 |
| 工程深度 | 强 | Go 服务、MySQL、Redis、Kafka、ACK/补偿已有 | 把工程机制用于业务一致性 |
| 异常边界 | 中 | 消息重试和部分故障测试已有；任务并发、重复审核不存在 | 幂等、事务、权限与通知重试 |
| 前端可演示性 | 中 | 可操作 IM 页；任务页面不存在 | 三账号可完成任务闭环 |
| 验证证据 | 中 | 群安全和投递单测已有；没有任务 E2E | 新的自动化与演示证据 |
| 面试可追问性 | 中 | 能讲 IM 可靠性；业务建模追问不足 | 角色、状态、事务、事件一致性 |

## 代码/文档差异及实施前基线

- `api/gateway.api` 的 `HistoryReq` 确有 `BeforeSeq`/`Limit`；实际 `cmd/gateway/internal/logic/historylogic.go` 会传给 Logic，`internal/logic/handler.go` 按 `seq` 查询。旧文档若写“仅最新页”应以这条实现为准。
- README 明示 Gateway 当前直接访问部分 MySQL；IM-P1 选择在同一 MySQL 上增加领域模块，不虚构一个已经存在的独立任务微服务。
- HEAD 之外已有 `delivery_outbox.go`、迁移和前端同步文件。实施会话应先核对这些改动、固定可复现基线，不覆盖它们。
- 定向测试已运行：`go test ./cmd/gateway/internal/logic ./internal/logic ./cmd/transfer`，三个包通过（Go 缓存命中）。这只是既有代码基线，**IM-P1 验收为零**。

## 本轮范围

本轮只产出需求、架构、图和实施计划，不改业务代码、数据库或真实用户数据。IM-P2 的课程/章节与 IM-P3 积分不在本轮规格内。
