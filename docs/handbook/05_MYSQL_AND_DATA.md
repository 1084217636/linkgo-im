# 05 MySQL 与最小数据模型

## 本章前置

你已经知道 HTTP 请求怎样进入 Gateway，也知道不同 Go 进程的普通内存互相不可见。

本章不讨论实时推送。我们先解决一个更基础的问题：程序退出以后，账号和聊天记录为什么还能存在？

## 本章目标

读完后，你应该能够：

1. 区分进程内存和持久化数据。
2. 看懂表、行、列、主键、唯一索引和普通索引。
3. 说出 LinkGo 的几张核心表分别保存什么。
4. 沿源码找到一次 MySQL 查询和一次消息写入。
5. 准确说出当前数据库实现的边界。

## 1. 为什么不能只用 Go 变量保存数据

下面的 map 只属于当前进程：

```go
users := map[string]string{
    "userA": "1001",
}
```

进程重启后，map 中的内容消失；另一台机器上的进程也看不到它。因此账号、好友关系和消息历史需要写入能够长期保存数据的系统。

MySQL 是一个关系型数据库。这里的“关系”可以先理解为：数据按表组织，不同表通过用户 ID、会话 ID 等字段建立联系。

在 LinkGo 中，MySQL 是聊天历史等业务事实的最终来源。缓存丢失不应该让已经成功保存的历史消息永久消失。

为什么选择 MySQL，而不是继续使用 map 或直接写普通文件？账号、好友关系、群成员和消息不仅要长期保存，还需要按条件查询、防止重复，并在并发修改时保持一致。MySQL 提供索引、唯一约束和事务；事务可以先理解为“让一组相关操作整体成功或整体失败”。这些能力与 LinkGo 中结构明确、关系较多的数据相匹配。

代价是每次访问都可能包含网络和磁盘开销，表结构、索引、连接数、备份及高可用也需要维护。MySQL 负责保存业务事实，不负责维护 WebSocket 连接位置或直接把消息推给在线用户。当前应用只配置一个稳定数据库入口，也没有在代码中完成读写分离。

## 2. 表、行和列

把 `users` 表想成一张有固定表头的表格：

| id | user_id | username | password | status |
| --- | --- | --- | --- | --- |
| 1 | 1001 | userA | bcrypt 哈希 | 1 |
| 2 | 1002 | userB | bcrypt 哈希 | 1 |

- 表（table）：一类数据的集合，例如用户表。
- 行（row）：一个具体对象，例如 userA。
- 列（column）：对象的一个属性，例如 username。
- 表结构（schema）：表有哪些列、每列类型和约束是什么。

项目的建表语句在 `sql/init.sql`。第一次阅读只看表名、字段和索引，不用背所有 SQL 语法。

## 3. 主键、唯一约束和索引

### 主键

主键用于唯一标识一行。`users.id` 是数据库内部自增主键；`users.user_id` 是对外使用的业务用户 ID。

### 唯一约束

唯一约束禁止出现重复值。例如：

```sql
username VARCHAR(32) NOT NULL UNIQUE
```

表示两个用户不能使用相同登录名。

消息表还有两个重要唯一约束：

```text
(from_uid, client_msg_id)
(conversation_id, seq)
```

现在只需知道它们防止重复；消息 ID 和序号的完整含义在后续可靠性章节解释。

### 普通索引

索引像书的目录，用额外空间换取更快查找。例如历史消息按会话和序号查询，对应：

```sql
INDEX idx_session_seq (session_id, seq)
```

索引不是越多越好。写入一行时还要维护索引，因此索引会占磁盘并增加写入成本。

## 4. SQL 和参数化查询

SQL 是操作关系型数据库的语言。常见动作：

```text
SELECT  查询
INSERT  插入
UPDATE  更新
DELETE  删除
```

登录查询在 `internal/logic/handler.go` 中：

```sql
SELECT user_id, password, status
FROM users
WHERE username = ?
LIMIT 1
```

问号是参数占位符。程序把 username 作为参数交给驱动，不把用户输入直接拼进 SQL，这也是防止 SQL 注入的基础做法。

## 5. LinkGo 的核心数据模型

先掌握下面六类，不要一次背完整 `init.sql`。

### users：用户账号

保存登录名、用户 ID、密码哈希、头像和账号状态。

### messages：最终消息历史

关键字段：

```text
message_id       服务端消息 ID
client_msg_id    客户端一次发送的 ID
conversation_id  会话 ID
session_id       当前实现中与 conversation_id 保存相同值
seq              会话内序号
from_uid         发送者
to_id / to_type  接收用户或群
content          正文
create_time      创建时间
```

`conversation_id` 和 `session_id` 当前同时存在，是项目迭代后的兼容结果，不要解释成两个不同业务对象。

### conversations：会话摘要

保存会话类型、最近更新时间和最新序号。它不是完整消息正文表。

### conversation_members：用户属于哪些会话

保存用户与会话的关系以及 MySQL `read_seq/acked_seq`。`read_seq` 是用户真正阅读位置，`acked_seq` 是客户端可靠收到并 ACK 的位置。ACK 会单调推进 Redis 快速游标，并尝试回写 MySQL `acked_seq`；它仍不是多端肉眼已读系统。第 13 章会把两套游标放在一起解释。

### friend_relations / group_members：发送权限

单聊会检查正常好友关系，群聊会检查发送者是否仍是有效群成员。数据存在不代表任何用户都有权读取或发送。

### red_packets 和 AI 表

它们属于后续业务章节。现在只需知道红包、AI 结果和调用记录也会持久化，不需要提前学习表结构。

## 6. 数据库连接和连接池

Go 使用 `database/sql` 访问 MySQL：

```go
db, err := sql.Open("mysql", dsn)
```

DSN 是数据库连接字符串，包含用户名、密码、网络地址、数据库名等信息。

每次 SQL 都重新建立网络连接成本很高，因此 `sql.DB` 管理连接池：复用一组数据库连接，并限制最大连接数。Logic 的默认配置是最多 100 个打开连接；Gateway 的默认配置是最多 80 个。

这不表示每个请求固定占一个新连接，也不表示连接数越多越快。公司部署多个实例时，要计算：

```text
实例数 × 每实例 MaxOpenConns
```

不能超过数据库能够承受的连接总量。

## 7. 为什么 Gateway 和 Logic 都连接 MySQL

理想化架构图常写“Logic 负责数据库”，但当前源码需要更准确地说：

- 独立 Logic 服务负责登录、消息持久化、历史查询和会话数据。
- Gateway 进程中的好友、建群、红包和 AI HTTP 接口也直接持有数据库连接。

证据在两个 `ServiceContext`：

```text
cmd/logic/internal/svc/servicecontext.go
cmd/gateway/internal/svc/servicecontext.go
```

因此不能笼统声称“Gateway 完全不访问数据库”。Gateway 不负责消息核心落库，但当前部分外围业务确实在 Gateway 进程内执行。

## 8. 本地地址与公司多服务器地址

本地配置可以直接连接：

```text
mysql:3306
```

公司环境通常让所有应用实例连接同一个稳定数据库入口，例如主库域名或数据库代理地址。入口后面可以有主库、只读副本和故障切换机制。

需要区分：

- 主库：接受事务写入的节点。
- 只读副本：复制主库数据，可承接允许短暂延迟的读取。
- 数据库代理/稳定入口：给应用一个不随后台节点切换而改变的地址。

当前 LinkGo 只有一个 `DB_DSN`，没有在应用代码中实现读写分离。消息写入、红包事务和历史查询都使用这一入口。面试时可以描述入口后面的高可用设计，但不能说代码已经把读取自动分配到从库。

## 9. 一次历史查询怎样发生

当前历史接口最终执行：

```sql
SELECT ...
FROM messages
WHERE session_id = ?
ORDER BY seq DESC
LIMIT 50
```

程序按 cursor 取最新页或 `seq < before_seq` 的上一页，再在内存中翻转为从旧到新的顺序返回。

注意两个边界：

1. 当前接口默认返回 50 条、最大 100 条，使用 `before_seq` 和 `has_more/next_before_seq` 游标，不使用深 OFFSET。
2. WebSocket 重连先使用 Redis 短期数据，再按 `seq > last_seq` 从 MySQL 分批兜底；历史 HTTP 与重连共用相同的 cursor 思路。

## 10. 消息表和会话表是不是一个事务写入

不是。

当前 `saveMessage` 先向 `messages` 执行单条 `INSERT`。之后 `updateConversationState` 更新会话缓存，并启动 goroutine，以最多三秒的独立上下文尽力写入 `conversations` 和 `conversation_members`。

所以准确口径是：

> 消息正文同步写入 MySQL；会话摘要随后更新，MySQL 会话元信息目前是异步尽力写入，不与消息 INSERT 处在同一个数据库事务中。

如果异步会话更新失败，消息历史仍可能已经存在，但登录时的会话摘要可能暂时不完整。这是当前实现边界，不应写成“消息和会话元信息原子提交”。

## 本章代码阅读任务

| 顺序 | 打开位置 | 这次只看什么 |
| --- | --- | --- |
| 1 | `sql/init.sql` 的 `users`、`messages`、`conversations`、`conversation_members` 建表段 | 先圈出主键、两个消息唯一约束和 `idx_session_seq`，不背字段类型 |
| 2 | `cmd/logic/internal/svc/servicecontext.go` 的 `ServiceContext`、`NewServiceContext` | 找到 `sql.Open`、`SetMaxOpenConns`、`SetMaxIdleConns`，理解一个 `sql.DB` 是连接池句柄 |
| 3 | `internal/logic/handler.go` 的 `Login`、`GetHistory`、`saveMessage` | 各找一条参数化 `SELECT` 和 `INSERT`，确认占位符参数没有字符串拼接 |
| 4 | 同文件的 `PushMessage`、`deliverPersistedMessage`，再到 `internal/logic/conversation.go` 的 `updateConversationState`、`persistConversationState` | 按调用顺序确认消息同步写入，会话 MySQL 摘要由 goroutine 异步尽力更新 |
| 5 | `cmd/gateway/internal/svc/servicecontext.go` 的 `ServiceContext`、`NewServiceContext` | 确认 Gateway 也有 `DB` 字段，避免把当前实现讲成 Gateway 完全不访问 MySQL |

看到这个程度就停：你能指出四张表的职责，能沿 `PushMessage -> saveMessage -> deliverPersistedMessage -> updateConversationState` 说清同步和异步边界。暂时不必掌握 InnoDB B+ 树内部实现、隔离级别细节、执行计划成本模型或数据库高可用搭建。

## 动手练习

### 练习一：找索引

回答下面的查询最可能使用哪个索引：

```sql
SELECT * FROM messages
WHERE session_id = ? AND seq > ?
ORDER BY seq
LIMIT 50;
```

答案应定位到 `idx_session_seq(session_id, seq)`。

### 练习二：计算连接上限

假设 6 个 Logic 实例，每个 `MaxOpenConns=80`，仅 Logic 理论最大连接数是多少？答案是 480。然后解释为什么这只是上限，不是正常时始终存在 480 条连接。

### 练习三：核对真实写入顺序

从 `LogicHandler.PushMessage` 找到 `saveMessage`，再找到 `deliverPersistedMessage` 和 `updateConversationState`。画出三者先后关系，标出哪一步同步返回错误、哪一步启动 goroutine。

## 闭卷检查

1. 为什么账号和历史消息不能只存 Go map？
2. 主键、唯一约束和普通索引分别解决什么问题？
3. `messages` 与 `conversations` 各保存什么？
4. `conversation_id` 和 `session_id` 在当前实现中是什么关系？
5. 为什么查询参数不能直接拼接进 SQL？
6. 连接池的作用是什么？
7. 当前 Gateway 是否完全不访问 MySQL？
8. 当前消息和会话元信息是否在同一个事务中？
9. 当前历史接口如何使用 `before_seq` 和 `limit` 分页？服务端最大允许多少条？
10. 公司部署多个 MySQL 节点时，应用为什么仍可只配置一个稳定入口？
11. 为什么核心业务事实选择 MySQL，它不能替代哪些实时能力？

能把第 7、8、9 题准确回答后，再进入登录章节。

## 动手练习与闭卷检查参考答案

### 动手练习答案

1. 查询条件和排序都以 `session_id` 开头，再按 `seq` 过滤和排序，最匹配 `idx_session_seq(session_id, seq)`。是否实际使用仍应以真实数据上的 `EXPLAIN` 为准。
2. 理论上限是 `6 x 80 = 480`。`MaxOpenConns` 是允许同时打开的最大值，连接池按请求量建立和复用连接，空闲或低流量时不必始终保持 480 条。
3. 首次消息在 `PushMessage` 中同步调用 `saveMessage`。成功后进入 `deliverPersistedMessage`，完成 recipients 和投递编排，再调用 `updateConversationState`。Redis 会话热状态在当前调用中更新；`updateConversationState` 内启动 goroutine，用独立 3 秒上下文调用 `persistConversationState` 写 MySQL 会话摘要。`saveMessage` 或投递错误会同步返回，异步会话写失败只记录日志。

### 闭卷检查答案

1. map 属于单进程内存，进程重启会丢失，其他实例也不能共享；账号、关系和历史需要持久事实源。
2. 主键唯一标识一行；唯一约束阻止特定业务组合重复；普通索引加速常用查询，但会占空间并增加写维护成本。
3. `messages` 保存每条消息正文和 ID、seq；`conversations` 保存会话级 `last_seq`、类型和更新时间摘要。
4. 当前聊天消息把两者保存成同一个会话标识，是兼容字段，不是两个独立业务会话。
5. 参数化查询让驱动把用户输入当数据，而不是 SQL 结构，降低 SQL 注入风险。
6. 连接池复用数据库连接并限制总打开数，避免每个请求重新握手，也避免应用无限占用数据库连接。
7. 不是。Gateway 的好友、群组、红包和 AI HTTP 逻辑也直接使用 `ServiceContext.DB`。
8. 不在。`messages INSERT` 同步完成，会话 Redis 热状态随后更新，MySQL 会话摘要在独立 goroutine 中尽力写入。
9. 默认返回 50 条，最大 100 条；最新页查询 51 条判断 `has_more`，下一页使用 `seq < next_before_seq`，不使用深 `OFFSET`。
10. 应用连接代理、VIP 或托管数据库提供的稳定域名；入口后面可以完成主从切换。当前代码仍只有一个 DSN，也没有应用层读写分离。
11. 结构稳定的消息、关系和红包需要事务、唯一约束、索引和长期查询，MySQL与这些需求匹配。它不保存 WebSocket 对象，也不替代在线路由、低延迟通知和短期待确认状态。

下一步：[06 登录、密码与 JWT](06_LOGIN_AND_JWT.md)

## 可靠性升级后的必读变化

本轮新增了 `conversation_members.acked_seq`。`read_seq` 是用户真正阅读到的位置，`acked_seq` 是客户端可靠收到并 ACK 到的位置；ACK 不能再被解释成“用户已读”。历史查询也从固定 50 条升级为 `before_seq + limit` 游标分页，服务端默认 50、最大 100，并通过多查一条判断 `has_more`。这两个字段和 cursor 必须回到 `internal/logic/conversation.go`、`internal/server/ack.go`、`internal/logic/handler.go` 逐个符号核对。
