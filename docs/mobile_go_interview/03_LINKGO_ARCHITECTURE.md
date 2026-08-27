# 03 LinkGo 服务分层与登录建连

## 1. LinkGo 的整体架构是什么？

### 面试官问

先不要讲 Redis 和 Kafka细节，用一分钟介绍服务架构。

### 30 秒回答

LinkGo 是一个面向多节点部署的 C/S 即时通信系统。客户端通过 HTTP 登录和查询，通过 WebSocket 收发实时消息。服务端按 Gateway 接入层、Logic 业务层和 Transfer 异步投递层拆分。Gateway 管理连接和协议，Logic 处理身份、权限、幂等、会话序号和持久化，Transfer 消费 Kafka 群任务并逐成员投递。MySQL 保存长期事实，Redis 保存在线路由和短期补偿状态，Etcd发现 Logic 实例。

### 详细回答

一次客户端请求先到 Gateway。HTTP 请求经过路由和中间件，WebSocket 连接还会登记本机连接状态。Gateway 不应该把好友关系、群权限和消息事实只放在内存里，因为它会多实例运行，也会重启。

Logic 是核心业务服务。登录时它查询用户，消息发送时它使用认证用户身份校验发送者，检查好友或群成员权限，处理 `client_msg_id` 幂等，分配会话 `seq`，把消息写入 MySQL，再组织在线投递或群任务。

Transfer 是群聊的执行层。Logic 不在一次 RPC 中同步循环写所有群成员，而是把包含 recipients 的任务写入 Kafka。Transfer 逐接收者调用 RedisDelivery，失败任务进入 retry 或 DLQ。

### 结合 LinkGo

三个 main 分别在 `cmd/gateway`、`cmd/logic` 和 `cmd/transfer`。Gateway 到 Logic 用 gRPC。Logic 和 Transfer 都通过 Redis 的定向 Gateway 频道完成最后一跳通知。每个 Gateway 只持有本机 WebSocket。

### 为什么这样选，替代方案是什么

单体服务更容易开发和部署，项目早期完全可以先用单体。当前拆分不是因为“微服务更高级”，而是三类负载不同：连接接入、核心业务、群扩散。它们的扩容方式和故障影响不同，拆分后可以独立处理。代价是增加 RPC、服务发现、配置和故障窗口。

### 边界与追问

部分好友、群、红包和 AI HTTP 接口当前仍在 Gateway 直接访问 MySQL，分层不是完全纯粹。面试时应说主消息链路按三层拆分，不要声称所有业务都只能经过 Logic。

## 2. Gateway 为什么是有状态的，Logic 为什么不做用户粘性？

### 面试官问

哪些服务有状态？同一个用户是否必须永远请求同一个 Logic？

### 30 秒回答

Gateway 持有 WebSocket 对象和本机连接表，所以是有状态接入服务。Logic 不依赖某个用户只存在某一实例的内存状态，业务事实放在 MySQL 和 Redis，因此一次 gRPC 可以由任意健康 Logic 处理。当前 `p2c_ewma` 是负载均衡，不是按用户做一致性哈希。

### 详细回答

“有状态”不是指服务是否访问数据库，而是某个实例本身是否拥有请求继续执行所必需、又不能随时从共享存储恢复的状态。WebSocket 连接绑定进程和内核 socket，所以 Gateway-1 的连接不能由 Gateway-2 直接写。

Logic 处理一次请求需要的用户、关系、消息和幂等状态来自共享 MySQL/Redis。它不维护“用户 A 永远属于 Logic-2”的本机 map，因此 Gateway 可以选择任意健康实例。这样一个 Logic 挂掉后，后续重试可以落到别的 Logic。

### 结合 LinkGo

`LogicRouterPool.GetClient` 虽然接收 `key` 参数，但当前没有用它固定路由。底层 zRPC client 配置 `p2c_ewma`，根据候选和近期延迟选择实例。在线用户位置则明确写在 `route:<uid>`，值是 `gatewayID|connectionID`。

### 为什么这样选，替代方案是什么

如果 Logic 内存维护某用户的唯一会话状态，就需要一致性哈希、迁移和故障恢复，复杂度明显上升。当前把连接状态留给 Gateway，把业务事实外置，Logic 更容易横向扩容。代价是每次请求需要访问共享存储，数据库和 Redis 成为关键依赖。

### 边界与追问

Logic 仍会有连接池、缓存等进程内资源，只是这些不是用户唯一事实。`p2c_ewma` 不保证全局最优，也不保证某次正在执行的 RPC 在 Logic 崩溃后自动转移。

## 3. Logic 服务发现和负载均衡怎样工作？

### 面试官问

Gateway 怎样知道当前有哪些 Logic？健康列表怎样更新？

### 30 秒回答

Logic 启动后把可访问地址注册到 Etcd 的 `services/logic`，注册记录带租约，需要持续续期。异常退出后租约到期，发现端会移除该地址。Gateway 未配置固定 `LOGIC_ADDR` 时使用 Etcd resolver 获得实例列表，zRPC 再通过 `p2c_ewma` 为调用选择后端。Gateway readiness 还会调用标准 gRPC Health Check。

### 详细回答

服务注册中心解决的是实例地址会变化的问题。Logic Pod 监听 `0.0.0.0:9001`，这个地址只表示监听所有本机网卡，不能让其他服务把 `0.0.0.0` 当目标。Kubernetes 通过环境变量注入 `POD_IP`，注册端使用实际 Pod IP 和端口。

Etcd 租约类似带过期时间的登记。Logic 活着时续租，异常退出后记录最终消失。Gateway 的 resolver 观察 Key 变化并更新 gRPC client 可选地址。当前 balancer 名称是 `p2c_ewma`，大意是比较少量候选并结合近期延迟选择。

### 结合 LinkGo

`cmd/gateway/internal/svc/logicrouter.go` 有两条路径：`Logic.Addr` 非空时直连；为空时用 Etcd endpoints 和 `services/logic`。client timeout 当前设置为 2 秒。`Ready` 等连接状态到 Ready 后，再执行 gRPC Health Check 并要求 `SERVING`。

### 为什么这样选，替代方案是什么

固定地址适合本地最小运行。Kubernetes Service 也能提供稳定地址，但 gRPC 常复用长连接，单条连接可能长期粘在一个后端。客户端发现让 balancer 看到实际实例。代价是应用依赖 Etcd，配置和故障排查更复杂。

### 边界与追问

Etcd 移除实例有租约延迟，在途 RPC 仍可能失败。Health Check 只证明 Logic 当前报告可服务，不证明某条消息一定能写 MySQL 并送到用户。

## 4. 从账号密码到登录成功，完整链路是什么？

### 面试官问

请从页面输入账号密码开始讲，不要跳过进程边界。

### 30 秒回答

页面调用 `POST /api/v1/login`，Gateway 的登录 Handler 解析 JSON，通过 LogicRouter 取得 gRPC client，并调用 Logic.Login。Logic 查询 MySQL 用户记录，验证密码和账号状态，签发带 user id 和过期时间的 JWT，再查询会话摘要。结果经 gRPC 返回 Gateway，Gateway 转成 HTTP JSON，页面保存 token、user id 和会话列表，然后建立 WebSocket。

### 详细回答

页面的用户名和密码只出现在登录请求体。Gateway 的公共登录路由不要求 JWT，但会经过 REST 限流。Handler 把请求交给 Gateway 层 LoginLogic，后者通过生成的 gRPC client 发给独立 Logic 进程。

LogicServer 是 gRPC 适配层，它创建具体 LoginLogic，再调用核心 `LogicHandler.Login`。核心代码查询 `users`，处理不存在、密码错误和状态异常等情况。认证成功后签发 JWT，并尝试读取会话列表。

会话列表查询失败时，当前策略可以记录错误但仍返回 token 和 user id。这表示身份认证成功不必被非核心的会话摘要暂时失败拖死。页面可以后续查询或恢复列表。它不表示客户端网络发送中的消息会自动转圈重试，这是两个不同场景。

### 结合 LinkGo

这里有两个名字相似的 LoginLogic。Gateway 目录里的对象负责 HTTP 到 gRPC，Logic 目录里的对象负责 gRPC server 到核心 handler。拆开后生成框架的运输层与手写业务层保持清楚。

### 为什么这样选，替代方案是什么

Gateway 可以直接查用户表并签 token，但这样认证逻辑会散在接入层，多 Gateway 升级也更难保持一致。放到 Logic 后可以集中业务规则。会话摘要可以改成登录必须成功才返回，但一次摘要查询故障会阻断所有登录，当前选择降级返回基本身份。

### 边界与追问

JWT 是签名，不是密码加密。当前代码兼容 bcrypt 和旧密码迁移，面试中不能主张使用明文更简单。密码安全是认证底线，即使项目重点是架构也不应退回明文。

## 5. 用户上线时服务器保存了哪些状态？

### 面试官问

WebSocket 建立以后，怎样让其他服务器找到这个用户？

### 30 秒回答

Gateway 为连接生成 `ConnectionID`，本机保存 `uid -> ClientConn`。同时它在共享 Redis 写 `route:<uid> = gatewayID|connectionID`，并维护 Gateway 用户集合、连接反查和活跃 Key。Logic 先读 route 找到目标 Gateway，再发布到该 Gateway 的专属频道，目标 Gateway 最后查本机 map 写 socket。

### 详细回答

本机 map 和 Redis route 解决不同问题。本机 map 里有真正的 WebSocket 对象，其他进程无法直接引用。Redis route 是可共享的位置索引，只告诉 Logic 去哪个 Gateway，也不能直接替代 socket。

路由值包含连接 ID，是为了处理同一用户快速重连。新连接可以覆盖旧 route，旧连接的心跳只能在 route 仍等于自己时续期，退出清理也只能比较删除自己的值。

Redis 还保存 `gateway_users:<gatewayID>`，便于一个 Gateway 扫描自己负责的用户；`gateway_conn:<gatewayID>:<connectionID>` 做连接反查；`gateway_live:<gatewayID>` 保存活跃标记。

### 结合 LinkGo

`ClaimRoute` 写入这些 Key。客户端心跳调用 `RefreshRouteIfMatch` 原子比较并续期。Handler 退出时调用 `ClearRouteIfMatch`。`ClientManager.Remove` 同样使用 `CompareAndDelete` 防止旧连接误删新连接。

### 为什么这样选，替代方案是什么

可以把所有用户连接广播给所有 Gateway，但连接数增长后同步成本很高。也可以让每条消息广播到所有 Gateway 再本地查找，这会浪费网络和 CPU。定向 route 加专属频道让在线通知只到目标 Gateway。

### 边界与追问

当前 `route:<uid>` 是单值，只表示一个当前连接，不支持同账号多设备各自独立路由。进程被强杀时清理函数可能不执行，所以 route 有 TTL。

## 6. 为什么目录里有很多 internal 和 logic？

### 面试官问

你怎样解释项目中重复出现的 `logic`、`internal` 和 `handler`？

### 30 秒回答

Go 的 `internal` 是编译期包可见性边界，只允许父目录树内部导入。`cmd/gateway/internal` 和 `cmd/logic/internal` 分别是两个可执行服务的私有代码。`handler` 通常接收传输层请求，`logic` 负责当前服务的一次用例编排，`svc` 保存共享依赖。仓库根部 `internal/logic` 是跨入口复用的核心 IM 业务实现，所以名字相似，但所属进程和职责不同。

### 详细回答

`cmd/<service>` 通常对应一个可执行程序。每个程序都有自己的配置、ServiceContext、Handler 或 gRPC Server。go-zero 生成的目录习惯会让 HTTP LoginLogic、gRPC LoginLogic 和核心 LogicHandler 同时存在。

可以按进程边界理解，而不是按文件名猜：

```text
浏览器
→ Gateway handler
→ Gateway logic 适配
→ 生成的 gRPC client
→ Logic server
→ Logic logic 适配
→ 根 internal/logic 核心业务
```

每层函数可能只有几行，但它让框架生成代码、网络运输代码和手写核心逻辑不混在一起。

### 结合 LinkGo

Gateway 的 `loginlogic.go` 负责取得 gRPC client。Logic 服务的 `logicserver.go` 实现生成的 server 接口。`cmd/logic/internal/logic/loginlogic.go` 再调用 `svcCtx.Core.Login`。真正查 MySQL、验密码和签 JWT 的代码在根目录 `internal/logic/handler.go`。

### 为什么这样选，替代方案是什么

小项目可以少分两层，减少跳转。当前结构部分来自 go-zero 生成约定，优点是边界统一，重新生成路由或 RPC 样板时不碰核心代码。代价是初学者会看到多个同名类型，必须先标记所属进程。

### 边界与追问

分层不等于每层都必须有复杂逻辑。很薄的适配层可以接受，但如果只增加转发且没有框架边界或替换价值，就要考虑合并。面试时不要说“层数越多越专业”。

