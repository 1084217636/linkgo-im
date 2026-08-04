# LinkGo Chat 学习入口

文档对应提交：`40b8b5b`（生成时基线；行号变化时以符号名为准）
生成或最后校验时间：2026-08-04
适用分支：`main`

## 学习目标

本项目不是用来背组件名，而是沿真实代码掌握一条 IM 消息从 WebSocket 进入、经 Logic 处理、写入 MySQL/Redis、跨 Gateway 投递并完成 ACK 的完整链路。当前学习者具备 Go 基础语法，但还不能独立阅读大型 Go 服务。

## 推荐顺序

1. 先按 `docs/handbook/README.md` 启动并完成最小登录。
2. 阅读 `cmd/gateway/main.go` 的 `main`，再看 `internal/server/client.go` 的连接读写循环。
3. 沿单聊链阅读 `cmd/gateway/internal/handler/websockethandler.go`、`cmd/logic/internal/logic/pushmessagelogic.go`、`internal/delivery/redis.go` 和 `internal/server/ack.go`。
4. 关闭 Redis 或丢弃 ACK，记录一次失败现象，再回到代码定位原因。
5. 完成一个相邻小功能前，先提交自己的修改面预测，不直接让 Codex 生成完整补丁。

## 事实边界

- 【当前已实现】Gateway、Logic、Transfer、MySQL、Redis、Kafka、Etcd 和 WebSocket 相关代码均以当前仓库和测试为准。
- 【上游能力】go-zero、WebSocket/Redis/Kafka 客户端提供基础设施封装。
- 【未来方案】未实测的吞吐、自动容灾、完整生产 Kubernetes 能力不得写入简历为已实现。
