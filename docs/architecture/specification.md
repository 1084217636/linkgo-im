# IM-P1 需求规格：学习群任务与审核

状态：**原始需求设计**；实际落地和差异见 [IM-P1 验收记录](../evidence/im-p1-evidence.md)。依据见 [现状发现](./discovery.md)。目标是一条可由三个账号独立演示的业务链：**群主创建草稿 → 发布任务 → 成员收到群消息并提交文字/链接 → 群主审核通过或退回 → 成员查看状态并按需重交 → 群主查看进度并关闭任务**。

## 1. 范围与术语

- 一个既有 `im_groups` 群就是 IM-P1 的学习群；本轮不新建班级、课程、章节、积分、文件上传或外部通知渠道。
- `group_tasks` 是任务事实；`task_submissions` 是每个成员的最新提交；`task_events` 记录每次成功的状态操作及请求标识；`task_notification_outbox` 负责把应通知的事件最终变成 IM 消息。WebSocket 消息是提醒，HTTP 任务详情才是权威状态。
- 当前活跃群成员中的 `owner/admin` 可以管理任务和审核；`member` 可以阅读、提交自己的任务。`owner/admin` 本轮不提交作业，不审核自己的提交。禁言影响群聊天，不影响阅读任务、交作业或审核；离群/移除则全部失去访问权限。
- 发布时不冻结名单。**当前活跃的普通成员**可查看和提交已发布任务；后来入群者可在任务列表发现旧任务；离群者的历史提交保留供管理员审计，但不计入当前成员进度，也不能再读详情。由于当前没有群成员变更 API，验收用受控数据库夹具模拟变化。

## 2. 用户故事与业务规则

| 编号 | 用户动作与可见结果 | 规则和异常 | 可执行验收 |
| --- | --- | --- | --- |
| US-01 | 群主在该群新建/编辑草稿，填写标题、说明、可选截止时间 | 仅本群活跃 owner/admin；标题 1～120 字、说明 ≤ 5000 字，截止时间晚于当前时间；草稿仅管理员可见；重复请求返回同一任务，不同内容复用请求 ID 返回冲突 | 用 owner、member、非成员各调用一次；仅 owner 成功。刷新页后草稿仍在，普通成员列表不可见 |
| US-02 | 管理员发布草稿，成员在任务列表看到任务并收到一条群消息提示 | 仅 `draft → published`；发布事务写任务状态、审计事件及通知待办；已发布重复请求不产生第二任务或第二通知；Redis/Kafka 不可用不回滚已提交的任务 | 三账号演示；模拟通知失败后仍可 GET 到 `published`，恢复后只形成一条可见通知消息 |
| US-03 | 活跃普通成员打开任务，提交文字和/或一个 URL | 仅 `published` 且未到截止时间；至少文字或链接一项非空；正文 ≤ 5000 字，URL 仅 http/https 且 ≤ 2048 字；服务端从 JWT 取用户 ID，不接受替别人提交；同一成员只有一份当前提交 | 正常提交后刷新可见；空内容、过期、非成员、管理员代交均拒绝；并发双击只有一个当前提交 |
| US-04 | 管理员查看提交并“通过”或“退回”，退回时写明理由 | 仅待审核 `submitted → approved/returned`；退回理由 1～1000 字；提交行和任务行锁定后检查版本；并发两人审核仅一个成功，另一个 409；重复同一请求 ID 返回原结果 | 两个管理员并发审核同一份提交，最终只出现一次有效审核事件；被退回者看到理由，其他普通成员看不到其内容 |
| US-05 | 被退回成员修改文字/链接后重新提交 | 仅 `returned → submitted`，`attempt_no` 增加；保留旧提交与退回的审计事件；`approved` 不可再改；到期或任务关闭不可重交 | 展示两次提交历史和最终状态；用旧版本再提交返回 409 |
| US-06 | 管理员看进度并关闭任务 | 按**当前活跃普通成员**统计 `未提交/待审核/退回/通过`，每人最多计一次；有待审核提交时不能关闭；`published → closed` 后禁止提交和审核，已批准结果可读 | 群主查看计数总和等于当前活跃普通成员数；关后提交/审核返回 409；刷新页状态保持 |
| US-07 | 成员收到任务状态提醒，并从提醒打开权威详情 | 发布给群的通知只含标题和任务 ID；审核结果私发给提交者，只含状态和任务 ID，退回理由只在受权任务详情；通知可延迟、可重复投递，页面按稳定消息 ID 去重并 GET 最新任务状态 | 断通知 worker 后状态已提交但页面可主动刷新；恢复 worker 后提醒出现；重复消费不形成第二条持久 IM 消息 |
| US-08 | 非成员或已移除成员尝试任务列表、详情、提交、审核、进度、通知链接 | 每次 HTTP 请求按 MySQL `group_members` 重新授权；列表不泄露标题，详情不泄露内容；通知摘要不包含答案或理由；对不存在和无权访问使用统一的 404 类响应以降低枚举 | 在移除成员夹具后重试所有接口均不能获得任务资料；旧 Web 页面按钮仍在也无法越权 |

## 3. 状态机和数据一致性

```text
任务: draft --发布--> published --关闭--> closed
提交: 无 --提交--> submitted --通过--> approved
                         └--退回--> returned --重交--> submitted
通知: pending --领取--> processing --IM 消息持久化--> done
          └/processing 超时或失败--> pending(退避) --次数耗尽--> dead(保留排查)
          └目标发送前已离群/群已解散--> skipped(保留原因)
```

- 任务 `closed` 是终态；草稿不允许直接关闭，已发布不允许改题目/说明/截止时间，以避免成员按不同版本交作业。本轮仅允许改草稿。截止时间到达**不自动关闭任务**，只是停止新提交与重交；管理员仍可审核已提交项。
- 提交的 `version` 每次成功状态变化加一。客户端带 `expected_version`；版本不一致返回 409 并附当前版本供刷新。`attempt_no` 仅成员重交时增加；同一请求 ID 绝不增加两次。
- 每个成功写操作在一个 MySQL 事务内提交业务行与 `task_events`；需要通知的操作再写 `task_notification_outbox`。DB 事务失败则三者都不出现。HTTP 200/201 只表示**业务事实已经入库**，不表示收件人已收到或阅读通知。
- 一个事件至多产生一条持久 IM 消息：系统使用 `task_event:<event_id>` 作为稳定 `client_msg_id`，依赖现有 `messages(from_uid, client_msg_id)` 唯一键；网络投递仍可能至少一次，接收页按 `message_id` 去重。

## 4. 最小 API 契约（均使用现有 JWT）

路径可在实施时按 goctl 生成约束做等价调整，但动作、权限、错误语义不可改变。所有写接口必须有 `request_id`（客户端生成、重试保持不变）。

| 方法与路径 | 请求关键字段 | 响应/权限 |
| --- | --- | --- |
| `POST /api/v1/groups/:group_id/tasks` | `request_id,title,description,due_at?` | 201 `task_id,status=draft,version`；owner/admin |
| `PATCH /api/v1/tasks/:task_id` | `request_id,expected_version,title,description,due_at?` | 更新草稿；owner/admin |
| `POST /api/v1/tasks/:task_id/publish` | `request_id,expected_version` | `published` 与事件 ID；owner/admin |
| `GET /api/v1/groups/:group_id/tasks?cursor=&limit=` | 限制 1～50 | 成员看已发布/关闭，管理员另见草稿；稳定游标分页 |
| `GET /api/v1/tasks/:task_id` | 无 | 返回任务、当前用户可见的提交和权限动作；owner/admin 可看汇总，成员仅自己 |
| `PUT /api/v1/tasks/:task_id/submission` | `request_id,expected_version?,text?,link?` | 创建/重交，返回 `submission_id,attempt_no,status,version`；普通成员 |
| `POST /api/v1/submissions/:submission_id/review` | `request_id,expected_version,decision=approve/return,reason?` | 通过/退回；owner/admin |
| `GET /api/v1/tasks/:task_id/submissions?cursor=&limit=` | 限制 1～50 | 管理员看列表与内容；普通成员无权 |
| `GET /api/v1/tasks/:task_id/progress` | 无 | 管理员看当前群成员分布；成员可只看自己的状态，不返回他人内容 |
| `POST /api/v1/tasks/:task_id/close` | `request_id,expected_version` | 关闭；owner/admin；有待审核时 409 |

错误：401 未登录，404 任务/群不存在或不可访问，400 字段无效，409 重复 ID 对应不同内容/版本冲突/非法状态迁移，503 MySQL 不可用。任务通知积压不把已成功的业务写请求改成 5xx。

## 5. 端到端场景及本轮不做的事

验收演示账号：A 为群主，B 为普通成员，C 为非成员。A 建草稿、发布；B 从群消息进入任务并提交；A 退回，B 修改重交，A 通过并关闭；A 看进度，C 全程无法读写。关闭通知 worker、恢复后重复演练，业务状态不变且通知最终出现一次。

IM-P1 不做课程章节、分数、积分、自动批改、附件上传、多设备完整同步、移动端 SDK 或生产级 HA。现有群消息的“已入库/已投递/客户端 ACK/已读”语义保持区分；任务状态通过 HTTP/MySQL 查询。
