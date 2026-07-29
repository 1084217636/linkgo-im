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
6. 当前会话与群组功能有哪些一致性和权限缺口？

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

所以 Redis route 只回答“B 在线哪里”，不回答“A 有没有权利给 B 发消息”。权限事实来自 MySQL 好友关系。

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

## 7. 当前创建群的真实链路

创建群也是 Gateway REST 逻辑直接访问 MySQL：

```text
POST /api/v1/group/create
→ JWT 得到 creator_id
→ 把 creator 自动加入成员集合
→ MySQL 事务 upsert im_groups
→ 事务内 upsert 每个 group_members
→ commit
→ 再把成员写入 Redis group_members:<gid>
→ 再写 user_groups:<uid>
```

MySQL 是正常路径的群成员事实来源；Redis 集合用于兼容和加速。

当前代码需要诚实说明几个缺口：

- 没有检查请求中的每个成员用户是否真实存在；
- 对已存在的相同 `group_id`，没有先验证当前调用者是原 owner，就会更新群名和状态；
- MySQL 已 commit 后 Redis 更新失败时，接口可能返回错误，但数据库已经成功，没有补偿任务；
- 没有完整的邀请审批、退群、踢人、解散群管理接口；
- 查询群成员列表的 REST 接口目前只要求登录，没有校验请求者本人属于该群。

因此它是基本群关系原型，不是完整商业群管理系统。

## 8. 群消息发送与历史权限

群消息发送前，Logic 查询发送者的成员行：

```text
status 必须是 active
mute_until 必须已经到期，或者为 0
```

群消息的 recipients 来自全部 active 成员，发送者本人被排除。

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

> messages 只能保存消息正文，完整 IM 还需要好友、群成员和会话关系。我的好友申请与好友关系分表，接受申请时在一个 MySQL 事务里更新申请并写双向关系；Logic 发送单聊前校验 normal 好友，群聊前校验 active 成员和禁言时间。会话用 c2c:排序后的两个 uid 或 group:gid 标识，conversations 保存 last_seq 和更新时间，conversation_members 保存用户参与关系和 read_seq。登录先读 Redis 最近会话热索引，未命中回源 MySQL。当前会话元信息是消息落库后的异步更新，接收方 ACK read_seq 也只写 Redis，群管理权限和历史分页还不完整，所以我把它描述为关系与会话基本闭环，不夸大成商业级多端已读系统。

## 代码锚点

按顺序阅读：

1. `sql/init.sql`：`friend_*`、`im_groups`、`group_members`、`conversations`、`conversation_members`。
2. `cmd/gateway/internal/logic/friendlogic.go`：好友申请和事务。
3. `cmd/gateway/internal/logic/groupcreatelogic.go`：创建群和 Redis 更新。
4. `internal/logic/relations.go`：消息发送权限与群 recipients。
5. `internal/logic/conversation.go`：会话 Redis/MySQL 更新与登录列表。
6. `internal/logic/handler.go`：会话 ID 和历史查询。
7. `cmd/gateway/internal/handler/routes.go`：这些 REST 接口在哪里注册。

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

下一步：[14 红包并发一致性](14_RED_PACKET.md)
