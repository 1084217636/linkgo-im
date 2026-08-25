# 附录 A 钱包与红包并发一致性

## 本章前置

你已经学过 HTTP、JWT、MySQL 表、事务和聊天会话。如果“事务”还不熟，可以先把它理解成：一组数据库操作要么全部成功，要么全部撤销。

本章讨论当前代码中的“模拟充值 + 站内钱包 + 等额红包”最小资金闭环，但不把它包装成真实支付系统。

## 本章目标

学完后，你必须能回答：

1. 为什么红包金额使用整数“分”，不能使用 `float64`？
2. 红包主表和领取明细表分别保存什么？
3. `SELECT ... FOR UPDATE` 为什么能防止并发超发？
4. 有行锁后为什么仍然需要唯一索引？
5. 顺序重试和真正并发的重复领取，当前怎样返回同一结果？
6. 外部支付、站内钱包和红包转账之间是什么关系？

## 1. 先定义当前功能边界

当前红包支持：

```text
模拟微信/支付宝充值订单
→ 站内钱包入账并记录流水
→ 发红包时扣发送者余额
创建红包
→ 在聊天中发送一条包含红包 ID 的文本提示
→ 会话成员领取并入账
→ 过期后按请求退回未领取余额
→ 查询钱包、流水、红包与领取明细
```

当前红包不支持：

```text
真实微信/支付宝签名、回调和退款
银行卡或支付账户提现
自动定时扫描所有过期红包
对账
风控、限额、实名和支付合规
```

因此它证明的是单库事务下的钱包扣款、领取入账、流水幂等和红包并发一致性，不证明真实支付渠道或金融级资金系统能力。

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

## 3. 五张核心表

### `wallet_accounts` 与 `wallet_ledger`

`wallet_accounts` 保存用户当前可用余额；`wallet_ledger` 保存每次充值、发红包、领取和退款后的不可变流水。流水通过 `(user_id, business_type, business_id)` 唯一键避免同一业务重复入账。

### `mock_payment_orders`

保存本地模拟微信/支付宝充值订单和客户端 `request_id`。它只用于演示“外部支付成功后给站内钱包入账”，不调用真实支付 SDK。

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
   或群聊校验发送者是 active 且当前未被禁言
→ 校验金额、份数和过期时间
→ BeginTx
→ 创建并锁定发送者 wallet_accounts
→ 检查余额并扣减 total_amount
→ 写入 red_packet_send 钱包流水
→ INSERT red_packets，初始 remaining = total
→ Commit
→ 返回 red_packet_id
```

为什么这里要检查 `mute_until`？禁言限制的是成员向群里创建新内容，群红包也是一种群内发送行为。如果只检查 `status=active`，被禁言的成员就能绕过群聊发送限制发红包。

但“是否还在群里”和“是否允许创建群内内容”是两条不同的权限。当前代码因此把它们拆成了两个校验：创建群红包要求 active 且未在禁言期；领取和查询详情只要求 active。这样禁言成员不能发新红包，但仍然可以领取已有红包和查看红包记录。

默认过期时间是创建后 24 小时。

创建红包和钱包扣款处于同一个 MySQL 事务：余额不足时不会创建红包；红包 INSERT 失败时钱包扣款与流水也一起回滚。

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

为什么显式使用 Read Committed？它保证不会读取其他事务尚未提交的数据，并让本次事务按已提交状态继续判断；但“不会超发”的核心并不是隔离级别这个名字，而是所有领取都对同一个红包主键执行 `FOR UPDATE`，并把读取、claim 和扣减放在一个事务中。换成某个更高隔离级别也不会自动替代行锁和条件更新。代价是同一热点红包仍要等待同一行锁，而且普通查询不具备可重复读快照语义。

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

两个相同用户请求真正并发时，二者可能都在事务外预查询阶段看不到记录。后续请求插入领取明细若触发 MySQL 1062 重复键，Service 会先回滚当前事务，再从已提交的 `red_packet_claims` 读取胜出记录，连同 `ErrRedPacketAlreadyClaimed` 返回。Gateway 因而仍能转换成同一个正常 `already_claimed` 响应。数据库唯一索引负责阻止多领，回读则让并发重复与顺序重试的响应语义一致。

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
- `mute_until` 大于当前毫秒时间戳时，创建会被拒绝；
- 成员不存在或 `status` 不是 active 时，同样会被拒绝。

### 领取或查详情

- 单聊：当前 uid 必须出现在 `c2c:uid1:uid2` 中；
- 群聊：当前 uid 必须是 active 成员，禁言状态不影响领取或查询。

这里不检查 `mute_until`，因为禁言是“禁止发布”而不是“移出群聊”。如果复用创建权限校验，禁言成员会被错误禁止查看已有红包，也无法领取已有红包。

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

Claim 成功时会在同一个事务内给领取者钱包入账并写 `red_packet_claim` 流水。Claim 观察到过期时，会把未领取金额退回发送者钱包、写 `red_packet_refund` 流水、标记 expired 并提交，然后返回 `ErrRedPacketExpired`。发送者也可调用退款接口主动结算已经过期的红包。

当前边界是：

- 过期后 Claim 会被拒绝；
- 至少有一次 Claim 观察到过期后，数据库 `status` 会提交为 expired；
- 没有人调用 Claim 或退款接口时，没有后台定时任务主动扫描，status 仍可能暂时是 active；
- `POST /red-packets/refund` 只允许发送者对已经过期的红包执行幂等退款；
- 详情接口本身不会根据当前时间动态改写状态。

因此可以说已经实现“触发式过期退款”，不能说已经完成“24 小时定时自动退款”。

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
- 创建红包时钱包锁定、扣款、流水和红包 INSERT 位于同一事务；
- Claim 的 `BEGIN → FOR UPDATE → INSERT claim → UPDATE packet → 钱包入账 → COMMIT` 顺序；
- 事务外预查询已存在 claim 时返回已有结果；
- 并发重复触发唯一键冲突时，回滚并回读已有领取结果；
- 领取时观察到 expired 会退款并提交，余额不足不会创建红包。

这些是单元级 SQL 行为证据，不是真实 MySQL 上几百 goroutine 并发抢红包的集成压测。

因此可以说“代码使用行锁和唯一索引，并有 SQL 流程单测”，不能说“已通过生产级高并发红包压测”。

## 15. 当前实现与真实支付系统的差距

| 当前钱包红包 Demo | 真实支付系统还需要 |
|---|---|
| 模拟充值立即 paid | 渠道签名、异步回调验签、订单状态机 |
| 单库钱包余额与流水 | 总账、分户账、日切与资金对账 |
| 发红包扣款、领取入账 | 冻结/解冻、提现、渠道退款 |
| 触发式过期退款 | 定时扫描与可靠退款任务 |
| SQL 事务 | 跨服务一致性、补偿和对账 |
| 基础日志 | 完整审计、风控、合规 |

## 16. 面试时怎样准确回答

可以这样说：

> 我实现的是站内钱包红包 Demo，不是真实微信或支付宝支付系统。模拟充值订单代表外部渠道已经支付成功，随后给站内钱包入账；发红包时在同一个 MySQL 事务里锁定发送者钱包、扣余额、写流水并创建红包。领取时锁定红包主行，通过唯一索引阻止同一用户重复领取，同时给领取者钱包入账。过期红包的未领取金额可触发式退回发送者，并由业务唯一键保证退款幂等。当前没有真实渠道回调、自动定时退款、提现、对账、风控和支付合规，因此只能称为最小资金闭环 Demo。

## 本章代码阅读任务

### 每次只问一个资金状态问题

红包附录涉及余额和并发状态，下面表格每一行必须单独问：

> 我正在学习 LinkGo 红包附录，现在只学习表格第 X 行。请先用一次创建、领取、重复领取或过期退款场景说明代码目标，再按 HTTP 入口、事务 SQL、锁/幂等判断、状态更新和响应分段解释。每个金额字段、唯一键和状态值都要说明；同时推演两次并发请求、事务中途失败和重复请求的结果。最后明确当前实现能保证什么、不能保证什么，并给 3 道带答案的自测题。

不要用“加事务就安全”代替具体 SQL 和约束解释。

| 顺序 | 打开位置 | 这次只看什么 |
| --- | --- | --- |
| 1 | `sql/init.sql` 的钱包、支付订单、红包和领取表 | 圈出整数金额、业务唯一键、remaining 字段和 `UNIQUE(red_packet_id,user_id)` |
| 2 | `cmd/gateway/internal/handler/wallethandler.go`、`redpackethandler.go` 与对应 Logic | 找到 JWT uid、模拟充值、钱包查询、红包授权和退款入口 |
| 3 | `internal/logic/wallet.go` 与 `redpacket.go` | 对充值、Create、Claim 和 Refund 的事务与流水逐行标注 |
| 4 | 同文件 `Claim` 的 duplicate-key、expired、remaining=0 三个分支 | 确认重复键先回滚再回读，expired/finished 状态修正显式 Commit |
| 5 | `public/index.html` 的 `createRedPacket()`、`claimRedPacket()`、`sendRedPacketNotice()` | 确认页面发的是普通文本提示，不存在结构化红包 WebSocket 消息类型 |
| 6 | `internal/logic/redpacket_test.go`、`wallet_test.go` | 写出 SQL mock 覆盖的钱包扣款、领取入账、退款和充值幂等边界，不把它当真实并发压测 |

看到这个程度就停：你能闭卷画出充值、发红包、Claim 和退款事务，并能说明余额、流水、业务唯一键、行锁和回滚的作用。暂时不必学习真实支付 SDK、清结算、风控和支付合规。

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
6. 顺序重试与并发唯一键冲突分别怎样返回原金额？
7. 当前哪些用户可以领取单聊红包？
8. 当前过期退款怎样执行，还缺少什么？
9. 为什么有钱包和流水后仍不能写成真实支付系统？
10. 当前测试是不是多线程真实 MySQL 压测？

十个问题能闭卷讲清楚后，再进入第 15 章。

## 动手练习与闭卷检查参考答案

### 动手练习答案

C 先 BeginTx 并通过 `FOR UPDATE` 获得红包主行锁；D 的相同查询等待。C 看到最后一份，插入 claim、把 remaining 扣为 0 并将 status 改 finished，Commit 后释放锁。D 随后读到 finished 或 remaining=0，提交必要的状态修正后返回不能领取，不会再插入一份。

同一用户两个并发请求也先由红包主行锁串行读取和扣减；如果两个请求都在事务外幂等查询 miss，数据库唯一索引在第二次 `INSERT red_packet_claims` 阻止重复。Service 捕获 1062 后回滚，再读取胜出的 claim 返回同一金额。

### 闭卷检查答案

1. 二进制浮点不能精确表示所有十进制金额，累计会出现舍入误差；整数分让加减与守恒直接可检验。
2. 主表保存红包总量、剩余和状态；领取表保存每个用户最终领取金额与时间。
3. base 为 33，remainder 为 1，领取顺序得到 34、33、33 分，总和 100。
4. 从 `SELECT ... FOR UPDATE` 获锁开始，直到当前事务 Commit 或 Rollback。
5. 行锁串行化同一红包剩余量更新；唯一索引保证同一用户对同一红包最多一条 claim。
6. 前一次已提交时，事务外查询直接返回原 claim；真正并发触发 1062 时，当前事务回滚后再查已提交 claim，也返回原金额和 already_claimed 状态。
7. 单聊会话 ID 中任一参与者都可领取，包括发送者本人；当前没有只允许接收方的规则。
8. Claim 或发送者退款接口会锁红包主行，把未领取金额退回发送者钱包、写幂等退款流水并提交 expired；仍缺后台定时扫描和可靠任务调度。
9. 模拟渠道没有签名与异步回调，系统也没有提现、总账对账、风控、合规和跨服务补偿，所以只是站内钱包最小闭环。
10. 不是。测试使用 sqlmock 验证 SQL 顺序和分支，没有启动真实 MySQL，也没有几百 goroutine 争抢同一行的集成压测。

可选下一篇：[附录 B AI 虚拟好友](APPENDIX_B_AI_BOT.md)。回到主线请阅读：[14 安全、日志、指标和故障](14_SECURITY_OBSERVABILITY.md)。
