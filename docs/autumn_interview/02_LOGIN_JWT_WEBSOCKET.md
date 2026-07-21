# 02 登录、JWT 与 WebSocket 建连

## 1. 完整链路

```text
POST /api/v1/login
-> Gateway LoginHandler
-> Gateway LoginLogic
-> zRPC 调 Logic.Login
-> MySQL 查询 users
-> bcrypt 校验密码
-> 生成 JWT
-> 返回 token 和会话列表

GET /api/v1/ws?token=...
-> AuthMiddleware 校验 JWT
-> Origin 白名单校验
-> HTTP Upgrade 为 WebSocket
-> 创建 Client 并加入 Manager
-> Redis 写 route:<uid>
-> 开始读写循环
-> 回放 pending/offline/timeline 缺失消息
```

## 2. 为什么先 HTTP 登录再 WebSocket

HTTP 更适合一次性请求响应和错误码；WebSocket 更适合登录后的持续双向消息。把身份验证结果放进 JWT，后续 REST 和 WS 可以复用同一身份。

## 3. JWT 必背

JWT 包含用户身份和过期时间，并使用服务端 Secret 签名。

它解决：无须每次请求查询登录 Session，就能验证 Token 是否由服务端签发、是否过期。

它不解决：

- Token 被盗后的天然撤销。
- 接口权限模型。
- 数据加密；JWT payload 不是秘密保险箱。

标准回答：

> JWT 负责认证“你是谁”，RBAC 和业务权限负责授权“你能做什么”。生产中还需要 HTTPS、短过期、刷新/撤销策略和 Secret 管理。

## 4. 密码安全

当前使用 bcrypt 保存密码哈希，不保存可逆明文。兼容旧数据时，明文校验成功会迁移为 bcrypt。

为什么不能 MD5：速度太快，攻击者可以高速暴力枚举；bcrypt 自带盐且计算成本可调。

## 5. WebSocket 基础

WebSocket 从 HTTP 握手升级为一条长连接，同一条连接可由客户端和服务端主动发送消息。

项目需要处理：

- 握手鉴权。
- Origin 白名单。
- 心跳和超时。
- 单连接读循环。
- 受控写入和发送队列。
- 连接关闭与 Redis 路由清理。
- 重连补偿。

## 6. Origin 为什么要校验

浏览器会携带 Origin。若服务端接受任意 Origin，恶意网页可能利用用户已有身份发起跨站 WebSocket 连接。项目使用精确白名单，不使用随意的字符串包含判断。

## 7. 在线路由

```text
route:<uid> = gatewayID|connID
gateway_users:<gatewayID> = 该网关用户集合
gateway_conn:<gatewayID>:<connID> = uid
gateway_live:<gatewayID> = 心跳
```

作用：Logic/Delivery 知道用户在哪个 Gateway；Gateway 重启或连接关闭时可以精确清理。

竞争风险：用户快速重连产生新连接时，旧连接关闭不能误删新连接路由。因此删除时要校验 connection identity，而不是无条件 DEL。

## 8. 多 Gateway 如何投递

每个 Gateway 只持有自己的 WebSocket。Redis 路由指出目标 Gateway；跨节点实时通知通过定向 Pub/Sub 到目标 Gateway，再由目标 Gateway 找本机连接写出。

Pub/Sub 不可靠，所以它只负责在线通知，不能代替 pending、offline 和 MySQL 历史。

## 9. 常见故障

### JWT 过期

握手返回未授权，客户端重新登录获取 Token。

### Redis 不可用

Gateway readiness 失败，不应继续接新流量；已有连接的可靠状态无法正常维护。

### Logic 不可用

Gateway readiness 现在会检查 Logic gRPC 连接，K8s 可停止向未就绪实例导流。

### Gateway 崩溃

内存连接消失，客户端重连；Redis 心跳和反向索引用于识别/清理旧路由，pending/timeline 用于补偿消息。

## 10. 边界

- 当前 JWT 方案不是完整 OAuth2/SSO。
- 当前 ACK 是收件确认，不是已读状态。
- WebSocket 单机连接数不等于带真实消息负载的系统吞吐。
- 本地演示 Origin 白名单和生产域名配置需要区分。

## 11. 闭卷题

1. 登录为什么走 HTTP？
2. JWT、RBAC 分别负责什么？
3. bcrypt 比 MD5 好在哪里？
4. WebSocket 建连时做哪些检查？
5. 为什么要校验 Origin？
6. route key 保存什么？
7. 旧连接为什么可能误删新路由？
8. Gateway 崩溃后怎么恢复？
