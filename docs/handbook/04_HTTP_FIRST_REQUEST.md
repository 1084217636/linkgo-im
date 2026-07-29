# 04 第一次 HTTP 请求

## 本章前置

你已经知道请求、响应、IP、端口，也能找到 Gateway 入口。

## 本章目标

看懂一个 HTTP 请求怎样进入 Gateway，并区分路由、Handler、Logic 和响应。

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

## 代码锚点

按顺序阅读：

1. `cmd/gateway/internal/handler/routes.go`：找到路径。
2. 对应的 handler 文件：看输入怎样交给下一层。
3. `public/index.html` 中搜索 `/api/v1/login`：看客户端怎样发请求。

本章不要继续追数据库和 JWT，它们在第 05、06 章解释。

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

下一步：[05 MySQL 与最小数据模型](05_MYSQL_AND_DATA.md)
