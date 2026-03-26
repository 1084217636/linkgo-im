# API

`api/` 目录存放 IM 系统的 protobuf 协议和 gRPC 生成代码，是 Gateway、Logic、Transfer 之间的统一通信契约。

## 目录职责

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
