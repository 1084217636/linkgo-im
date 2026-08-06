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

如果全部放在一个进程里，连接、同步业务和大群投递会争用同一份资源，也无法按各自压力单独扩容。当前拆成三个服务，是为了隔离不同工作负载并允许分别扩容；替代方案是先做单体服务，但随着负载差异增大会更难治理。拆分的代价是进程之间必须走网络调用，还要分别配置、部署、监控和处理故障。

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

接口很少时，确实可以把解析 HTTP、业务判断和数据库调用都写进一个函数；接口增多后，这样会造成重复初始化、函数过长、测试困难，并让一次网络格式修改影响业务代码。LinkGo 因此使用 go-zero 常见的四层组织：Handler 只处理传输入口，Logic 编排一次接口流程，ServiceContext 集中保存共享依赖，Types 定义请求和响应。替代方案可以是其他分层方式，重点是职责分开，而不是目录名字必须相同。

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

这种分层的代价是文件和调用跳转变多；它也只是代码组织方式，不会自动保证业务边界完美。当前 Gateway 的部分好友、群组、红包和 AI 接口仍会直接访问 MySQL，不能仅凭目录结构声称“所有数据库操作都只在独立 Logic 服务”。

## 配置与代码的关系

代码描述“程序能做什么”，配置描述“这次运行连接哪里、监听哪个端口”。同一份二进制可以在不同环境读取不同地址。

例如本地 Redis 地址和公司 Redis 地址不同，但不需要复制两份业务代码。

## 本章代码阅读任务

执行：

```bash
rg "func main" cmd
rg "RegisterHandlers" cmd/gateway
rg "service Logic" api/protocol.proto
```

目标不是理解所有结果，而是能回答“入口在哪里”。

建议按下面顺序核对结果：

| 顺序 | 打开位置 | 这次只看什么 |
| --- | --- | --- |
| 1 | `cmd/gateway/main.go`、`cmd/logic/main.go`、`cmd/transfer/main.go` 的 `func main()` | 把三个目录与三个进程职责对应起来 |
| 2 | `cmd/gateway/internal/handler/routes.go` 的 `RegisterHandlers` | 看一个路由表怎样把路径连接到 Handler |
| 3 | `api/protocol.proto` 的 `service Logic`、`WireMessage` | 看手写协议字段，不进入生成的 `.pb.go` |
| 4 | `cmd/gateway/internal/svc/servicecontext.go` 的 `ServiceContext` | 只看它持有哪些共享依赖，不追初始化细节 |

看到这个程度就停：给你一个 HTTP 路径时，你知道先去 `routes.go`，再找 Handler、Gateway Logic 和 `ServiceContext`；给你内部消息字段时，你先看 `.proto`。暂时不必读生成文件、所有业务 Logic 或每个配置环境变量。

## 闭卷检查

1. `cmd/gateway`、`cmd/logic`、`cmd/transfer` 分别是什么？
2. `handler/logic/svc/types` 各有什么职责？
3. 为什么优先读 `.proto`，而不是背生成的 `.pb.go`？
4. 配置文件和业务代码分别决定什么？
5. 三个进程和四层目录分别解决什么问题，又各增加了什么成本？

## 源码导航与闭卷检查参考答案

三个命令应分别命中三个 `func main()`、Gateway 的 `RegisterHandlers` 和 `.proto` 中的 `service Logic`。练习完成标准不是看懂搜索结果的每一行，而是能在一分钟内重新定位它们。

1. Gateway 接入客户端并维护连接；Logic 处理消息、登录和历史等核心流程；Transfer 异步消费群投递任务。
2. Handler 处理网络输入输出，Gateway Logic 编排单次接口，ServiceContext 保存可复用依赖，Types 定义 HTTP 请求和响应结构。
3. `.proto` 是人工维护的协议契约，字段少且意图清楚；`.pb.go` 是工具生成实现，篇幅大且会随生成器变化。
4. 业务代码决定程序具备什么行为；配置决定这次运行监听哪里、连接哪些依赖和使用哪些开关。
5. 三进程拆分隔离连接、同步业务和群扩散并允许单独扩容，成本是 RPC、部署和排障变复杂；四层拆分代码职责，成本是文件和调用跳转增加。

下一步：[04 第一次 HTTP 请求](04_HTTP_FIRST_REQUEST.md)
