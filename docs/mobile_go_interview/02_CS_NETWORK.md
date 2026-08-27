# 02 C/S 网络、HTTP、WebSocket 与 gRPC

## 1. 什么是 C/S 架构？LinkGo 的客户端和服务端分别做什么？

### 面试官问

你说项目是 Client-Server 架构，具体是什么意思？

### 30 秒回答

C/S 架构把用户交互放在 Client，把认证、业务规则、共享状态和数据持久化放在 Server。LinkGo 当前客户端是浏览器页面，HTTP 用于登录、好友、群组和历史查询，WebSocket 用于实时双向消息。服务端由 Gateway、Logic 和 Transfer 组成。未来换成桌面端或移动 App，服务端核心链路不需要因为界面变化而重写。

### 详细回答

客户端负责收集账号密码、保存登录后 token、建立长连接、编码消息、渲染会话，并在收到消息后发送 ACK。客户端不能被当成可信执行环境，因为用户可以修改网页、重放请求或自己构造协议。

服务端负责校验身份和权限，分配服务端消息 ID 与会话序号，把消息写入 MySQL，维护 Redis 在线路由，并把消息推到接收方所在 Gateway。好友关系、群成员关系和群禁言等规则必须在服务端判断。

浏览器页面只是当前演示客户端。实际产品可以让每台电脑安装客户端，也可以提供 Web 页面。它们都通过公开的 HTTP/WebSocket 协议连接同一套服务器。

### 结合 LinkGo

`public/index.html` 是当前客户端。Gateway 暴露 `/api/v1/login` 等 HTTP 路由和 `/ws` WebSocket 路由。Logic 通过 gRPC 提供登录、历史和消息处理。Transfer 不直接面对客户端，它只消费 Kafka 群任务并逐成员投递。

### 为什么这样选，替代方案是什么

把所有规则写在前端开发快，但无法防止篡改。把页面渲染也放服务端可以减少前端逻辑，却不适合实时双向消息。当前前端保持简单，把主要工程能力放在 Go 服务端，符合项目的后端求职定位。

### 边界与追问

当前网页不是完整商业客户端。它没有本地持久消息库、完善的多设备游标和系统级通知。面试时可以说它足够验证协议和主链路，不能说已经完成多端产品。

## 2. HTTP 和 WebSocket 为什么要同时存在？

### 面试官问

为什么登录用 HTTP，聊天不用每隔一秒轮询 HTTP？

### 30 秒回答

HTTP 适合一次请求对应一次响应的操作，比如登录和查询历史。聊天需要服务端主动把 B 的消息推给 A，频繁轮询会产生空请求和额外延迟。WebSocket 在一次 HTTP Upgrade 后复用同一条 TCP 连接，双方都能主动发送消息，所以适合实时 IM。

### 详细回答

普通 HTTP 的思维是客户端发起请求，服务端返回响应。服务端不能随时拿一条已经结束的 HTTP 请求再次向客户端写消息。可以用短轮询、长轮询或 Server-Sent Events 弥补，但短轮询有大量空请求，长轮询要反复重建请求，SSE 主要是服务端单向推送。

WebSocket 建连后是一条全双工通道。客户端能发普通消息、ACK 和心跳，服务端能发聊天消息、处理结果和 PONG。它减少了反复建立连接和请求头开销，也带来连接状态、心跳、断线恢复和服务端扩容等新问题。

### 结合 LinkGo

登录成功后页面保存 JWT，再调用 `connectWebSocket()` 建立 `/ws?token=...`。Gateway 的 `WebSocketHandler` 完成身份读取、可选回放授权、Logic client 获取和 Upgrade，随后把连接登记到本机 `ClientManager` 与 Redis route。

### 为什么这样选，替代方案是什么

小规模通知系统可以用 SSE，纯请求型业务继续用 HTTP。IM 需要双向低延迟通信，WebSocket 更合适。QUIC 或自定义 TCP 协议也能做，但浏览器支持、开发成本和代理兼容性更复杂，不适合当前秋招项目范围。

### 边界与追问

WebSocket 只解决长连接通信方式，不自动保证消息可靠、顺序或鉴权。它写成功也只表示数据交给连接，仍要靠 ACK 判断客户端处理结果。

## 3. HTTP Upgrade 到 WebSocket 时发生了什么？

### 面试官问

从登录成功到 WebSocket 建立，服务端具体做了什么？

### 30 秒回答

客户端先用 HTTP 登录得到 JWT，再带 token 请求 `/ws`。AuthMiddleware 验签并把 user id 放进 Context，限流中间件限制频繁建连。Handler 校验可选的回放会话权限并取得 Logic client，然后 Gorilla WebSocket 检查 Origin 并执行 Upgrade。成功后服务端创建连接 ID，登记本机连接和 Redis 路由，再回放短期未确认消息并进入读循环。

### 详细回答

Upgrade 之前，请求仍是普通 HTTP。客户端请求头声明希望升级协议，服务端返回 `101 Switching Protocols` 后，连接才进入 WebSocket 帧通信。之后不能再把这条连接当普通 REST 请求处理。

认证必须发生在 Upgrade 前。如果先升级再发现没有身份，服务器已经为恶意请求占用了长连接资源。Origin 是浏览器提供的网页来源信息，当前代码按允许列表检查，它是浏览器场景的额外保护，不代替 JWT。原生 App 可能没有 Origin，因此代码通过配置决定是否允许缺失。

Upgrade 成功后，服务端生成 `ConnectionID`。本机 map 保存 `uid -> ClientConn`，Redis 保存 `route:<uid> -> gatewayID|connectionID`。两者分别解决本进程写 socket 和跨进程定位 Gateway。

### 结合 LinkGo

入口是 `cmd/gateway/internal/handler/websockethandler.go`。`server.Manager.Add` 登记本机连接，`ClaimRoute` 写 Redis，`SyncOfflineMessagesWithDB` 做重连补偿，最后 `StartClientLoopWithDB` 区分 ACK、HEARTBEAT 和普通消息。

### 为什么这样选，替代方案是什么

可以在 WebSocket 建立后发送第一帧认证，但服务端要维护未认证连接的短时状态和超时，攻击面更大。当前把 HTTP 阶段已有的 JWT 中间件复用到握手阶段，流程更简单。公开部署时把 JWT 放 URL 可能进入代理日志，更严格的方案是先用 JWT 换一次性短期 WS ticket，当前没有实现。

### 边界与追问

Origin 不是用户身份，也不是加密。TLS 才保护传输内容。当前本地演示常用 `ws://`，真实公网应使用 HTTPS/WSS 并妥善管理 token。

## 4. JWT 是加密吗？服务端怎样验证它？

### 面试官问

JWT 全称是什么？用户能不能修改里面的 user id？

### 30 秒回答

JWT 全称是 JSON Web Token。LinkGo 使用它承载 user id 和过期时间，并对 header 和 payload 做签名。它默认不是加密，客户端可以看到 payload，但修改后签名会不匹配。服务端验证签名算法、密钥和过期时间后，才把 Claims 中的 user id 当成已认证身份。

### 详细回答

JWT 通常由 `header.payload.signature` 三段组成。Header 说明算法，Payload 保存 Claims，Signature 用服务端密钥对前两段计算签名。客户端拿到 token 后在后续 HTTP 的 `Authorization: Bearer <token>` 中提交，当前 WebSocket 握手也支持 query token。

签名回答的是完整性和来源问题：内容有没有被改，是否由持有密钥的服务签发。它不隐藏内容。因此 JWT 里不能放密码和敏感隐私。服务端还必须检查过期时间和允许的算法，不能只做 Base64 解码。

### 结合 LinkGo

登录由 Logic 查询 MySQL 并验证密码，成功后 `GenerateToken` 签发 token。Gateway 的 AuthMiddleware 使用 `ExtractBearerToken` 或 WebSocket query 取得 token，`ParseToken` 成功后用 `context.WithValue` 保存 Claims.UserID。后续 Handler 通过 `UserIDFromContext` 取身份。

### 为什么这样选，替代方案是什么

JWT 的好处是 Gateway 验证签名时不需要每个请求查询 session 表，适合多 Gateway。传统随机 session id 配合 Redis 也可行，优点是容易服务端强制失效，代价是每次认证依赖共享存储。当前项目选 JWT 是为了无状态验签和学习标准认证链路。

### 边界与追问

签发后的 JWT 在过期前不容易单独撤销，除非加黑名单或版本号。密钥泄漏会影响所有 token。认证只证明“你是谁”，能不能读取群历史或给某人发消息还要做授权检查。

## 5. Gateway 到 Logic 为什么使用 gRPC 和 Protobuf？

### 面试官问

服务内部为什么不用 HTTP JSON？

### 30 秒回答

Gateway 和 Logic 是明确的服务边界，LinkGo 用 gRPC 做内部 RPC，用 Protobuf 定义强类型协议。Protobuf 生成 client 和 server 接口，字段契约集中，二进制编码比 JSON 紧凑。gRPC 也提供超时、连接复用和健康检查。代价是调试不如纯 JSON 直观，协议演进必须遵守字段兼容规则。

### 详细回答

RPC 让调用方像调用本地方法一样写代码，但实际过程包括序列化、网络传输、服务端反序列化和返回。任何一次调用都可能超时、断连或只执行了一部分，所以不能真把它当本地函数。

Protobuf 中定义消息字段和服务方法，生成的 Go 代码负责编码与接口样板。服务端 `LogicServer` 接收生成的请求类型，再把调用交给业务 Logic。Gateway 持有 `api.LogicClient`，底层 gRPC 连接通过 HTTP/2 复用。

### 结合 LinkGo

Gateway 普通消息最终调用 `api.LogicClient.PushMessage`，传入已认证 `UserId` 和原始 Protobuf 内容。Logic 的 gRPC server 再调用核心 `LogicHandler.PushMessage`，完成权限、幂等、seq、MySQL 和投递。Gateway 的 readiness 还通过标准 gRPC Health Check 检查 Logic。

### 为什么这样选，替代方案是什么

HTTP JSON 对跨语言和人工调试很友好，小项目也更简单。当前 Go 服务之间调用频繁、协议字段明确，gRPC 的生成契约更合适。消息队列不适合替代所有 RPC，因为登录和历史查询需要同步响应。

### 边界与追问

gRPC client 生成成功不代表调用必然成功。必须设置 timeout，设计幂等，并明确某次超时后服务端是否可能已经落库。Protobuf 删除字段时不能随意复用旧字段号。

## 6. 负载均衡怎样处理 WebSocket 长连接？

### 面试官问

十台 Gateway 连接满了，加一台服务器，旧连接会自动过去吗？

### 30 秒回答

不会。入口负载均衡主要为新的 TCP/WebSocket 握手选择健康 Gateway。已经建立的连接状态在原 Gateway 的内存和内核里，增加 Gateway 只能承接后续新连接。旧节点下线时要先停止接收新连接，等待或通知客户端断开，再由客户端连接稳定入口，Redis 新 route 覆盖旧 route，并通过 ACK 状态和历史数据恢复消息。

### 详细回答

负载均衡是把请求或连接分配到多个服务实例。短 HTTP 请求结束快，下一次请求可以落到另一个实例。WebSocket 连接可能保持很久，一旦握手落在 Gateway-1，后续帧都沿同一 TCP 连接进入 Gateway-1。

因此 Gateway 是有状态接入服务。它的业务数据主要放外部存储，但活跃 socket 无法共享。Kubernetes 新建 Pod 后，Service 会把新握手分给 ready Pod，不会复制旧 socket。

### 结合 LinkGo

每个 Gateway 只维护自己的 `ClientManager`。用户重连后 `ClaimRoute` 写入新的 `gatewayID|connectionID`。旧连接的心跳通过 `RefreshRouteIfMatch` 发现自己不再是 owner 后退出，旧 defer 使用比较删除，不能删掉新 route。

### 为什么这样选，替代方案是什么

试图在应用层迁移 TCP 状态非常复杂，也不适合秋招项目。通用方案是客户端自动重连、服务端外置会话进度，并给旧实例 draining 时间。若需要更均匀的连接分布，可以在缩扩容时主动通知部分客户端重连，但这仍是重建连接，不是无损搬迁。

### 边界与追问

当前项目能说明多 Gateway 路由和重连补偿，但没有真实云负载均衡资源，也没有证明大规模连接迁移。`replicas` 数量不等于物理服务器数量，多个 Pod 可能在同一 Node。

