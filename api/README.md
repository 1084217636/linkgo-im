# API

`api/` 目录存放 IM 系统的 API 契约，包括 Gateway HTTP API 定义、protobuf 协议和 gRPC 生成代码。

## 目录职责

- `gateway.api` 定义 Gateway 对外 HTTP API，是 go-zero API 代码生成的唯一源文件。
- `protocol.proto` 定义登录、消息推送、历史查询和 WebSocket 二进制消息帧。
- `protocol.pb.go` 和 `protocol_grpc.pb.go` 是生成产物，不手改。

## 当前协议关注点

- `WireMessage`：客户端和服务端在 WebSocket 上使用的 Protobuf 二进制帧。
- `PushMessage`：Gateway 把客户端上行二进制帧转交 Logic。
- `Login`：登录鉴权，返回 JWT 与用户 ID。
- `GetHistory`：按会话维度拉取历史消息。

## 重新生成代码

在项目根目录执行：

```bash
protoc --go_out=. --go-grpc_out=. api/protocol.proto
```

## 设计说明

协议层统一了 gRPC 和 WebSocket 的消息模型，避免外层继续依赖 JSON 文本协议。

HTTP API 定义统一放在 `api/gateway.api`，不要在 `cmd/gateway/` 下再保留一份，避免代码生成入口漂移。
