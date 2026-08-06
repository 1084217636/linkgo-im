# 13 好友、群组与会话：为什么聊天不能只有 messages 表

## 本章前置

你已经知道 HTTP 接口怎样进入 Gateway，也已经学过单聊、群聊、ACK、MySQL 和 Redis。

本章开始把“能传一条消息”扩展成“有联系人、群成员、最近会话和历史记录的 IM”。

## 本章目标

学完后，你必须能回答：

1. 好友申请和好友关系为什么需要两张表？
2. 单聊和群聊发送前分别检查什么权限？
3. `conversation`、`session_id` 和一条 `message` 有什么区别？
4. 登录时最近会话从哪里读取？
5. `last_seq`、`read_seq`、`unread_count` 怎样计算？
6. 关系数据库不可用时，为什么发送、群扩散和成员列表都要拒绝？
7. 当前会话与群组功能还有哪些一致性和权限边界？

## 1. 消息不是全部业务数据

如果系统只有 `messages` 表，它只能回答“过去写入过什么消息”，却很难回答：

- A 是否允许给 B 发消息？
- 谁向谁发过好友申请？
- G100 是否存在？
- A 现在是不是 G100 的成员？
- 登录后应该展示哪些最近会话？
- 某个会话最新消息序号是多少？
- 用户读到了哪一条？

所以 LinkGo 把不同事实放在不同表中。

## 2. 先理解关系型数据库中的“关系”

MySQL 表中的一行可以表达一个业务关系，例如：

```text
(user_id=1001, friend_id=1002, status=normal)
```

它表示 1001 当前把 1002 视为正常好友。

关系表通常需要联合主键或唯一索引，避免同一对对象出现多份互相冲突的记录。

## 3. 好友申请与好友关系为什么分开

当前有两张表：

```text
friend_requests   申请流程
friend_relations  已形成的好友关系
```

### `friend_requests`

保存：

```text
from_user_id
to_user_id
message
status = pending / accepted / rejected
created_at / updated_at
```

它回答“谁申请了谁、申请处理到什么状态”。

### `friend_relations`

保存：

```text
user_id
friend_id
status = normal / blocked / deleted
```

它回答“当前是否允许作为好友互动”。

把两者分开的原因是：历史流程状态和当前有效关系不是一件事。

当前好友关系为两个方向各保存一行，换来“按 user_id 直接查自己的好友”这一简单查询；代价是同一个业务关系出现两份数据。接受申请时必须在同一个 MySQL 事务里同时写两行，否则可能出现 1001 认为 1002 是好友、1002 却查不到 1001 的单向不一致。

## 4. 发起和处理好友申请的真实链路

这些 REST 接口在 Gateway 进程的 `internal/logic` 中直接访问 MySQL，没有经过独立 Logic gRPC 服务。

### 发起申请

```text
POST /api/v1/friend/apply
→ JWT 得到申请人 uid
→ 拒绝给自己申请
→ 查询目标用户是否存在
→ 如果已经是 normal 好友，直接返回 accepted
→ INSERT friend_requests
→ 相同方向重复申请时更新回 pending
```

### 接受或拒绝

```text
POST /api/v1/friend/respond
→ JWT 得到当前接收人 uid
→ 开启 MySQL 事务
→ 只更新 from→to 且仍为 pending 的申请
→ accept 时写入两条 friend_relations
→ commit
```

为什么接受时写两条？因为当前数据模型按方向查询：

```text
1001 → 1002 normal
1002 → 1001 normal
```

事务保证申请状态和两条好友关系要么一起提交，要么一起回滚。

## 5. 单聊发送前怎样校验好友

客户端写入的 `from` 不能被信任。Logic 首先用 WebSocket JWT 对应的用户 ID校验发送者：

```text
frame.From 为空 → 补成已登录 uid
frame.From 不等于已登录 uid → 拒绝 sender mismatch
```

然后单聊查询：

```sql
SELECT status
FROM friend_relations
WHERE user_id = ? AND friend_id = ?;
```

只有 `normal` 才能发送。

所以 Redis route 只回答“B 在线哪里”，不回答“A 有没有权利给 B 发消息”。权限事实来自 MySQL 好友关系。如果 Logic 没有数据库句柄，或查询因表缺失、连接错误而失败，当前代码会 fail-closed 拒绝发送，不会把“无法判断”当成“允许”。

为什么不在 MySQL 故障时临时根据 Redis 缓存放行？好友删除、拉黑或群成员移除后，缓存可能还是旧值。放行会把数据库故障变成越权发送。当前选择的代价是 MySQL 问题会直接影响发消息可用性，生产环境需要通过高可用数据库入口和故障恢复降低这个影响。

## 6. 群组与群成员为什么也分表

当前有：

```text
im_groups      群本身
group_members  用户与群的成员关系
```

### `im_groups`

主要字段：

```text
group_id
name
owner_id
status = active / dismissed
```

### `group_members`

主要字段：

```text
group_id + user_id  联合主键
role                owner / admin / member
mute_until
status              active / left / removed
joined_at
```

一个群只有一条 `im_groups`，但可以有很多条 `group_members`。

如果把群名、群状态、每个成员角色全塞进一行或为每个成员重复群信息，修改群名会更新很多副本，角色和退出状态也很难单独约束。拆表后，`im_groups` 保存群本身，`group_members` 保存“谁以什么状态属于群”；代价是发送、列表和权限校验要额外查询或 JOIN，并且创建群后写入的 Redis 成员缓存与 MySQL 之间还需要一致性策略。当前消息验权和 recipients 解析不会在 MySQL 失败时回退到 Redis 成员集合。

## 7. 当前创建群的真实链路

创建群也是 Gateway REST 逻辑直接访问 MySQL：

```text
POST /api/v1/group/create
→ JWT 得到 creator_id
→ 把 creator 自动加入成员集合
→ MySQL 事务 INSERT im_groups
→ 相同 group_id 已存在时回滚并拒绝，不接管原群
→ 事务内写入每个 group_members
→ commit
→ 再把成员写入 Redis group_members:<gid>
→ 再写 user_groups:<uid>
```

MySQL 是群和群成员的授权事实来源；Redis 集合是创建后的辅助索引，不在数据库失败时替代验权。拒绝重复 group_id 的理由是：如果把创建写成无条件 upsert，另一个已登录用户可能用同一 ID 覆盖群主和成员，这不是普通冲突，而是资源接管风险。

查询群成员列表时，服务端先从 JWT 取请求者 uid，再查询该用户的 `group_members.status`。只有 `active` 成员才能继续查全部 active 成员；非成员、数据库不可用和查询错误都不会降级放行。

当前代码需要诚实说明几个缺口：

- 没有检查请求中的每个成员用户是否真实存在；
- MySQL 已 commit 后 Redis 更新失败时，接口记录错误但仍按数据库成功返回，避免让客户端误以为可以重新创建同一群；当前没有缓存重建任务，Redis 辅助索引可能暂时缺失；
- 没有完整的邀请审批、退群、踢人、解散群管理接口；
- 没有更细的 owner/admin/member 管理 RBAC；当前成员列表只区分“active 成员”和“其他人”。

因此它是基本群关系原型，不是完整商业群管理系统。

## 8. 群消息发送与历史权限

群消息发送前，Logic 查询发送者的成员行：

```text
status 必须是 active
mute_until 必须已经到期，或者为 0
```

群消息的 recipients 来自全部 active 成员，发送者本人被排除。

这份 recipients 也只从 MySQL `group_members` 读取。如果 DB 句柄缺失或查询失败，Logic 不会从 Redis `group_members:<gid>` 猜一份成员列表，而是终止群任务发布。否则已退群或被移除的用户可能继续出现在 Kafka job 的收件人快照中。

查询群历史时也会先检查请求用户仍是 active 成员，再查询 `messages`。

但发送权限和历史权限不是同一个函数：发送还检查禁言，历史只检查当前 active 成员。文档中不要笼统说“所有群接口都有完整 RBAC”。

## 9. conversation 与 message 的区别

一条消息是一次具体发送：

```text
message_id
from_uid
content
seq
```

会话是多条消息所属的长期容器：

```text
c2c:1001:1002
group:G100
```

当前代码中的 `session_id` 与 `conversation_id` 对聊天消息保持相同值。

单聊会把两个 uid 排序，保证 A→B 和 B→A 使用同一个会话：

```text
c2c:min_uid:max_uid
```

群聊会话是：

```text
group:<group_id>
```

表名不同不代表当前系统有两套会话 ID 算法。

## 10. 为什么需要 conversations 表

如果每次登录都扫描全部 `messages` 并对每个会话求最新一条，数据量大后会很昂贵。

`conversations` 保存会话级摘要：

```text
id
type
created_at
updated_at
last_seq
```

`conversation_members` 保存用户参与哪些会话：

```text
conversation_id
user_id
read_seq
joined_at
```

最近消息正文仍从 `messages` 中通过 `last_seq` 关联读取。

## 11. 一条消息怎样更新会话状态

消息投递编排完成后，Logic 调用 `updateConversationState`。

### Redis 热状态

同步更新：

```text
conversation:members:<conversation_id>  SET
conversation:last:<conversation_id>     HASH
user:conversations:<uid>                ZSET
user:conversation:read:<uid>            HASH
```

`user:conversations` 的 score 是最近更新时间，所以能倒序取最近会话。

异步步骤可能乱序完成，因此 Redis 更新不是无条件覆盖：`updateConversationLastScript` 只接受不小于当前 `last_seq` 的消息，`user:conversations` 使用 `ZADD GT` 只在新 score 更大时推进。MySQL 的 upsert 也使用 `GREATEST(last_seq, VALUES(last_seq))`。这些保护避免旧消息晚完成时把会话摘要倒退，但不能把异步会话元信息变成与 messages 同事务的强一致结果。

发送者发送一条消息后，Redis 中其 `read_seq` 会推进到该消息 seq。

### MySQL 持久状态

Logic 启动一个带 3 秒超时的 goroutine，异步执行：

```text
upsert conversations
upsert conversation_members
```

发送者的 MySQL `read_seq` 推进到新 seq，接收者初始为 0。

这里不是和 `messages INSERT` 同一个事务。进程在消息写入后、会话元信息更新前崩溃时，MySQL 可能出现“消息已存在，但最近会话摘要尚未更新”。当前没有对账修复任务。

## 12. 登录时最近会话怎样读取

用户登录成功后，Logic 最多读取最近 50 个会话。

顺序是：

```text
先查 Redis user:conversations:<uid>
→ 有数据：结合 conversation:last 和 Redis read_seq 返回
→ 没数据：查询 MySQL conversations + conversation_members + messages
→ 把结果回填 Redis
```

返回字段包括：

```text
conversation_id
type
title
last_msg
last_seq
read_seq
unread_count
updated_at
```

未读数当前只是：

```text
max(last_seq - read_seq, 0)
```

这是一种简化模型，不处理撤回、不可见消息、分设备游标等复杂情况。

## 13. 当前 read_seq 的重要边界

接收方对消息 ACK 时，`AckMessage` 会把新的 read seq 写入 Redis：

```text
user:conversation:read:<uid>
```

但是当前 ACK 路径没有把接收方 `read_seq` 回写 MySQL `conversation_members`。

因此：

- Redis 仍在时，最近会话未读数可以使用较新的游标；
- Redis 过期或丢失并回源 MySQL 时，接收方 `read_seq` 可能还是旧值；
- 不能声称已实现持久、跨设备一致的已读状态。

而且第 11 章已经说明：当前 ACK 是客户端收件确认，代码只是借它推进简化 read seq，不等于用户肉眼已读。

## 14. 历史查询怎样工作

页面切换聊天对象时调用：

```text
GET /api/v1/history?target_id=...
```

单聊根据当前 JWT uid 与 target 构造 `c2c` 会话；群聊根据 group ID 构造 `group:` 会话并校验当前成员身份。

MySQL 查询最近 50 条：

```sql
WHERE session_id = ?
ORDER BY seq DESC
LIMIT 50
```

服务端反转后返回旧到新。

当前没有：

- 上拉更多的游标分页；
- 按时间范围搜索；
- 全文检索；
- 消息撤回、编辑、删除；
- 入群前历史可见范围控制。

## 15. Gateway 和独立 Logic 的边界要讲准确

项目的“消息主链路”是：

```text
Gateway 管连接
Logic 管消息校验、seq、持久化和投递编排
Transfer 管群扩散
```

但当前所有 REST 业务并没有都下沉到独立 Logic 服务：

| 功能 | 当前执行位置 |
|---|---|
| 登录、历史 | Gateway 转 gRPC Logic |
| WebSocket 普通消息 | Gateway 转 gRPC Logic |
| 好友申请/列表 | Gateway 进程直接访问 MySQL |
| 创建群/群列表/群成员 | Gateway 进程直接访问 MySQL/Redis |
| 红包、AI HTTP 接口 | Gateway 进程直接访问 MySQL |

所以面试时可以重点讲消息层分层，但不能说“Gateway 完全无业务、永远不访问数据库”。事实上 Gateway 的 readiness 也会检查 MySQL。

## 16. 面试时怎样准确回答

可以这样说：

> messages 只能保存消息正文，完整 IM 还需要好友、群成员和会话关系。我的好友申请与好友关系分表，接受申请时在一个 MySQL 事务里更新申请并写双向关系；Logic 发送单聊前校验 normal 好友，群聊前校验 active 成员和禁言时间。MySQL 是这些授权和群 recipients 的事实源，数据库不可用时会拒绝，不会用可能过期的 Redis 集合降级放行。创建群遇到重复 group_id 会回滚，只有 active 成员才能查成员列表。会话用 c2c:排序后的两个 uid 或 group:gid 标识，conversations 保存 last_seq 和更新时间，conversation_members 保存用户参与关系和 read_seq。登录先读 Redis 最近会话热索引，未命中回源 MySQL。当前会话元信息是消息落库后的异步更新，接收方 ACK read_seq 也只写 Redis，群管理 RBAC 和历史分页还不完整，所以我不会把它夸大成商业级多端已读系统。

## 本章代码阅读任务

| 顺序 | 打开位置 | 这次只看什么 |
| --- | --- | --- |
| 1 | `sql/init.sql` 的 `friend_requests`、`friend_relations`、`im_groups`、`group_members`、`conversations`、`conversation_members` | 对每张表写一句“它保存的事实”，并圈出联合主键或唯一约束 |
| 2 | `cmd/gateway/internal/handler/routes.go` 中好友、群组 Route，再看 `cmd/gateway/internal/logic/friendlogic.go` 的 `Apply`、`Respond` | 找到 JWT uid、申请状态更新、MySQL transaction 和双向关系写入 |
| 3 | `cmd/gateway/internal/logic/groupcreatelogic.go` 的 `Create` | 看 creator 来自 Context、重复 group_id 返回冲突、MySQL commit 在 Redis cache 前、缓存失败只记录日志 |
| 4 | `cmd/gateway/internal/logic/groupmemberslogic.go` 的 `List` | 找到请求者必须是 active 成员的第一条查询，再看第二条查询怎样列成员 |
| 5 | `internal/logic/relations.go` 的 `validateSendPermission`、单聊和群聊校验函数 | 确认 DB 缺失、查询错误和关系不允许都 fail-closed，不回退到 Redis 猜权限 |
| 6 | `internal/logic/handler.go` 的 `buildSessionID`、`resolveRecipients`、`GetHistory` | 对比单聊好友、群成员、历史授权与 recipients 解析 |
| 7 | `internal/logic/conversation.go` 的 `listConversations`、`updateConversationState`、`cacheConversationState`、`persistConversationState` | 看 Redis miss 回 MySQL、`last_seq` 单调保护和异步 3 秒 MySQL 更新 |

看到这个程度就停：你能从一条好友申请推到双向关系，从建群推到成员授权，并能分清 messages、conversation 摘要和成员 read seq。暂时不必学习完整社交产品的邀请审批、踢人、解散群、消息搜索和多设备已读协议。

## 动手练习

用 userA、userB 推演：

1. A 发好友申请。
2. B 接受。
3. 数据库中哪三类行发生变化？
4. A 发第一条消息后，`messages`、`conversations`、`conversation_members` 分别保存什么？
5. B ACK 后 Redis 和 MySQL 的 read seq 是否都更新？

再画一个 G100 群，标出 `im_groups` 一行和 `group_members` 多行的关系。

## 闭卷检查

1. 为什么好友申请不能直接等同于好友关系？
2. 接受申请为什么需要事务和双向关系？
3. Redis route 与 MySQL 好友关系分别回答什么问题？
4. 单聊会话 ID 为什么要排序 uid？
5. `conversations` 与 `messages` 分别保存什么？
6. 登录会话列表的 Redis miss 怎样处理？
7. 当前 `read_seq` 为什么不是可靠的多端已读游标？
8. 群创建当前有哪些权限和一致性缺口？
9. 为什么不能说 Gateway 完全不访问 MySQL？

九个问题能闭卷回答后，再进入第 14 章。

## 动手练习与闭卷检查参考答案

### 动手练习答案

1. A 申请时写入或把 `friend_requests(A,B)` 更新为 pending。
2. B 接受时在一个事务中把这条申请改为 accepted，并写入 `friend_relations(A,B,normal)` 与 `friend_relations(B,A,normal)`。
3. 三类变化是申请流程行、A 指向 B 的关系行、B 指向 A 的关系行。
4. A 发第一条消息后，`messages` 有正文、ID、seq 和会话 ID；`conversations` 异步保存该会话的 last_seq/updated_at；`conversation_members` 异步保存 A、B 参与关系，发送方 A 的 read_seq 可推进，B 初始为 0。
5. B ACK 会推进 Redis `user:conversation:read:B`，当前不会把 B 的接收进度写回 MySQL `conversation_members`。

G100 图中 `im_groups` 只有一行，保存名称、owner 和群状态；每个 owner/admin/member 各有一条 `group_members(group_id,user_id)` 行，保存角色、禁言和成员状态。

### 闭卷检查答案

1. 申请是可 pending/accepted/rejected 的流程历史；好友关系是当前是否允许互动的状态，生命周期和查询目标不同。
2. 双向两行便于每个用户按自己的 uid 查好友；事务让申请状态和两行关系一起成功或回滚，避免单向好友。
3. route 回答 B 当前连接在哪个 Gateway；好友关系回答 A 是否有权给 B 单聊。
4. 对两个 uid 排序后，A 到 B 与 B 到 A 得到同一个 `c2c:min:max`，消息能归入同一会话和 seq 空间。
5. conversations 是会话摘要和 last_seq；messages 是每条具体消息正文与服务端 ID。
6. 先查 Redis `user:conversations:<uid>`；为空时查询 MySQL 会话、成员和最新消息，再尽力回填 Redis。
7. 接收方 ACK 只更新 Redis，刷新或 Redis 丢失会回源旧 MySQL read_seq；当前还是单值路由，也没有 per-device 游标，ACK 本身也不是肉眼已读。
8. 当前不会验证请求中每个成员账号存在，没有完整邀请、退群、踢人和角色 RBAC；MySQL commit 后 Redis 缓存失败只记录日志且无重建任务。重复 group_id 会拒绝，非 active 成员不能列成员。
9. 好友、建群、成员列表、红包和 AI 等 Gateway REST Logic 直接持有并访问 MySQL；只有消息主链路的登录、历史和 WebSocket Push 通过独立 Logic。

下一步：[14 红包并发一致性](14_RED_PACKET.md)
