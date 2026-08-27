# 01 Go 语言与并发

## 1. Goroutine 和线程有什么区别？

### 面试官问

你的 Gateway 有很多长连接，为什么用 goroutine？它和操作系统线程有什么区别？

### 30 秒回答

Goroutine 是 Go 运行时调度的轻量执行单元，操作系统线程由内核调度。Go 会把大量 goroutine 复用到较少的线程上，goroutine 初始栈较小并能按需增长，所以很适合大量主要在等待网络 I/O 的连接。它不是免费资源，goroutine 仍会占栈、调度和引用对象，因此我不会为每条消息无限创建 goroutine，而是用有界分片队列控制并发。

### 详细回答

线程切换要经过内核调度，线程栈通常也比 goroutine 初始栈大。Go 运行时使用 G、M、P 模型：G 表示 goroutine，M 可以理解为操作系统线程，P 保存执行 Go 代码所需的调度资源。多个 G 会被调度到多个 M 上运行。

网络读写阻塞时，Go 的网络轮询器会让等待 I/O 的 goroutine 暂停，把执行机会交给其他 goroutine。这样一个 Gateway 可以维护很多互相独立的连接处理流程，而不用手写复杂的事件循环。

但是 goroutine 数量失控仍会带来问题。任务会占内存，调度成本会增加，任务引用的请求数据也不能释放。如果下游 MySQL 或 Logic 变慢，每条消息都启动一个 goroutine，积压会从数据库一路扩散到 Gateway。

### 结合 LinkGo

LinkGo 的每条 WebSocket 连接由 `StartClientLoopWithDB` 持续读取。普通消息不会再随手启动一个 goroutine，而是提交到 `PushWorkerPool`。当前池有 64 个 shard，每个 shard 的 channel 容量也是 64。每个 shard 只有一个消费 goroutine，因此同一发送用户的任务按入队顺序处理，不同 shard 可以并行。

### 为什么这样选，替代方案是什么

直接在连接循环里同步调用 Logic 最简单，但下游慢时会连心跳都读不及时。每条消息启动 goroutine 并发度最高，却没有上限。固定全局 worker pool 能限流，但多个 worker 会打乱同一用户的处理顺序。按 uid 分片的有界队列是在顺序、并发和内存上限之间做的折中。

### 边界与追问

当前顺序保证的是同一 uid 在同一个 Gateway 进程中的任务处理顺序，不是跨设备、跨 Gateway 的全局严格顺序。最终会话排序还要依赖 Logic 分配的 `seq`。面试官继续问时，要能说出队列满会返回 `SERVER_BUSY`，客户端复用原 `client_msg_id` 退避重试。

## 2. Channel 是队列吗？为什么要做成有界？

### 面试官问

项目里的 channel 解决了什么问题？为什么不用无限队列？

### 30 秒回答

Channel 是 goroutine 之间传递数据和同步的一种语言机制。在 LinkGo 中，它承担 Gateway 内部任务队列的角色。队列必须有界，因为下游处理速度长期低于入口速度时，任何无限队列最终都会吃光内存。有界队列把资源上限写进系统，满了就快速返回可重试错误，这叫背压。

### 详细回答

无缓冲 channel 的发送和接收要同时就绪，适合直接同步交接。带缓冲 channel 可以暂存固定数量的元素，发送方在缓冲区未满时不用等待消费者。

背压的核心不是让系统永远不拒绝，而是在容量不足时明确拒绝。没有背压时，Gateway 看起来一直收到了消息，实际上任务在内存里越积越多，延迟持续上升，最终进程被 OOM 杀掉。此时所有连接一起断开，影响比早一点拒绝部分请求更大。

LinkGo 的提交使用 `select`：channel 可写就入队，请求 Context 已取消就返回取消，默认分支表示队列当前已满。这个提交不会无限等待。

### 结合 LinkGo

`PushWorkerPool.SubmitWithResult` 会返回 `accepted`、`queue_full`、`pool_closed` 或 `context_canceled`。`accepted` 只代表进入 Gateway 内存，不代表 Logic 已经落库。Worker 调用 Logic 完成以后，Gateway 才向发送客户端写 `MESSAGE_ACCEPTED` 或 `MESSAGE_REJECTED`。

### 为什么这样选，替代方案是什么

可以把任务放进 Kafka，但单聊主链路需要低延迟定向投递，引入 Kafka 会增加组件依赖和状态判断。也可以用无界链表队列，但要另外实现限额、淘汰和监控。Go 的有缓冲 channel 足够表达当前进程内背压，所以实现更直接。

### 边界与追问

队列容量 64 和 shard 数 64 是项目默认值，不是生产压测结论。真实部署要根据消息处理时间、目标延迟、内存和拒绝率校准。进程在任务落库前崩溃时，内存队列里的任务可能丢失，所以客户端必须根据结果和 `client_msg_id` 重试。

## 3. Mutex、RWMutex 和 sync.Map 分别怎么用？

### 面试官问

WebSocket 能不能被多个 goroutine 同时写？你怎么保护连接表？

### 30 秒回答

普通 `Mutex` 适合保护一段必须串行执行的临界区，`RWMutex` 允许多个读者并发，但写入仍独占。`sync.Map` 适合并发读写且键值生命周期相对独立的场景。LinkGo 用 `sync.Map` 保存 uid 到本机连接的映射，用每条连接自己的 `writeMu` 串行化 WebSocket 写操作，避免多个推送协程同时写同一个连接。

### 详细回答

并发安全不只看容器是否安全，还要看一整段操作是否原子。`sync.Map.Load`、`Swap` 和 `CompareAndDelete` 各自是并发安全的，但如果业务需要“读旧值、判断、再写新值”作为一个不可分割动作，就还要用 CAS 风格方法或额外锁。

WebSocket 连接可能同时收到 Redis 在线推送、ACK 超时重试、心跳响应和处理结果。如果这些路径直接并发调用 `WriteMessage`，帧可能冲突，库也不保证所有并发写法安全。`ClientConn.WriteBinary` 先获取 `writeMu`，设置写 deadline，再写一帧，确保同一连接一次只有一个写操作。

### 结合 LinkGo

`ClientManager.UserConns` 是 `sync.Map`。新连接通过 `Swap` 替换同 uid 的旧连接，并关闭旧连接。清理时使用 `CompareAndDelete(uid, conn)`，只有 map 里仍然是当前这条连接才删除。这样旧连接晚到的 defer 不会误删用户刚建立的新连接。

### 为什么这样选，替代方案是什么

也可以用 `map[string]*ClientConn` 加 `RWMutex`，好处是行为直观，适合需要跨多个键做整体操作的场景。当前连接表主要按单个 uid 独立加载、替换和删除，`sync.Map` 的接口正好覆盖。连接写锁必须放在每个 `ClientConn` 上，如果使用一把全局锁，所有用户的写入都会互相阻塞。

### 边界与追问

`sync.Map` 只保存本 Gateway 的连接，不能跨进程查询。跨 Gateway 定位用户依赖 Redis `route:<uid>`。`writeMu` 只能保证写调用串行，不能证明客户端已经收到，所以还需要应用层 ACK。

## 4. Context 到底是什么？为什么能 Value 查值？

### 面试官问

`context.Context` 在项目里有什么作用？`ctx.Value(ctxUserIDKey{})` 为什么像 map 一样？

### 30 秒回答

Context 用来沿调用链传递取消信号、截止时间和少量请求级元数据。`WithValue` 返回一个新的 Context 节点，节点保存 parent、key 和 value。`Value(key)` 先比较当前节点的 key，不匹配就向 parent 继续查，因此用起来像 map，底层更像一条不可变的链。LinkGo 把 JWT 已验证的 user id 放进请求 Context，后续 Handler 不再相信客户端自己填写的身份。

### 详细回答

Context 接口提供 `Deadline`、`Done`、`Err` 和 `Value`。`WithCancel`、`WithTimeout`、`WithDeadline`、`WithValue` 都不会修改原对象，而是基于父 Context 创建子节点。取消父节点会影响它下面的子节点。

写入用户身份时，代码大致是：

```go
ctx := context.WithValue(r.Context(), ctxUserIDKey{}, claims.UserID)
next(w, r.WithContext(ctx))
```

后续执行 `ctx.Value(ctxUserIDKey{})` 时，当前 value 节点比较 key。`ctxUserIDKey{}` 是可比较的空结构体，同一个未导出类型的两个零值相等。返回类型是 `any`，所以读取方再用 `.(string)` 做类型断言。

Context 不应该保存数据库连接、大型业务对象或可选参数集合。它适合 trace id、认证身份这类贯穿一次请求的元数据。

### 结合 LinkGo

HTTP AuthMiddleware 解析 Bearer Token，把 Claims 中的 user id 写入 Request Context。WebSocket Handler 从 Context 读取 user id，再用它登记本机连接和 Redis 路由。普通消息进入 Logic 时，Gateway 也把这个已认证 uid 作为 `PushMsgReq.UserId` 发送，Logic 不以 Protobuf 消息正文里的 `from` 作为可信身份。

### 为什么这样选，替代方案是什么

可以给每个 Handler 增加显式 `userID string` 参数，但 HTTP 中间件的统一传递会变得啰嗦。也可以把身份放全局变量，这是错误的，因为并发请求会互相覆盖。Context 的优点是生命周期跟请求走，并且可以向下游 RPC 继续传递超时和取消。

### 边界与追问

Context 的 Value 查找深度随包装层数增加，不应该把它当通用 map。Context 取消也是协作式的，下游代码只有监听 `Done()` 或调用支持 Context 的 API 才会及时停止。

## 5. Defer 和错误处理在长连接里有什么价值？

### 面试官问

WebSocket Handler 里为什么有很多 defer？Go 为什么没有异常捕获作为普通错误流程？

### 30 秒回答

Go 把可预期失败作为显式 `error` 返回，调用方决定重试、降级还是退出。`defer` 适合在函数离开时成对释放资源。LinkGo 建连成功后依次登记本机连接、指标和 Redis 路由，对应的 defer 会关闭 socket、条件删除本机连接、减少连接指标并清理仍属于本连接的路由。

### 详细回答

`defer` 在当前函数返回前按后进先出的顺序执行。它的价值是把清理动作放在资源获取附近，后面无论从哪个错误分支返回，都不容易遗漏。

长连接处理有许多退出原因：客户端主动关闭、读取超时、心跳续期失败、Redis 不可用、WebSocket 写失败、旧连接失去路由所有权。每个分支手写相同的清理代码容易漏掉一项。

显式错误也让调用方区分业务拒绝和基础设施错误。例如队列满可以返回可重试的 `SERVER_BUSY`，权限不足应该拒绝且不重试，连接写失败则需要关闭连接。

### 结合 LinkGo

`WebSocketHandler` 在 `Upgrade` 后创建 `ClientConn` 并 `defer Close`。登记 `Manager.Add` 后 defer `Manager.Remove`。连接计数增加后 defer 减少。`ClaimRoute` 成功后 defer `ClearRouteIfMatch`。最后的条件删除很重要，因为无条件 `DEL route:<uid>` 可能把新连接刚写入的路由删掉。

### 为什么这样选，替代方案是什么

集中写一个 cleanup 函数也可以，但仍要保证所有返回路径都调用。`defer` 与 Go 的函数级资源生命周期契合。对于循环内部的大量资源，不应无脑 defer 到整个函数退出，否则资源会积压，这时应显式关闭或抽成小函数。

### 边界与追问

`defer` 不是分布式事务。进程被强制杀死时，defer 不保证执行，所以 Redis 路由还需要 TTL 兜底，外部持久状态也不能只依赖退出清理。

## 6. Interface 在这个项目里解决了什么问题？

### 面试官问

为什么 Gateway 使用 `api.LogicClient` 接口，而不是直接依赖具体实现？

### 30 秒回答

Go 接口用一组方法描述能力，类型只要实现这些方法就自动满足接口。Gateway 依赖 `api.LogicClient`，只知道可以调用 `Login`、`PushMessage` 等 RPC，不需要知道 Logic 内部怎样查 MySQL。这样服务边界清楚，也便于测试时换成假实现。

### 详细回答

接口的价值不是把每个结构体都再包一层，而是在调用方只需要某种能力、实现可能替换时减少耦合。Go 的隐式实现让业务类型不用声明 `implements`。

跨进程时，Protobuf 生成的 gRPC client 实现 `api.LogicClient`。Gateway 调用接口，底层负责序列化、HTTP/2 传输和响应解码。单元测试可以提供只实现目标方法的 fake client，制造成功、超时或错误，不必真的启动 Logic 和 MySQL。

### 结合 LinkGo

`pushTask` 保存 `api.LogicClient`，worker 只调用 `PushMessage`。`LogicRouterPool` 持有生成的 client，并根据直连配置或 Etcd resolver 建立底层连接。这里的接口隔离的是 Gateway 与 Logic 进程，不是为了追求抽象层数量。

### 为什么这样选，替代方案是什么

如果 Gateway 和 Logic 是一个小型单体，直接调用具体 service 结构体更简单。当前项目要演示多进程分层和独立扩容，RPC client 接口符合真实边界。接口定义过大也会让 fake 难写，实际开发可以在调用侧再定义更小的局部接口。

### 边界与追问

接口本身不会自动带来解耦。协议字段、超时、重试和错误语义仍然是跨服务契约。修改 Protobuf 要考虑客户端和服务端的版本兼容，不能只看 Go 编译通过。

