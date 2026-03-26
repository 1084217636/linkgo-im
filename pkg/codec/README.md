# Codec

`pkg/codec` 提供简单的长度头编解码能力，适合 TCP 自定义协议场景。

虽然当前项目主链路以 WebSocket 为主，但该目录保留了后续扩展原生 TCP 接入层的基础设施。

## 提供的能力

- `Encode`：把消息体包装成 `4 byte length + body`。
- `Decode`：从 `io.Reader` 按协议读取完整包体。
