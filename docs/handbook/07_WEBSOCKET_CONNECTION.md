# 07 从 HTTP 升级为 WebSocket

## 本章前置

你已经知道 HTTP 请求与响应、JWT 认证、Gateway 和 Logic 是不同进程。

本章只解决“一个用户怎样与某台 Gateway 保持实时连接”。跨 Gateway 找人留到第 10 章。

## 本章目标

读完后，你应该能够：

1. 解释为什么登录成功不等于已经建立聊天连接。
2. 说清 WebSocket 握手、双向通信、帧和心跳。
3. 沿代码找到连接创建、登记、读循环和清理位置。
4. 说明当前一个 Gateway 怎样保存本机连接。
5. 解释为什么并发写需要互斥锁，慢连接为什么还需要写超时。
6. 说出浏览器客户端的真实重连与多端边界。

## 1. HTTP 为什么不够方便

普通 HTTP 主要由客户端发起：

```text
客户端请求
→ 服务端响应
→ 本次请求结束
```

聊天中，B 没有必要每秒发 HTTP 问“有新消息吗”。更自然的方式是建立一条持续连接，让服务端有消息时主动推送。

WebSocket 是建立在一次 HTTP 握手之上的双向长连接协议。双向表示客户端和服务端都可以主动发送数据。

替代方案包括定时轮询，以及主要由服务端单向推送的 SSE（Server-Sent Events）。轮询容易产生空请求和额外延迟；SSE 可以推送，但客户端发送聊天内容仍要配合另一条 HTTP 请求。LinkGo 需要双方在同一连接上持续发送心跳、普通消息和确认帧，因此选择 WebSocket。

代价是 Gateway 要长期占用连接、内存和文件描述符，不能再把每次交互当作“响应结束就清理”。项目还必须处理身份校验、心跳、并发写、断线清理和重连；WebSocket 本身不会自动保证消息落库、必达或多设备同步。

## 2. 什么叫 HTTP Upgrade

客户端最初仍发送 HTTP 请求，但请求中表达“希望升级为 WebSocket”。服务端接受后返回升级成功，之后双方不再按一次 HTTP 请求、一次 HTTP 响应的方式交互，而是在同一条连接上传输 WebSocket 帧。

LinkGo 路径是：

```text
GET /ws?token=<JWT>
```

握手成功后的 `websocket.Conn` 是 Gateway 进程内的 Go 对象。它不能被另一台 Gateway 直接读取。

## 3. 帧是什么

WebSocket 在连接上传输一帧一帧的数据。LinkGo 的业务消息使用二进制帧，内容按照 `api.WireMessage` 的 Protobuf 规则编码。

浏览器页面没有引入完整的 Protobuf JavaScript 库，而是在 `public/index.html` 中手写了项目当前字段所需的最小编码和解码函数：

```text
encodeWireMessage
decodeWireMessage
encodeVarint
```

优点是单文件、零构建依赖；代价是 `.proto` 增删字段时需要人工同步网页代码。正式客户端通常会从 `.proto` 生成客户端代码。

## 4. 握手前经过哪些检查

`/ws` 路由依次经过：

```text
AuthMiddleware
→ WebSocket 专用限流中间件
→ WebSocketHandler
→ Origin 检查
→ 可选的会话回放权限检查
→ 获取 Logic 客户端
→ Upgrade
```

### JWT 检查

`AuthMiddleware` 从查询参数读取当前网页传来的 Token，验证后把 user_id 放入 Context。Handler 不接受一个匿名连接再让它事后声明身份。

### Origin 检查

Origin 表示浏览器页面来自哪个 scheme、host 和 port，例如：

```text
http://127.0.0.1:8088
```

如果任意网页都能利用用户 Token 发起跨站 WebSocket，可能产生安全问题。项目把 Origin 标准化后与白名单精确匹配，不使用模糊字符串包含。

默认情况下，缺少 Origin 也会被拒绝。受信任的非浏览器客户端只有在显式配置 `WS_ALLOW_MISSING_ORIGIN=true` 后才能省略 Origin，并且仍然需要 JWT。

### 会话回放权限检查

建连可以不带会话。若携带 `/ws?...&session_id=...&last_seq=...`，`session_id` 是客户端可修改的资源标识，JWT 只能证明“你是谁”，不能自动证明“你能读这个会话”。如果不检查，已登录用户可能替换会话 ID，读到共享 Redis timeline 中不属于自己的近期消息。

因此当前 Handler 在 Upgrade 前先校验：单聊 `c2c` 会话必须包含当前 JWT user_id；群聊必须能在 MySQL 查到当前用户的 `active` 成员记录；格式错误、非成员和数据库查询错误都拒绝回放。心跳帧后续要求补拉另一会话时也会重新校验，避免建连后换 ID 绕过。代价是群聊回放会多一次 MySQL 成员查询；规模上升后可评估短 TTL 权限缓存，但成员退群后的失效语义也必须一起设计。

### Logic 客户端句柄

当前 Handler 在 Upgrade 之前取得 Logic gRPC 客户端句柄。只有句柄本身不存在时才返回 HTTP 503；`GetClient` 不会在这里执行一次远程业务探测。因此“拿到客户端句柄”不能证明某个 Logic 实例此刻一定可用，真实远程故障仍可能在建连后的消息调用中暴露。Gateway 另有 readiness 检查，但那属于后续工程化章节。

## 5. 连接怎样保存在本机

成功 Upgrade 后，项目创建：

```go
type ClientConn struct {
    Conn         *websocket.Conn
    SessionID    string
    WriteTimeout time.Duration
    writeMu      sync.Mutex
}
```

这里的 `SessionID` 实际是一次 WebSocket 连接的随机身份，名称容易与聊天 `session_id` 混淆。它用于区分旧连接和新连接，不是会话 ID。

`ClientManager` 使用 `sync.Map` 保存：

```text
user_id → *ClientConn
```

这张表只在当前 Gateway 内存里。

如果同一用户在同一 Gateway 又建立新连接，`Add` 使用 `Swap` 替换旧连接并关闭旧连接。`Remove` 使用 `CompareAndDelete`，只有待删除对象仍是当前连接时才删除，避免旧连接结束时误删刚建立的新连接。

## 6. 为什么写连接需要互斥锁

心跳回复、实时消息和错误帧都可能尝试向同一 WebSocket 写数据。`ClientConn.WriteBinary` 使用 `writeMu` 把写操作串行化，并在每次写入前设置默认 5 秒写 deadline。

互斥锁（mutex）保证同一时刻只有一个 goroutine 进入受保护代码。这里不是为了让业务消息全局有序，而是避免多个 goroutine 并发写同一连接。

当前读操作集中在一个 `StartClientLoop` 中，写操作统一经过 `WriteBinary`。如果只加锁不设超时，一个长时间无法接收数据的客户端可能让写操作一直占有锁，连带阻塞后续心跳回复、实时消息和回放。写超时让失效连接尽快退出；代价是在非常慢的网络上也可能主动断开。5 秒是当前工程默认值，不是经过生产 SLO 校准的结论。

## 7. 读循环做什么

`StartClientLoop` 持续调用 `ReadMessage`。每收到一帧：

1. 按 Protobuf 解码为 `WireMessage`。
2. 根据 `msg_type` 判断它是普通消息、心跳还是确认帧。
3. 心跳在本循环处理。
4. 普通消息交给后续消息工作队列。
5. 读取失败时退出循环，Handler 的 defer 开始清理连接。

服务端把单帧读取上限设置为 `64 << 10`，即 64 KiB，避免客户端无限发送超大帧占用内存。

## 8. 心跳为什么存在

连接在网络断开后不一定马上让双方感知。心跳用于确认连接仍有数据往来，并延长服务端读取期限。

当前网页行为：

```text
每 20 秒发送 HEARTBEAT
→ 服务端刷新连接相关状态和读超时
→ 返回 HEARTBEAT/PONG
→ 网页记录最近 PONG 时间
```

如果网页超过 45 秒没有收到 PONG，会主动关闭 WebSocket。服务端默认路由 TTL 和读取期限是 75 秒，只有收到心跳帧时才延长读取 deadline。

心跳还会续期这条连接对应的共享路由，但续期前必须确认 Redis 中仍然是同一个 `gateway_id|connection_id`。如果同一用户已在另一台 Gateway 登录，旧连接的心跳不能把新路由改回去，服务端会结束这条旧连接。第 09、10 章会解释这个路由所有权检查。

TCP 层也有保活机制，但应用心跳还能携带业务进度，并让项目按自己的时间要求判断失活。

## 9. 连接结束时怎样清理

Handler 使用 defer 注册清理动作：

```text
关闭 websocket.Conn
从本机 ClientManager 移除当前连接
减少连接数指标
清理属于当前连接的共享在线记录
```

最后一项为什么必须验证“仍属于当前连接”，会在 Redis 和多 Gateway 章节完整解释。现在只记住：旧连接退出不能伤害新连接。

## 10. 当前客户端会不会自动重连

不会完整自动重连。

当前网页：

- 提供“重连 WS”按钮。
- 心跳超时会关闭连接。
- `onclose` 只更新页面状态，不运行指数退避自动重连循环。
- 刷新页面后 Token 也会丢失，需要重新登录，因为 Token 只保存在 JavaScript 内存中。

因此面试时可以说客户端支持手动重连和携带进度恢复，不能说已经完成移动端级别的网络恢复、指数退避自动重连和 Token 刷新。

当前网页把登录会话列表中的 `conversation.last_seq` 放入内存 `lastSeqBySession`，收到消息后再更新。初始 `last_seq` 是服务端会话最新序号，不等于这台浏览器已经收到或 ACK 的可靠设备游标；刷新页面后这份内存状态也会丢失。因此它只能支持演示性的单会话近期补偿，不能当作商业级 per-device 同步进度。

## 11. 当前是否支持同一账号多设备同时在线

不支持完整多端同时在线。

本机 `ClientManager` 对一个 user_id 只保存一个 `ClientConn`。后续共享在线路由也只保存一个位置。新连接会覆盖旧连接所代表的位置。

若要支持手机和电脑同时在线，通常要把路由模型改为：

```text
user_id → 多个 device_id/connection_id → 各自 Gateway
```

并分别维护推送、ACK 和清理。这个模型当前没有实现。

## 本章代码阅读任务

| 顺序 | 打开位置 | 这次只看什么 |
| --- | --- | --- |
| 1 | `cmd/gateway/internal/handler/routes.go` 中注册 `/ws` 的 Route | 写下中间件顺序，确认认证和限流发生在 Handler 前 |
| 2 | `cmd/gateway/internal/middleware/authmiddleware.go` 的 `Handle` | 找到查询参数 Token 解析和 uid 写入 Context 的位置 |
| 3 | `cmd/gateway/internal/handler/websockethandler.go` 的 `WebSocketHandler`、`webSocketOriginAllowed`、`authorizeReplaySession` | 按源码顺序看 Origin、回放授权、Logic 客户端、Upgrade、`ClientConn`、`ClaimRoute` 和 defer 清理 |
| 4 | `internal/server/manager.go` 的 `ClientConn`、`ClientManager`、`NewClientConn`、`Add`、`Remove`、`WriteBinary` | 圈出 `SessionID`、`writeMu`、写 deadline 和 `CompareAndDelete` |
| 5 | `internal/server/client.go` 的 `StartClientLoop` | 只区分 ACK、HEARTBEAT 和普通消息三个分支，并找到 64 KiB 读取限制 |
| 6 | `public/index.html` 的 `connectWebSocket()`、`startHeartbeat()`、`encodeWireMessage()` | 对照浏览器建连、20 秒心跳和二进制编码 |

看到这个程度就停：你能从 `/ws?token=...` 讲到本机 `ClientManager` 登记，再讲到读循环退出后的条件清理；也能解释 `ClientConn.SessionID` 是连接身份。暂时不必读 WebSocket 帧协议 RFC、gorilla/websocket 内部实现和完整 Protobuf 编码算法。

## 动手练习

### 练习一：按顺序写握手检查

不看文档写出：JWT、限流、Origin、可选会话回放授权、Logic 客户端、Upgrade。再说明其中哪些发生在 WebSocket 建立之前。

### 练习二：验证旧连接不能删新连接

```bash
go test ./internal/server -run TestClientManagerRemoveOnlyMatchingSession -v
```

阅读测试中的两个连接对象，解释为什么普通 `Delete(uid)` 会出错。

### 练习三：验证 Origin

```bash
go test ./cmd/gateway/internal/handler -run 'TestWebSocketOriginAllowed|TestRejectInvalidWebSocketOrigin' -v
```

列出允许、拒绝和缺失 Origin 三类输入。

## 闭卷检查

1. 登录成功为什么不等于已有实时连接？轮询、SSE 和 WebSocket 的取舍是什么？
2. HTTP Upgrade 前做了哪些检查？JWT 与会话回放授权分别证明什么？
3. Origin 白名单解决什么问题？
4. `ClientConn.SessionID` 是聊天会话 ID 吗？
5. ClientManager 保存在哪里，其他 Gateway 能直接读取吗？
6. 为什么 `WriteBinary` 需要 mutex？
7. 当前心跳间隔和网页 PONG 超时各是多少？
8. 为什么旧连接清理不能直接删除 user_id？
9. 当前网页是否实现自动退避重连？
10. 当前是否支持同账号多设备并存？

## 动手练习与闭卷检查参考答案

### 动手练习答案

1. 顺序是 JWT 中间件、WebSocket 限流、Handler 内 Origin、可选会话回放授权、取得 Logic 客户端、Upgrade。列出的检查都在 Upgrade 前完成；Upgrade 后才登记连接和启动读循环。
2. 测试先把 `old` 加入 uid，再用 `new` 替换，最后让旧连接执行 `Remove`。普通 `Delete(uid)` 会把新连接一起删掉；`CompareAndDelete(uid, old)` 发现当前值已是 `new`，因此保留新连接。
3. 配置白名单中的 scheme、host、port 精确匹配时允许；未列出的、相似恶意域名、scheme 或 port 不同、格式错误时拒绝。缺少 Origin 默认拒绝，只有 `allowMissingOrigin=true` 才允许受信任非浏览器客户端继续认证。

### 闭卷检查答案

1. 登录 HTTP 返回后请求已经结束，只得到 JWT；轮询会产生空请求与延迟，SSE 主要是单向下行，WebSocket 在一条长连接上允许双方持续发消息和心跳，但增加连接状态管理成本。
2. JWT、限流、Origin、可选回放资源授权、Logic 客户端存在性检查。JWT 证明 uid 身份；资源授权证明该 uid 可以读取指定单聊或群 timeline。
3. 限制哪些网页来源可以借浏览器建立连接，降低跨站 WebSocket 滥用；它不能替代 JWT。
4. 不是。它是这一次 WebSocket 连接的随机身份，用来区分同 uid 的旧、新连接。
5. 保存在当前 Gateway 进程的 `ClientManager` 内存中，其他 Gateway 不能直接读取。
6. 心跳回复、实时投递和结果帧可能由不同 goroutine 写同一 socket；mutex 把写操作串行化，写 deadline 则避免慢连接长期占锁。
7. 网页每 20 秒发送心跳，45 秒未收到 PONG 会主动关闭。
8. uid 可能已经由新连接占用。旧连接只能在当前 map 值仍是自己时删除，否则会误伤新连接。
9. 没有。当前只有手动“重连 WS”，`onclose` 不执行指数退避自动重连。
10. 不支持。uid 在本机连接表和 Redis route 中都是单值，新连接替换旧位置。

下一步：[08 先看单台 Gateway 的单聊](08_SINGLE_GATEWAY_CHAT.md)
