# 08 单台 Gateway：先看消息上行与本机连接

## 本章前置

你已经知道 MySQL、登录、gRPC、Protobuf 和 WebSocket，也知道某台 Gateway 使用 `ClientManager` 保存本机连接。

本章先假设 A、B 都连接同一台 Gateway，目的是看清一条普通消息怎样进入系统。当前源码即使在这个场景也不会走“直接调用本机 map”的捷径，返回链路会在第 09、10 章补齐。

## 本章目标

读完后，你应该能够：

1. 看懂 `WireMessage` 的最小发送字段。
2. 从浏览器追到 Gateway 读循环、工作队列和 Logic。
3. 解释为什么队列要有界、为什么按 user_id 分片。
4. 说明消息在 Logic 中先校验、保存，再进入投递。
5. 区分“概念上的本机直推”和“当前代码的统一投递路径”。

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

如果没有被接收，Gateway 写回 `SYSTEM` 二进制帧，其中包含：

```text
code
retryable
retry_after_ms
原 client_msg_id
原 trace_id
```

网页识别 `SERVER_BUSY` 后复用同一个 `client_msg_id`，采用指数增长并加入随机抖动的等待时间，最多自动重试 5 次。

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

这里必须按当前源码说实话。

- 队列立即拒绝时，Gateway 会向网页返回结构化错误帧。
- 队列接收任务后，`PushMsgReply` 是内部空响应，不会作为“发送成功帧”返回浏览器。
- 如果后续 gRPC/Logic 处理失败，当前 worker 主要记录日志和指标，没有再向原客户端发送结构化失败帧。
- 网页先乐观显示 pending 消息，并在约 900 ms 后调用历史接口刷新。

因此当前还没有完整的“服务端已持久化回执”协议。`client_msg_id` 为安全重试提供基础，但浏览器不能仅凭 `ws.send` 返回就断言 MySQL 已成功提交。

这是重要的当前边界，也是一项后续可优化内容：Logic 返回最终 `message_id/seq`，Gateway 再给发送端明确的接受回执。

## 11. 发送者会不会收到自己的实时投递

单聊 `resolveRecipients` 当前只返回目标用户 B，不把发送者 A 加入实时收件人列表。

A 页面上的消息来自乐观渲染，随后通过历史接口看最终记录；B 才是在线投递的目标。多设备同步尚未实现，因此也没有把 A 的消息实时推送到 A 的其他设备。

## 代码锚点

按顺序阅读：

1. `public/index.html`：`sendMessage`、`encodeWireMessage`、`scheduleMessageRetry`。
2. `api/protocol.proto`：`WireMessage` 和 `PushMsgReq`。
3. `internal/server/client.go`：`StartClientLoop`。
4. `internal/server/pool.go`：`PushWorkerPool.Submit`、`runShard`、`processPushTask`。
5. `internal/server/client_error.go`：队列拒绝错误帧。
6. `cmd/logic/internal/server/logicserver.go`：`PushMessage` gRPC 入口。
7. `internal/logic/handler.go`：`PushMessage`、`normalizeFrame`、`saveMessage`、`deliverPersistedMessage`。
8. `internal/server/manager.go`：稍后返回到目标 Gateway 后的本机写出位置。

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

下一步：[09 Redis 基础与在线状态](09_REDIS_BASICS.md)
