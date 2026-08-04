# 启动、运行与调试路线

文档对应提交：`40b8b5b`
生成或最后校验时间：2026-08-04
适用分支：`main`

## 启动阅读项

### 服务入口

- 文件：`cmd/gateway/main.go`、`cmd/logic/main.go`、`cmd/transfer/main.go`
- 符号：各文件 `func main()`
- 阶段：第 0 遍，只看配置加载、ServiceContext 创建和服务启动，不追框架源码。
- 问题：每个进程创建哪些客户端？哪些依赖必须先可用？健康检查如何确认？

### WebSocket 入口

- 文件：`cmd/gateway/internal/handler/websockethandler.go`
- 符号：WebSocket handler 中的握手与连接处理函数
- 阶段：第 1 遍，在网页客户端完成一次连接后阅读。
- 问题：身份从哪里注入？连接对象何时注册？读循环、写循环和关闭信号如何结束？

### 连接状态

- 文件：`internal/server/client.go`、`internal/server/manager.go`、`internal/server/pool.go`
- 符号：`Client`、`Manager`、`NewPushWorkerPool`
- 阶段：第 2/3 遍。
- 问题：哪些状态在进程内存？锁和 channel 分别保护什么？如何观察 goroutine 泄漏或队列阻塞？

## 实际验证

先使用 `make test`、`make build`、`make docker-up`；网页调试客户端按根 README 的端口和测试账号操作。故障练习至少完成一次 Redis 不可用或 ACK 丢失，并用 `message_id`、`session_id` 和日志定位失败环节。日志默认随进程或容器输出，提交前检查不得包含 Token、密码和消息正文等敏感信息。
