# 04 第一次 HTTP 请求

## 本章前置

你已经知道请求、响应、IP、端口，也能找到 Gateway 入口。

## 本章目标

看懂一个 HTTP 请求怎样进入 Gateway，并区分路由、Handler、Logic 和响应。

## 为什么这里选择 HTTP

登录、查询历史和处理好友关系都是“客户端发起一次操作，服务端返回一次明确结果”。如果直接使用自定义 TCP 数据格式，项目还要自己约定操作名称、输入格式、错误表示和浏览器接入方式。LinkGo 选择 HTTP 和 JSON，是因为浏览器可以直接用 `fetch()`，方法、路径和状态码也能组成清晰的接口契约。

替代方案并非不存在，例如内部服务可以使用 gRPC，自定义客户端也能使用私有协议。HTTP/JSON 的代价是文本和请求头会增加一些传输与解析开销，而且一次请求/响应不适合持续主动推送聊天消息；实时推送会使用后面介绍的长连接。本章先处理适合 HTTP 的一次性交互。

## HTTP 先学四个部分

一个 HTTP 请求至少包含：

```text
方法     GET / POST 等
路径     /api/v1/login
请求头   Content-Type、Authorization 等
请求体   例如 JSON 数据
```

响应包含状态码、响应头和响应体。

常见状态码：

- `200`：成功。
- `400`：客户端输入不合法。
- `401`：身份未通过。
- `403`：身份已知但没有权限。
- `500`：服务器内部错误。

## JSON 是什么

JSON 是文本数据格式。登录请求可以写成：

```json
{
  "username": "userA",
  "password": "123456"
}
```

Go 服务会把 JSON 转成请求结构体，这个过程叫反序列化；返回结构体转成 JSON 叫序列化。

## 路由是什么

路由把“方法＋路径”映射到处理函数：

```text
POST /api/v1/login → LoginHandler
GET  /healthz      → HealthHandler
```

LinkGo 的路由注册入口是：

```text
cmd/gateway/internal/handler/routes.go
```

## Handler 为什么不写全部业务

Handler 面向网络层，通常负责读取请求、调用下一层、写响应。业务流程放到 Logic，避免一个函数同时处理 HTTP 格式、数据库和业务规则。

```text
HTTP 请求
→ Router 找到 Handler
→ Handler 解析请求
→ Gateway Logic 编排
→ 返回结果
→ Handler 写 HTTP 响应
```

这里的 `Gateway Logic` 指 Gateway 目录中的接口逻辑，不是独立 Logic 进程。下一章之后才会追到远程 Logic 服务。

## 健康检查作为第一个例子

先看不涉及登录和数据库的健康接口：

```text
GET /healthz
```

它用来回答“Gateway 进程是否还活着”。浏览器或命令行可以请求：

```bash
curl http://127.0.0.1:8090/healthz
```

现在只理解请求进入和响应返回，不讨论 Kubernetes 为什么需要它。

## 前端怎样调用 HTTP

`public/index.html` 使用浏览器自带的 `fetch()`：

```text
页面点击按钮
→ JavaScript 调 fetch
→ Gateway 收到 HTTP 请求
→ Promise 得到响应
→ 页面更新状态
```

## 本章代码阅读任务

| 顺序 | 打开位置 | 这次只看什么 |
| --- | --- | --- |
| 1 | `cmd/gateway/internal/handler/routes.go` 的 `RegisterHandlers` | 找到 `/healthz`、`/readyz` 和带 `/api/v1` 前缀的 `/login`，看方法与 Handler 怎样配对 |
| 2 | `internal/health/health.go` 的 `LiveHandler`、`ReadyHandler` | 比较只返回存活与逐项执行依赖检查的差异 |
| 3 | `cmd/gateway/internal/handler/loginhandler.go` 的 `LoginHandler` | 只看 JSON 怎样进入 `types.LoginReq`，以及结果怎样写回 HTTP |
| 4 | `public/index.html` 的 `apiFetch()` 与 `login()` | 找到 `POST /api/v1/login`、请求体和页面处理响应的位置 |

看到这个程度就停：你能从浏览器的路径反查到 `RegisterHandlers` 和对应 Handler，并能解释 Handler 负责网络格式。暂时不追 `LoginLogic.Login` 后面的 gRPC、MySQL 和 JWT。

## 动手练习

启动后端后依次执行：

```bash
curl -i http://127.0.0.1:8090/healthz
curl -i http://127.0.0.1:8090/readyz
```

观察状态码和响应体差异。后者为什么可能失败留到工程化章节。

## 闭卷检查

1. HTTP 请求的四个主要部分是什么？
2. `401` 和 `403` 有什么区别？
3. Router、Handler、Logic 各负责什么？
4. 前端的 `fetch()` 与 Gateway 的路由怎样对应？
5. 登录为什么适合 HTTP，而实时消息推送为什么不只靠普通 HTTP？

## 动手练习与闭卷检查参考答案

练习中，`/healthz` 只说明 Gateway 进程能响应，正常时返回成功；`/readyz` 会运行 Logic、Redis 和 MySQL 检查，任一关键依赖异常都可能返回非成功状态。两者都成功仍不能证明一条聊天消息端到端可用。

1. 方法、路径、请求头和请求体。
2. `401` 表示身份没有通过；`403` 表示身份已知，但没有目标资源或操作权限。
3. Router 根据方法和路径选 Handler；Handler 解析网络输入并写响应；Logic 编排业务步骤。
4. `login()` 把 `/api/v1/login`、POST 和 JSON 交给 `apiFetch()`；Gateway 用同样的方法和路径在 `RegisterHandlers` 中匹配 `LoginHandler`。
5. 登录是一次输入对应一次结果，HTTP 契合请求响应；聊天下行要求服务端在任意时刻主动推送，普通 HTTP 请求结束后没有持续通道，所以项目另建 WebSocket。

下一步：[05 MySQL 与最小数据模型](05_MYSQL_AND_DATA.md)
