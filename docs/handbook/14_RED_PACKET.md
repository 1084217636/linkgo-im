# 14 红包并发一致性：很多人同时领取时怎样不超发

## 本章前置

你已经学过 HTTP、JWT、MySQL 表、事务和聊天会话。如果“事务”还不熟，可以先把它理解成：一组数据库操作要么全部成功，要么全部撤销。

本章只讨论当前代码中的等额红包账本模型，不把它包装成真实支付系统。

## 本章目标

学完后，你必须能回答：

1. 为什么红包金额使用整数“分”，不能使用 `float64`？
2. 红包主表和领取明细表分别保存什么？
3. `SELECT ... FOR UPDATE` 为什么能防止并发超发？
4. 有行锁后为什么仍然需要唯一索引？
5. 顺序重试和真正并发的重复领取，当前响应为什么不同？
6. 为什么当前红包不能称为资金系统？

## 1. 先定义当前功能边界

当前红包支持：

```text
创建红包
→ 在聊天中发送一条包含红包 ID 的文本提示
→ 会话成员领取
→ 查询红包与领取明细
```

当前红包不支持：

```text
发送者钱包扣款
接收者余额入账
冻结金额
资金流水
过期退款
对账
真实支付渠道
```

因此它证明的是 MySQL 事务、行锁、唯一约束和幂等，不证明金融支付能力。

## 2. 为什么金额以“分”为单位

代码使用 `int64`：

```text
100 = 1.00 元
1   = 0.01 元
```

浮点数不能精确表示所有十进制小数。例如某些运算中的 `0.1 + 0.2` 不会严格等于十进制 `0.3`。金额使用最小货币单位的整数，可以让加减和守恒检查更明确。

当前红包创建还要求：

```text
total_amount > 0
total_count > 0
total_amount >= total_count
total_count <= 1000
```

`total_amount >= total_count` 保证每一份至少 1 分。

## 3. 两张核心表

### `red_packets`：红包主状态

主要字段：

```text
id
sender_id
conversation_id
to_type
total_amount / total_count
remaining_amount / remaining_count
greeting
status = active / finished / expired
created_at / expires_at / updated_at
```

一只红包只有一行主记录。所有领取者竞争的就是这一行中的剩余金额和份数。

### `red_packet_claims`：每次领取结果

主要字段：

```text
red_packet_id
user_id
amount
created_at
```

关键唯一索引：

```text
UNIQUE(red_packet_id, user_id)
```

它从数据库约束层保证同一个用户对同一红包最多只有一条领取记录。

## 4. 创建红包的真实路径

红包 REST 接口在 Gateway 进程直接操作 MySQL：

```text
POST /api/v1/red-packets
→ JWT 得到 sender_id
→ 根据 target 构造或校验 conversation_id
→ 单聊校验双方是 normal 好友
   或群聊校验发送者是 active 成员
→ 校验金额、份数和过期时间
→ INSERT red_packets，初始 remaining = total
→ 返回 red_packet_id
```

群成员校验代码也读取 `mute_until`，意图是拒绝仍在禁言期的成员；但当前函数在“active 且仍被禁言”分支会把 nil 错误返回，实际会放行。这是第 10 节会再次标出的实现缺陷，不能把“红包已完成禁言控制”写进简历。

默认过期时间是创建后 24 小时。

创建本身只写红包主表，不扣除任何钱包余额。因此用户可以创建任意满足参数范围的“演示金额”，这也是它不能称资金系统的直接原因。

## 5. 当前等额分配算法

假设总金额 100 分、总份数 3：

```text
base      = 100 / 3 = 33
remainder = 100 % 3 = 1
```

前 `remainder` 个领取者多 1 分：

```text
第 1 份 34 分
第 2 份 33 分
第 3 份 33 分
合计     100 分
```

最后一份直接领取全部 `remaining_amount`，避免整数除法或中间异常留下尾差。

当前不是随机“拼手气”红包。它是确定性的普通等额红包。

## 6. 抢红包事务逐步拆解

领取入口：

```text
POST /api/v1/red-packets/claim
```

Gateway 先读取红包详情并校验当前用户是该单聊会话参与者或当前群成员，然后调用 `RedPacketService.Claim`。

Claim 的主要流程是：

```text
1. 事务外先查询该用户是否已有 claim
2. BeginTx，隔离级别 Read Committed
3. SELECT red_packets ... FOR UPDATE
4. 检查 status、expires_at、remaining
5. 计算本次 amount
6. INSERT red_packet_claims
7. UPDATE red_packets 扣减 amount 和 count
8. 最后一份时把 status 改为 finished
9. COMMIT
```

事务中的领取明细和主表扣减要一起成功。如果中间 SQL 失败，defer 的 Rollback 会撤销已执行但未提交的修改。

## 7. `FOR UPDATE` 到底锁住什么

下面的查询不是普通读取：

```sql
SELECT ...
FROM red_packets
WHERE id = ?
FOR UPDATE;
```

InnoDB 在事务提交或回滚前锁住命中的红包行。

假设 C、D 同时抢最后一份：

```text
C 事务先获得红包行锁
→ D 等待
→ C 看到 remaining_count=1
→ C 插入 claim、扣成 0、commit
→ D 获得锁
→ D 重新读到 finished/remaining=0
→ D 不能再领取
```

如果没有锁，两人可能同时读到“还剩 1 份”，都继续扣减。

当前 UPDATE 还带条件：

```sql
WHERE id = ?
  AND remaining_amount >= ?
  AND remaining_count > 0
```

并检查影响行数是否为 1。这是额外的防御，但当前主要串行化手段仍是红包主行锁。

## 8. 有行锁为什么还要唯一索引

行锁保护的是红包总剩余值，唯一索引保护的是“同一个用户不能重复领取”。两者约束不同。

可能发生两个相同用户请求并发进入：

```text
请求 1 事务外查询：没有 claim
请求 2 事务外查询：也没有 claim
```

即使它们随后按顺序拿到红包主行锁，只有应用代码检查很容易因为修改或遗漏产生重复。数据库唯一索引提供最终约束：第二条相同 `(red_packet_id, user_id)` 插入会报重复键。

标准回答：

> 行锁保证红包总量并发扣减正确，唯一索引保证单用户领取幂等，它们不是替代关系。

## 9. 重复请求怎样返回

Claim 一开始先查询已有领取记录。

如果找到：

```text
返回原 red_packet_id、user_id、amount、created_at
同时给出 ErrRedPacketAlreadyClaimed
```

Gateway 把它转换成正常业务响应：

```json
{
  "already_claimed": true,
  "status": "already_claimed",
  "amount": 之前领取的金额
}
```

对于前一次请求已经提交、下一次请求在事务外预查询命中的顺序重试，客户端能看到同一个结果，而不是再领一份。

但两个相同用户请求真正并发时，二者可能都在预查询阶段看不到记录。后拿到锁的请求会触发唯一键冲突，当前 Service 返回 `ErrRedPacketAlreadyClaimed` 但没有携带原 claim，Gateway 会返回错误，而不会再次查询并转换成上面的正常 `already_claimed` 响应。这是响应语义缺口；数据库仍能保证不会多领。

这里的幂等键不是额外的 request ID，而是业务唯一组合：

```text
red_packet_id + user_id
```

## 10. 会话权限怎样校验

### 创建单聊红包

- 不能发给自己；
- `conversation_id` 必须与发送者和目标用户匹配；
- 目标必须是 normal 好友。

### 创建群红包

- 发送者必须是 active 群成员；
- 代码读取了 `mute_until`，但当前 `validateActiveGroupMember` 在“active 且仍被禁言”分支最终返回了 `nil`，所以实际不会拒绝该用户。这是实现缺陷，不能把“禁言成员不能发红包”说成已完成。

### 领取或查详情

- 单聊：当前 uid 必须出现在 `c2c:uid1:uid2` 中；
- 群聊：当前 uid 必须是 active 成员。

当前单聊领取并不会限制“只能接收方领取”：发送者也是会话参与者，也能调用 Claim。群红包发送者也属于群成员，因此也没有被显式排除。这是当前业务规则，而不是微信红包规则的完整复刻。

## 11. 红包状态和聊天消息为什么分开

红包事实保存在 `red_packets` 与 `red_packet_claims`。聊天消息只需要引用 `red_packet_id`。

这是因为：

- 红包剩余金额会变化；
- 聊天消息历史不应该随着每次领取被反复改写；
- 客户端点击红包时应实时查询红包域状态。

不过当前 Protobuf `MsgType` 只有 NORMAL、HEARTBEAT、ACK、SYSTEM，没有结构化 `RED_PACKET` 类型。

网页创建成功后只是自动发送一条普通文本：

```text
[红包] 恭喜发财 1.00 / 3份 id=rp-...
```

因此当前前端是可操作调试入口，不是完整红包卡片协议。

## 12. 过期处理的当前代码边界

领取时会比较 `expires_at`。过期红包会拒绝领取。

但是当前代码在事务内执行 `markRedPacketExpired` 后立即返回错误，defer 随后回滚事务，所以这个状态更新不会真正提交。也没有后台定时任务批量把 active 改成 expired。

结果是：

- 过期后 Claim 会被拒绝；
- 数据库 `status` 可能仍显示 active；
- 没有自动退款，因为本来就没有钱包扣款；
- 详情接口本身不会根据当前时间动态改写状态。

这是需要修复的真实缺口，不能说已经完成“24 小时过期退款闭环”。

## 13. 热点红包为什么会变慢

同一红包的所有领取事务都竞争同一行锁：

```text
很多并发请求
→ 在一行上排队
→ 数据正确，但吞吐受限
```

当前设计适合学习一致性和中小规模演示。

真正高并发红包可以演进为：

```text
创建时预拆分份额
→ Redis Lua 原子抢领取资格
→ 异步落 MySQL 资金/领取流水
→ 定时对账与补偿
```

但这会引入 Redis 与数据库一致性、资格成功但入账失败、补偿重试等新问题。它是演进方案，不是当前实现。

## 14. 测试证据应该怎样描述

`internal/logic/redpacket_test.go` 当前使用 `sqlmock` 验证：

- 100 分 3 份分配为 34、33、33；
- 创建 SQL 的初始值；
- Claim 的 `BEGIN → FOR UPDATE → INSERT → UPDATE → COMMIT` 顺序；
- 事务外预查询已存在 claim 时返回已有结果；这不覆盖两个并发请求同时预查询 miss 后的唯一键冲突响应。

这些是单元级 SQL 行为证据，不是真实 MySQL 上几百 goroutine 并发抢红包的集成压测。

因此可以说“代码使用行锁和唯一索引，并有 SQL 流程单测”，不能说“已通过生产级高并发红包压测”。

## 15. 当前实现与资金系统的差距

| 当前红包模型 | 真实资金系统还需要 |
|---|---|
| 创建一行红包记录 | 校验余额、冻结或扣款 |
| claim 记录金额 | 接收账户入账 |
| 主行剩余值 | 双边资金流水与总账 |
| 业务唯一索引 | 请求幂等号、支付单状态机 |
| 过期时间字段 | 定时关闭、退款任务 |
| SQL 事务 | 跨服务一致性、补偿和对账 |
| 基础日志 | 完整审计、风控、合规 |

## 16. 面试时怎样准确回答

可以这样说：

> 我实现的是等额红包并发业务模型，不是真实支付系统。金额使用 int64 的分，主表保存总额和剩余值，领取表用 red_packet_id + user_id 唯一索引。领取事务在 Read Committed 下 SELECT FOR UPDATE 锁住红包主行，插入领取明细后条件扣减剩余金额和份数，最后一份把状态改为 finished；行锁保护总量，唯一索引保证同一用户不会多领。顺序重试预查询命中时会返回原领取结果，但真正并发的重复请求在唯一键冲突后目前返回业务错误，这是待完善边界。当前没有钱包扣款、入账、退款、流水和对账，过期状态提交也有待修复，前端只是普通文本引用红包 ID，所以我不会把它描述成资金系统。

## 代码锚点

按顺序阅读：

1. `sql/init.sql`：`red_packets` 与 `red_packet_claims`。
2. `cmd/gateway/internal/logic/redpacketlogic.go`：身份、会话权限和响应转换。
3. `internal/logic/redpacket.go`：Create、Claim、Detail 和分配算法。
4. `cmd/gateway/internal/handler/redpackethandler.go`：HTTP 入口。
5. `public/index.html`：搜索 `createRedPacket`、`claimRedPacket`、`sendRedPacketNotice`。
6. `internal/logic/redpacket_test.go`：当前测试到底验证了什么。

## 动手练习

用 100 分、3 份、C 和 D 同时领取最后一份画时序图。必须标出：

```text
BeginTx
FOR UPDATE 获锁/等待
INSERT claim
UPDATE remaining
COMMIT
```

然后回答：如果同一用户发出两个并发请求，行锁与唯一索引分别在哪一步发挥作用？

## 闭卷检查

1. 为什么金额不使用 `float64`？
2. 主表和领取表的职责分别是什么？
3. 100 分 3 份当前怎样分配？
4. `FOR UPDATE` 的锁保持到什么时候？
5. 行锁与唯一索引分别保护什么？
6. 哪种顺序重试会返回原金额？并发唯一键冲突为什么目前只返回错误？
7. 当前哪些用户可以领取单聊红包？
8. 当前过期状态处理有什么缺口？
9. 为什么不能把项目红包写成真实支付系统？
10. 当前测试是不是多线程真实 MySQL 压测？

十个问题能闭卷讲清楚后，再进入第 15 章。

下一步：[15 AI 虚拟好友](15_AI_BOT.md)
