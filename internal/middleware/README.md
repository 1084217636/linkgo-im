# Middleware

`internal/middleware` 提供统一鉴权能力，当前以 JWT 为核心。

## 当前实现

- `GenerateToken`：登录成功后生成 JWT。
- `Auth`：支持从 `Authorization: Bearer <token>` 和 `ws?token=...` 两种入口提取凭证。
- `ratelimit.go`：基于内存令牌桶做请求限流。
- 解析通过后把 `user_id` 注入 Gin Context，供后续接口和 WebSocket 使用。

## 使用位置

- REST API 的鉴权路由组。
- WebSocket 握手阶段。
