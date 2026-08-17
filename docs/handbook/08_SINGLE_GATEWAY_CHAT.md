# 08 一条单聊消息怎样从 A 发到 B

## 本章前置

你已经知道登录后怎样建立 WebSocket，也知道 Gateway 使用 `ClientManager` 保存本机连接。

本章只研究一件事：用户 A 点击发送后，这条单聊消息怎样经过 Gateway 和 Logic，保存到 MySQL，再尝试送给 B。

为了先把调用顺序看明白，本章假设 A、B 都连接 Gateway-1。这只是学习时简化部署图，不代表代码有一套“单 Gateway 模式”。当前代码无论 A、B 在同一台还是不同 Gateway，都使用相同的 Redis 投递路径。第 09、10 章再解释 Redis 怎样找到 B 所在的 Gateway。

## 先说结论：这章到底在讲什么

这章讲的是“发送消息”，不是笼统地讲读数据和写数据，也不是专门讲数据库保存。

一次发送包含四个阶段：

```text
第一阶段：Gateway 从 A 的 WebSocket 读到消息
第二阶段：Gateway 把任务放入内存队列，再通过 gRPC 交给 Logic
第三阶段：Logic 校验消息、分配 seq，并把正文写入 MySQL
第四阶段：Logic 发起投递，B 收到后再回 ACK
```

源码中的“读”和“写”要看清对象：

| 源码动作 | 实际含义 | 是否持久化 |
| --- | --- | --- |
| `conn.Conn.ReadMessage()` | Gateway 从 A 的 WebSocket 读取一帧 | 否 |
| `queue <- task` | 消息任务进入 Gateway 内存队列 | 否，Gateway 崩溃会丢 |
| `logic.PushMessage(...)` | Gateway 跨进程调用 Logic | 否，这是 RPC 调用 |
| `INSERT messages` | Logic 把消息正文写入 MySQL | 是，MySQL 是历史消息事实源 |
| Redis `PUBLISH` | 通知目标 Gateway 给 B 推送 | 否，Pub/Sub 本身不保存历史正文 |
| `conn.WriteBinary(...)` | 目标 Gateway 向 B 的 WebSocket 写一帧 | 否，只表示交给当前网络连接 |
| B 返回 `ACK` | B 告诉服务端已经收到指定消息 | 它会推进确认进度并清理待确认记录 |

因此，这一章确实包含“消息怎样保存”，但范围更大。它从 A 发出开始，一直讲到服务端尝试交给 B。真正的离线补偿、Redis 路由和跨 Gateway 转发分别在后续章节展开。

## 本章目标

第一次读完只要求你能够：

1. 从 A 点击发送讲到 MySQL 保存消息。
2. 说清 Gateway 为什么先排队，再调用 Logic。
3. 区分“进入队列”“Logic 接受”和“B 已 ACK”。
4. 知道单 Gateway 只是学习假设，当前实现没有本机直推捷径。

UID 分片算法、队列的四种返回值和完整失败重试放到第二遍学习。第一遍不要在这些细节里卡住。

## 本章场景

```text
A 浏览器 ──WebSocket──> Gateway-1 ──gRPC──> Logic
                                           │
                                           ├──写 MySQL
                                           └──发起 Redis 投递

B 浏览器 <──WebSocket── Gateway-1 <──Redis 通知
```

这里虽然只有一台 Gateway，Gateway 和 Logic 仍是两个独立进程。Gateway 管连接，Logic 管消息业务和持久化。MySQL 与 Redis 也是独立服务，不会因为只画一台 Gateway 就变成单机内存系统。

## 1. 浏览器发送了什么

用户点击发送后，`sendMessage` 构造普通 `WireMessage`：

```text
from           网页当前 user_id
to             目标用户或群
to_type        user 或 group
msg_type       NORMAL
body           文本正文
client_msg_id  本次发送请求的客户端 ID
sent_at        客户端时间
trace_id       调试调用链 ID
```

网页将其编码为 Protobuf 二进制帧，再调用 `ws.send(...)`。

此时网页还不知道最终 `message_id` 和会话 `seq`，它先渲染一条 pending 气泡。最终 ID 由服务端生成。

## 2. Gateway 怎样收到普通消息

`StartClientLoop` 是每条连接的读取循环：

```text
ReadMessage
→ proto.Unmarshal
→ 查看 msg_type
→ 普通消息补 trace_id（若客户端没传）
→ 提交到 PushWorkerPool
```

Gateway 不相信帧里的 `from` 就是真实身份。它把 JWT 得到的 uid 另行放入 `PushMsgReq.UserId`，Logic 会比较两者。

## 3. 为什么不在读循环里直接执行所有业务

如果读循环同步执行数据库、远程调用和投递，它在慢操作期间就无法继续读取这条连接的新帧。项目先把普通消息放入工作队列，由 worker 处理。

队列是等待处理的任务集合；worker 是不断从队列取任务的 goroutine。

但队列不能无限增长。无限队列只是把过载变成内存持续上涨，最终整个 Gateway 被拖垮。因此项目使用有界队列：容量满时立即拒绝本次提交。

## 4. UID 分片队列是什么

为什么不为每条消息直接启动一个 goroutine？高峰时 goroutine 数量会失去上限，而且同一用户的两条消息可能因下游耗时不同而乱序。为什么不只用一个全局 worker？那会让某个慢用户阻塞所有其他用户。项目因此在“限制资源、同用户顺序、不同用户并行”之间选择有界分片队列。

项目创建 64 个 shard，每个 shard 是容量 64 的 channel，并有一个 goroutine 串行消费。

```text
shard = hash(user_id) % 64
```

- 同一个 user_id 总进入同一个 shard，按进入该队列的顺序处理。
- 不同 user_id 可能进入不同 shard 并行处理。
- 不同 user_id 也可能哈希到同一 shard，此时会共享一个串行队列。

FIFO 是 First In, First Out，即先入队的任务先出队。这里能保证的是同一 Gateway 内，同一发送用户的任务按提交顺序进入同一 worker；它不等于全网所有发送者严格按物理时间排序。

这个选择也有代价：不同用户哈希到同一 shard 时会互相阻塞；某个热点 shard 已满时，其他 shard 仍可能空闲。任务只保存在 Gateway 内存中，进程崩溃会丢失尚未处理的任务；`accepted` 只表示成功入队，不表示消息已经写入 MySQL。固定的 shard 数和容量也需要根据真实负载测试调整。

## 5. 队列满时客户端怎样知道

`Submit` 返回四种结果：

```text
accepted
queue_full
pool_closed
context_canceled
```

这四个名称只描述 Gateway 内部队列的提交结果：

| 结果 | 当前含义 | 发送端是否收到帧 |
| --- | --- | --- |
| `accepted` | 任务已放入某个 Gateway shard | 入队时不立即回帧；Worker 完成 Logic 调用后再返回 `MESSAGE_ACCEPTED` 或 `MESSAGE_REJECTED` |
| `queue_full` | 该 shard 的有界队列已满 | 是，`SERVER_BUSY`，可重试 |
| `pool_closed` | 工作池已关闭 | 是，`SERVER_UNAVAILABLE`，可重试 |
| `context_canceled` | 提交时上下文已取消 | 是，`REQUEST_CANCELED`，不自动重试 |

如果队列拒绝任务，Gateway 写回 `SYSTEM` 二进制帧，其中包含：

```text
code
retryable
retry_after_ms
原 client_msg_id
原 trace_id
```

错误帧会带回原 `client_msg_id` 和 `trace_id`，让网页能把拒绝绑定到正确的气泡。网页识别 `SERVER_BUSY` 后复用同一个 `client_msg_id`，采用指数增长并加入随机抖动的等待时间，最多自动重试 5 次。

随机抖动表示每个客户端的等待时间略有不同，避免大量客户端同时重试形成第二次流量尖峰。

## 6. Worker 怎样调用 Logic

`processPushTask` 发起：

```go
logic.PushMessage(ctx, &api.PushMsgReq{
    UserId:  task.uid,
    Content: task.data,
})
```

- `UserId` 来自已经认证的连接身份。
- `Content` 是客户端原始 Protobuf 消息字节。

Logic 的 gRPC 入口解码后进入 `LogicHandler.PushMessage`。

## 7. Logic 对普通消息做什么

现在只看主干顺序：

```text
解码 WireMessage
→ 用认证 uid 校验/补全 from
→ 要求 to、body、client_msg_id 非空
→ 检查单聊好友或群成员权限
→ 生成稳定会话 ID
→ 分配会话 seq
→ 生成 message_id 和服务端 sent_at
→ INSERT messages
→ 进入投递步骤
→ 更新会话摘要
```

这里先做最小定义：好友关系表示 A、B 已建立允许单聊的业务关系；群成员关系表示用户仍在目标群中。它们防止已登录用户任意给陌生人或任意群发送。具体表、状态和管理动作放到第 13 章，本章只需要知道发送前存在这道权限门。

重复发送、seq 如何原子分配和 ACK 都属于后续可靠性内容。本章先记住边界：消息正文成功写入 MySQL 发生在进入在线投递之前。

## 8. A 和 B 在同一 Gateway，为什么不直接 GetConn

概念上可以写一个本机优化：

```text
Logic 判断 B 在当前 Gateway
→ Gateway.Manager.GetConn(B)
→ WriteBinary
```

但当前代码没有这条专用捷径。无论 A、B 是否碰巧在同一 Gateway，Logic 都进入同一个共享投递适配器；该适配器产生目标 Gateway 的通知，目标 Gateway 的订阅循环收到通知后，才调用本机 `Manager.GetConn(B)`。

这样做的好处是同机与跨机路径统一；代价是同机消息也经过一次外部共享服务。第 09 章解释该共享服务，第 10 章再完成精确返回链路。

因此面试不能说“同一 Gateway 时当前代码直接读 map，跨 Gateway 才走 Redis”。当前实现两种情况都走统一 Redis 投递路径。

## 9. 单 Gateway 场景现在能画到哪里

本章已经掌握的真实部分：

```text
A 网页
→ WebSocket 二进制帧
→ Gateway StartClientLoop
→ UID 分片有界队列
→ gRPC Logic.PushMessage
→ 身份/权限/字段校验
→ MySQL messages 持久化
→ 统一投递适配器
→ （第 09 章解释中间桥梁）
→ 同一 Gateway 的订阅循环
→ ClientManager.GetConn(B)
→ B 的 WebSocket
```

这不是遗漏，而是依赖顺序：不先理解“为什么普通内存无法跨进程共享”，直接背中间件名没有意义。

## 10. 发送方什么时候知道成功

这里必须区分入队结果、Logic 处理结果和接收方 ACK。

- 队列立即拒绝时，Gateway 返回 `SERVER_BUSY`、`SERVER_UNAVAILABLE` 或 `REQUEST_CANCELED`。
- 任务成功入队时不会立刻宣告成功。Worker 调用 Logic 完成后，通过原连接返回一个 `SYSTEM` 结果帧。
- Logic 返回成功时，结果码是 `MESSAGE_ACCEPTED`。这表示 `LogicHandler.PushMessage` 已正常返回，首次消息已经完成 MySQL 持久化和当前投递编排，或者同一 `client_msg_id` 对应的已完成请求被幂等接受。它不表示 B 已收到或 ACK。
- gRPC、权限、MySQL、Redis 等处理失败时，结果码是 `MESSAGE_REJECTED`。可重试错误会要求网页复用同一个 `client_msg_id` 重试。
- 同 ID 的前一个请求仍在处理时，Logic 把 `ErrClientMessageInFlight` 转为 gRPC `Aborted`，Gateway 将其作为可重试的 `MESSAGE_REJECTED` 返回，不会把“正在处理”误报成成功。
- 网页先乐观显示 pending 气泡；收到 `MESSAGE_ACCEPTED` 后改成 accepted，收到不可重试拒绝后改成 rejected，并仍可通过历史接口刷新服务端最终记录。

当前正向结果帧只带回 `client_msg_id` 和 `trace_id`，没有携带服务端最终 `message_id/seq`。因此它已经补上了“Logic 是否接受”的反馈，但还不是包含完整服务端消息对象的商业级发送回执。

## 11. 发送者会不会收到自己的实时投递

单聊 `resolveRecipients` 当前只返回目标用户 B，不把发送者 A 加入实时收件人列表。

A 页面上的消息来自乐观渲染，随后通过历史接口看最终记录；B 才是在线投递的目标。多设备同步尚未实现，因此也没有把 A 的消息实时推送到 A 的其他设备。

## 本章代码阅读任务

不要第一次就按八个文件全部钻下去。先完成主链，再回头读队列保护。

第一遍只读 1、3、6、7、8：看 A 怎样发送，Gateway 怎样收到，Logic 怎样落库，目标 Gateway 怎样找到 B 的本机连接。读完后能画出调用顺序即可。

第二遍再读 4、5：理解有界分片队列、队列满拒绝和最终处理结果。这些是可靠性增强，不是理解单聊业务的前置条件。

| 顺序 | 打开位置 | 这次只看什么 |
| --- | --- | --- |
| 1 | `public/index.html` 的 `sendMessage()`、`handleSystemMessage()`、`scheduleMessageRetry()` | 找到乐观 pending、相同 `client_msg_id` 重试和 accepted/rejected 状态变化 |
| 2 | `api/protocol.proto` 的 `WireMessage`、`PushMsgReq` | 圈出客户端字段与 Gateway 另传的认证 `user_id` |
| 3 | `internal/server/client.go` 的 `StartClientLoop` 普通消息分支 | 看解码、补 `trace_id`、`SubmitWithResult` 和完成回调怎样写结果帧 |
| 4 | `internal/server/pool.go` 的 `PushWorkerPool`、`SubmitWithResult`、`runShard`、`processPushTask` | 找到 shard 哈希、有界 channel、串行 worker 和 `onComplete` 调用 |
| 5 | `internal/server/client_error.go` 的 `pushRejectionDetail`、`pushProcessingDetail`、`writePushResult` | 区分入队拒绝码和 Logic 最终处理结果码 |
| 6 | `cmd/logic/internal/server/logicserver.go` 与 `cmd/logic/internal/logic/pushmessagelogic.go` 的 `PushMessage` | 看 gRPC 入口，以及 `ErrClientMessageInFlight` 怎样映射为 `codes.Aborted` |
| 7 | `internal/logic/handler.go` 的 `PushMessage`、`normalizeFrame`、`saveMessage`、`deliverPersistedMessage` | 按源码顺序写出验身份、权限、seq、MySQL 和投递步骤 |
| 8 | `internal/server/manager.go` 的 `SubscribeRedis` | 只定位目标 Gateway 最终调用本机 `GetConn` 和 `WriteBinary` 的位置，细节留到第 10 章 |

看到这个程度就停：你能分别指出“浏览器送入 Gateway”、“Gateway 排队”、“Worker 调 Logic”、“Logic 返回结果”四个阶段，且不会把入队 accepted、`MESSAGE_ACCEPTED` 和 B 的 ACK 混成一件事。暂时不必掌握 Redis 投递 Key 和 Kafka 群聊。

## 动手练习

### 练习一：运行队列测试

```bash
go test ./internal/server -run 'TestPushWorkerPoolPreservesOrderForSameUID|TestPushWorkerPoolRunsDifferentShardsInParallel|TestPushWorkerPoolReportsQueueFull' -v
```

分别说明三个测试证明了什么，尤其不要把“不同 shard 并行”误说成“任意两个不同用户一定并行”。

### 练习二：找身份信任边界

假设 userA 的连接在帧里伪造 `from=userB`。找到 `normalizeFrame`，说明为什么请求会被拒绝。

### 练习三：判断发送成功

依次判断下面事件能证明什么：

```text
浏览器 ws.send 返回
PushWorkerPool 返回 accepted
Logic 的 messages INSERT 成功
B 的 WebSocket WriteBinary 成功
```

本章正确答案至少应指出：前两项都不能证明消息已落 MySQL，第四项也不能证明 B 的应用已经处理消息。

## 闭卷检查

1. 浏览器发送普通消息时哪些字段由客户端填写？
2. Gateway 为什么另行传递认证 user_id？
3. 为什么读循环不直接执行所有消息业务，也不为每条消息无限启动 goroutine？
4. 有界队列解决什么问题？队列满时付出的代价是什么？
5. UID 分片相对单个全局 worker 有什么好处，又能保证什么、不能保证什么？
6. `SERVER_BUSY` 为什么必须携带原 client_msg_id？
7. Logic 在消息投递前完成了哪些核心动作？
8. 当前同 Gateway 单聊是否直接调用本机 Manager？
9. 当前下游 Logic 失败是否一定会通知发送网页？
10. 发送者页面为什么能先看到自己的消息？
11. `accepted` 为什么不能解释成“消息已落 MySQL”？

## 动手练习与闭卷检查参考答案

### 动手练习答案

1. `TestPushWorkerPoolPreservesOrderForSameUID` 证明同 uid 进入同 shard 串行处理；`TestPushWorkerPoolRunsDifferentShardsInParallel` 证明刻意选择到不同 shard 的任务可并行；`TestPushWorkerPoolReportsQueueFull` 证明有界队列满时明确返回背压。不同 uid 仍可能哈希到同一 shard，不能保证任意不同用户都并行。
2. `normalizeFrame` 使用 `PushMsgReq.UserId` 代表认证 uid。frame.from 为空时补 uid，不为空且不等于 uid 时返回 sender mismatch，所以 userA 不能靠修改 Protobuf 冒充 userB。
3. `ws.send` 只表示浏览器把数据交给 WebSocket；内部 `SubmitAccepted` 只表示任务进入 Gateway 内存队列；MySQL INSERT 成功证明消息正文持久化；B 的 `WriteBinary` 成功只表示数据交给网络连接，仍需客户端解码和 ACK。当前网页另会收到 `MESSAGE_ACCEPTED/REJECTED` 表示 Logic 调用结果。

### 闭卷检查答案

1. `from`、`to`、`to_type`、`msg_type`、`body`、`client_msg_id`、`sent_at` 和可选 `trace_id`；服务端随后规范化服务端字段。
2. 帧内容可被客户端篡改，认证 uid 来自握手 JWT。两者分开传递才能在 Logic 校验发送者。
3. 同步慢操作会阻塞连接读循环；每消息无限起 goroutine 会失去资源上限并破坏同用户顺序，所以使用有界 worker 队列。
4. 它限制内存中的待处理任务并显式暴露过载；代价是队列满时必须拒绝或让客户端重试。
5. 相比全局单 worker，不同 shard 可以并行且同 uid 保持提交 FIFO；它不保证跨 Gateway 全局顺序，也不保证两个不同 uid 一定在不同 shard。
6. 网页需要把拒绝关联到正确气泡，并用相同 ID 安全重试，避免另生成 ID 导致重复消息。
7. 解码和规范化、认证发送者、字段校验、关系权限、会话 ID、幂等、seq、message_id、MySQL 持久化，然后进入收件人投递和会话更新。
8. 不直接调用。当前同机和跨机都进入统一 Redis 定向投递路径。
9. 服务端会尝试通过 Worker 完成回调写 `MESSAGE_REJECTED`；可重试错误携带 retryable 和建议等待时间。但若原 WebSocket 已断开或结果帧写失败，页面不一定实际看到，所以题目中的“一定”仍应回答不能保证。
10. 页面先乐观渲染自己刚构造的消息，不是服务器把 A 当接收者实时推回。
11. 内部 `SubmitAccepted` 只表示任务进入 Gateway 内存。只有后续 `MESSAGE_ACCEPTED` 才表示 Logic 正常返回；它仍不证明 B 已收到或 ACK。

下一步：[09 Redis 基础与在线状态](09_REDIS_BASICS.md)
