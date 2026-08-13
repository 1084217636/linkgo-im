# 06 登录、密码与 JWT

## 本章前置

你已经知道 HTTP 路由、Handler、Gateway 内部 Logic，以及 MySQL 表和参数化查询。

## 本章目标

读完后，你应该能够从网页按钮一直追到 MySQL，并解释密码为什么不能明文保存、JWT 能证明什么、不能证明什么。

## 1. 登录、认证和授权不是一个词

- 登录：用户提交凭据，系统尝试建立身份。
- 认证（Authentication）：确认“你是谁”。
- 授权（Authorization）：确认“你能做什么”。

账号密码登录成功，只能证明服务端愿意把某个用户身份交给客户端。后续读取群历史时，仍要判断这个用户是否属于该群。这就是授权。

## 2. 为什么密码不能明文保存

如果数据库直接保存 `123456`，数据库泄漏后攻击者立刻得到所有密码。

密码哈希把密码经过单向计算变成一串结果。验证时不是“解密”，而是用用户本次输入再次进行验证。

LinkGo 使用 bcrypt：

- 每个哈希包含随机盐，两个相同密码的结果也可以不同。
- 计算成本可调，故意比 MD5 这类快速哈希慢，降低批量暴力尝试速度。
- 数据库只需保存 bcrypt 结果。

bcrypt 的“慢”也有成本：每次登录校验都会消耗更多 CPU，成本参数过高会增加延迟，因此还需要接口限流来防止攻击者耗尽计算资源。它只降低数据库泄漏后的密码暴露风险，不能替代 HTTPS，也不能保护被日志错误记录的明文密码。哈希不可逆，用户忘记密码时应走重置流程，而不是从数据库中“解密找回”。

当前代码还兼容早期明文测试账号：如果旧明文密码校验成功，会生成 bcrypt 哈希并执行带旧值条件的 `UPDATE`。这是迁移逻辑，不代表生产系统应该继续写入明文密码。

## 3. Gateway 为什么要调用另一个进程

登录 HTTP 请求先到 Gateway，但账号查询和 Token 生成位于独立 Logic 服务。

普通函数调用只能直接调用本进程内代码。两个进程通过网络互相调用，叫远程过程调用（Remote Procedure Call，RPC）。LinkGo 使用 gRPC 作为内部 RPC 框架。

双方需要约定请求和响应字段。这个约定写在 `api/protocol.proto`：

```proto
rpc Login (LoginReq) returns (LoginReply);
```

Protobuf 是结构化二进制协议。`.proto` 定义字段，工具生成 Go 客户端和服务端代码。学习时读 `.proto` 和实际调用点，不需要背生成文件。

这里也可以选择 HTTP/JSON，或者自己设计 TCP 协议。LinkGo 选择 gRPC 和 Protobuf，是因为 `.proto` 能形成强类型契约，并生成 Go 客户端与服务端代码，适合内部服务调用。两者职责不同：Protobuf 定义并编码数据，gRPC 负责发起和承载远程调用。

代价是修改协议后要重新生成代码并保持字段兼容，二进制内容也不如 JSON 直观。更重要的是，远程调用不是本地函数：它可能超时、断网或只得到不确定结果，调用方必须设置超时并处理错误。一次 gRPC 返回成功，也只说明这次远程处理按接口返回，不自动证明聊天消息已经送到接收者。

本章只理解“一次远程调用”。Logic 多实例怎样被发现和选择在第 10 章解释。

## 4. 当前完整登录链路

```text
网页 login()
→ POST /api/v1/login，JSON 携带 username/password
→ Gateway 路由匹配 LoginHandler
→ Handler 解析为 types.LoginReq
→ Gateway 的 LoginLogic.Login
→ 通过生成的 gRPC 客户端调用 Logic.Login
→ LogicServer.Login 接收远程请求
→ Logic 侧 LoginLogic.Login
→ LogicHandler.Login 查询 MySQL users
→ 校验 status 和 bcrypt 密码
→ 生成 JWT
→ 尝试读取最近会话列表
→ gRPC 返回 LoginReply
→ Gateway 转为 LoginResp JSON
→ 网页把 token 和 user_id 保存到当前页面内存
```

这里有两个同名 `LoginLogic`：

- `cmd/gateway/internal/logic/loginlogic.go`：HTTP 接口编排并发起 gRPC。
- `cmd/logic/internal/logic/loginlogic.go`：gRPC 服务内的薄封装，最终调用核心 `LogicHandler.Login`。

不要因为名字相同就认为它们在同一个进程。

## 5. 登录失败为什么统一返回同一句话

当前核心查询：

```sql
SELECT user_id, password, status
FROM users
WHERE username = ?
LIMIT 1
```

以下情况对客户端统一返回 `invalid credentials`：

- 用户不存在。
- 密码错误。
- 账号被禁用。

这样不会通过不同错误文案帮助攻击者枚举“某个用户名是否存在”。真实数据库错误会写服务端日志，但不把内部细节直接返回给客户端。

## 6. JWT 是什么

登录请求结束后，后续的查历史、发消息仍要证明“请求者是谁”。如果服务端直接相信请求体里的 `user_id`，攻击者就能填写别人的 ID；如果每次都重传密码，又会扩大密码暴露风险。常见选择包括服务端保存 Session，或让客户端携带经过签名的 Token。

LinkGo 选择 JWT，使多个 Gateway 和 Logic 使用同一个 Secret 就能验证签名，不必为每次认证都查询一份服务端 Session。代价是当前 Token 签发后难以立即撤销，Secret 分发和轮换也必须统一管理；Token 内容还可以被解码，所以不能存放秘密。

JWT 是服务端签发的身份 Token。当前 Token 中主要包含：

```text
user_id    用户身份
issued_at  签发时间
expires_at 过期时间
```

LinkGo 使用 HS256 和共享 Secret 签名，默认有效期 24 小时。

签名的作用是：客户端如果自行修改 `user_id` 或过期时间，服务端用 Secret 验证时会发现签名不匹配。

JWT 通常写成三段：

```text
header.payload.signature
```

前两段可以被解码，因此 JWT 不是加密保险箱。不能把密码、数据库 DSN、AI Key 等秘密放入 payload。

## 7. JWT 怎样保护后续接口

浏览器调用受保护的 HTTP 接口时，可在请求头发送：

```text
Authorization: Bearer <token>
```

Gateway 的 `AuthMiddleware`：

1. 提取 Token。
2. 校验签名和过期时间。
3. 取出 `user_id`。
4. 把 user_id 放入 Go `context.Context`。
5. 调用下一个 Handler。

后续代码从 Context 获取身份，不相信请求体里由用户随便填写的发送者 ID。

WebSocket 握手目前还支持从 URL 查询参数 `token=...` 读取 Token。这方便浏览器演示，但 URL 可能出现在代理访问日志中；正式环境需要 HTTPS/WSS、日志脱敏和更谨慎的 Token 传递策略。

## 8. 登录成功为什么还要 WebSocket

JWT 只是一份身份凭证。登录接口返回成功后，本次 HTTP 请求就结束了，它不会自动变成聊天长连接。

正确顺序是：

```text
HTTP 登录得到 JWT
→ 客户端携带 JWT 发起 WebSocket 握手
→ Gateway 再次认证 Token
→ 实时连接建立
```

下一章只讲这次建连，不讲消息可靠性。

## 9. 最近会话读取失败会不会让登录失败

当前不会。

密码校验和 Token 生成成功后，`LogicHandler.Login` 会调用 `listConversations`。如果会话列表查询失败，代码记录日志，但仍返回 Token 和用户 ID，会话列表可能为空。

这是一个明确取舍：身份登录是核心结果，会话摘要属于可补充数据。但这也意味着“登录成功”不能证明会话列表一定加载成功。

## 10. 公司多实例场景下的 Secret

Logic 负责生成 Token，Gateway 负责解析 Token，因此所有相关实例必须使用相同的 `JWT_SECRET`。如果不同实例使用不同 Secret，用户可能在一台 Logic 登录成功，却在另一台 Gateway 被判定签名无效。

Secret 不能写入公开 Git 仓库中的生产配置。公司环境通常通过秘密管理系统或部署平台注入环境变量。当前仓库配置中的默认值只适合本地演示。

## 11. 当前 JWT 边界

当前已经实现：

- HS256 签名。
- user_id、签发时间和 24 小时过期时间。
- HTTP Bearer Token 和 WebSocket 查询参数解析。
- 统一认证中间件。

当前没有实现：

- Refresh Token。
- 服务端 Token 黑名单或即时撤销。
- 完整 OAuth2/SSO。
- 密钥轮换流程。
- 登录失败次数锁定和完整账号找回。

所以不能回答“JWT 签发后服务端随时可以立刻撤销”。在现有实现里，除非更换 Secret，否则已签发 Token 通常持续有效到过期。

## 本章代码阅读任务

| 顺序 | 打开位置 | 这次只看什么 |
| --- | --- | --- |
| 1 | `public/index.html` 的 `login()` | 找到请求路径、`username/password` 请求体和页面保存 `token/user_id` 的位置 |
| 2 | `cmd/gateway/internal/handler/routes.go` 的登录 Route，再看 `cmd/gateway/internal/handler/loginhandler.go` 的 `LoginHandler` | 确认 `/api/v1/login` 怎样进入 Gateway Logic |
| 3 | `cmd/gateway/internal/logic/loginlogic.go` 的 `Login` | 找到 `LogicRouter.GetClient` 和生成的 gRPC `Login` 调用，画出第一个进程边界 |
| 4 | `api/protocol.proto` 第 56 行附近的 `service Logic`，先看其中的 `rpc Login (LoginReq) returns (LoginReply);`，再向下看 `message LoginReq` 和 `message LoginReply` | 分清三件事：`rpc Login` 是 Logic 服务的方法声明，`LoginReq` 是请求消息，`LoginReply` 是响应消息。它们不在同一个代码块里；这次不读生成的 `.pb.go` |
| 5 | `cmd/logic/internal/server/logicserver.go` 与 `cmd/logic/internal/logic/loginlogic.go` 的 `Login` | 看 gRPC 请求怎样进入核心 `LogicHandler` |
| 6 | `internal/logic/handler.go` 的 `Login`、`verifyPassword`、`upgradeLegacyPassword` | 找到 users 查询、统一错误、bcrypt 校验、旧明文迁移和会话列表降级 |
| 7 | `internal/middleware/auth.go` 的 `GenerateToken`、`ParseToken`，再看 `cmd/gateway/internal/middleware/authmiddleware.go` 的 `Handle` | 确认签发字段、签名校验和 `user_id` 写入 Context 的位置 |

看到这个程度就停：你能从 `login()` 连续说出 HTTP、Gateway、gRPC、Logic、MySQL、bcrypt、JWT 和 HTTP 响应，且能指出两个同名 LoginLogic 分属不同进程。暂时不必背 JWT 库实现、HS256 数学细节、Protobuf 生成代码和 gRPC HTTP/2 帧。

## 动手练习

### 练习一：画进程边界

把登录链路画成两个大框：Gateway 进程和 Logic 进程。标出 HTTP 在哪一侧结束、gRPC 在哪两个框之间、MySQL 查询在哪一侧。

### 练习二：运行针对性测试

```bash
go test ./internal/logic -run 'TestLogin|TestVerifyPassword' -v
```

阅读测试名，说明它们分别验证“统一错误”“旧明文迁移”和“bcrypt 校验”中的哪一项。

### 练习三：判断真假

- JWT payload 天然加密：错。
- JWT 验证通过等于可以查看任意群历史：错。
- 当前 Token 默认 24 小时过期：对。
- 当前会话列表查询失败一定导致登录失败：错。

## 闭卷检查

1. 登录、认证和授权有什么区别？
2. bcrypt 为什么比明文和快速 MD5 更合适？
3. Gateway 为什么不能直接用普通函数调用另一个 Logic 进程？
4. gRPC 和 Protobuf 在登录链路各负责什么？
5. 从网页到 MySQL 的登录调用链是什么？
6. 为什么不存在用户和密码错误返回相同文案？
7. JWT 签名和加密有什么区别？
8. AuthMiddleware 把 user_id 放在哪里？
9. 当前 Token 能否即时撤销？
10. 为什么所有 Gateway 和 Logic 实例必须共享 JWT Secret？

## 动手练习与闭卷检查参考答案

### 动手练习答案

1. Gateway 框内是浏览器 HTTP、路由、`LoginHandler`、Gateway `LoginLogic`；两个框之间是 gRPC；Logic 框内是 gRPC Server、Logic `LoginLogic`、`LogicHandler.Login` 和 MySQL 查询。HTTP 响应由 Gateway 写回浏览器。
2. `TestLoginUsesGenericCredentialError` 验证不存在用户、禁用或错误密码不向客户端暴露可枚举差异；`TestLoginUpgradesLegacyPlaintextPassword` 验证旧明文校验成功后写 bcrypt；`TestVerifyPasswordSupportsBcrypt` 验证正常 bcrypt 比较。
3. 四项依次是错、错、对、错。JWT payload 可解码；资源授权仍需单独检查；默认有效期 24 小时；会话列表错误只记录日志，已完成的登录仍返回。

### 闭卷检查答案

1. 登录是提交凭据的用户动作；认证确认身份；授权判断该身份能否操作某个资源。
2. 明文泄漏后可直接使用；MD5 太快且不适合密码存储；bcrypt 带盐并故意增加计算成本，降低批量破解速度，但也需要限流和合理成本参数。
3. 普通函数调用只在同一进程地址空间内；Gateway 与 Logic 是独立进程，只能通过网络 RPC 交互。
4. Protobuf 定义并编码强类型字段；gRPC 使用这份契约发起远程方法调用并返回结果。
5. `login()` -> `POST /api/v1/login` -> `LoginHandler` -> Gateway `LoginLogic.Login` -> gRPC `Logic.Login` -> `LogicServer.Login` -> Logic `LoginLogic.Login` -> `LogicHandler.Login` -> MySQL users -> bcrypt -> JWT -> 返回。
6. 统一 `invalid credentials` 避免攻击者通过文案判断用户名是否存在或账号是否禁用；数据库内部错误只应留在受控日志。
7. 签名用于发现 payload 被篡改，内容本身仍可解码；加密才负责隐藏内容。当前 JWT 是签名 Token，不是加密容器。
8. 放入本次请求的 `context.Context`，后续 Handler 和 Logic 从 Context 取已认证 uid。
9. 当前没有黑名单或即时撤销；通常要等过期，或者轮换 Secret 使一批 Token 同时失效。
10. Logic 用 Secret 签发，所有 Gateway 用 Secret 验证。值不一致会让一个实例签发的 Token 被另一个实例判为无效。

下一步：[07 从 HTTP 升级为 WebSocket](07_WEBSOCKET_CONNECTION.md)
