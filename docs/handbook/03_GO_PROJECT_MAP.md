# 03 看懂 Go 项目目录

## 本章前置

你已经知道一个运行中的 Go 程序是进程，也知道 LinkGo 有客户端和服务端。

## 本章目标

看到目录时不再从几百个文件乱翻，能先找到三个服务入口和公共包。

## 先认识 Go module

根目录的 `go.mod` 定义当前 Go module：

```text
github.com/1084217636/linkgo-im
```

代码中的 import 会从这个路径开始。`go.sum` 记录外部依赖版本校验，不需要背内容。

## 三个可执行服务

`cmd/` 通常放可执行程序入口：

```text
cmd/gateway   客户端接入
cmd/logic     核心消息与业务处理
cmd/transfer  群消息异步投递
```

为什么不是一个 `main.go`？因为三类工作负载不同：Gateway 维护大量连接，Logic 执行业务，Transfer 处理异步群扩散。现在只记职责，后面逐个理解。

## internal 是什么

Go 的 `internal/` 目录表示模块内部代码，外部项目不能随意 import。

LinkGo 中的主要目录：

```text
internal/server      WebSocket 连接、ACK、推送队列
internal/logic       消息业务逻辑
internal/delivery    Redis 投递适配
internal/ai          AI provider、问答和总结
internal/middleware  JWT 等通用校验
```

不要一次读完。以后的章节会指定入口。

## api 是什么

`api/protocol.proto` 是消息和内部 RPC 的协议定义。生成的 `.pb.go` 文件通常很长，不需要逐行背。

学习顺序是：先读 `.proto` 中的字段，再在需要时查看生成代码如何被调用。

## public、sql、deploy、scripts

- `public/`：网页客户端。
- `sql/`：创建数据库表的 SQL。
- `deploy/`：容器、Kubernetes 和监控配置。
- `scripts/`：演示、检查、故障注入脚本。
- `docs/handbook/`：当前唯一学习主线。

## go-zero 风格的四层

Gateway 中能看到：

```text
handler  接收 HTTP 请求并解析输入
logic    完成一个接口的流程编排
svc      保存共享依赖，例如数据库连接
types    请求和响应结构
```

第一次学习时按这个方向追：

```text
routes.go
→ 某个 handler
→ 某个 logic
→ ServiceContext 中的依赖
```

不要看到目录名 `logic` 就与独立的 Logic 服务混淆：Gateway 内的 `internal/logic` 是 HTTP 接口逻辑目录，而 `cmd/logic` 是另一个进程。

## 配置与代码的关系

代码描述“程序能做什么”，配置描述“这次运行连接哪里、监听哪个端口”。同一份二进制可以在不同环境读取不同地址。

例如本地 Redis 地址和公司 Redis 地址不同，但不需要复制两份业务代码。

## 第一次源码导航练习

执行：

```bash
rg "func main" cmd
rg "RegisterHandlers" cmd/gateway
rg "service Logic" api/protocol.proto
```

目标不是理解所有结果，而是能回答“入口在哪里”。

## 闭卷检查

1. `cmd/gateway`、`cmd/logic`、`cmd/transfer` 分别是什么？
2. `handler/logic/svc/types` 各有什么职责？
3. 为什么优先读 `.proto`，而不是背生成的 `.pb.go`？
4. 配置文件和业务代码分别决定什么？

下一步：[04 第一次 HTTP 请求](04_HTTP_FIRST_REQUEST.md)
