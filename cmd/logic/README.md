# Logic

`cmd/logic` 是业务逻辑层入口，对外暴露 gRPC 服务，负责消息编排和可靠落库。

## 负责的能力

- 处理登录校验并签发 JWT。
- 归一化 Protobuf 消息体，补全 `session_id / seq / message_id / sent_at`。
- 根据单聊或群聊场景计算投递目标。
- 单聊消息直投，群聊消息写入 Kafka。
- 异步持久化到 MySQL，支持历史消息查询。

## 启动依赖

- Redis：路由、序列号、离线消息、群组成员缓存。
- MySQL：用户信息和消息持久化。
