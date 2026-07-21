# 09 安全、鉴权与权限

## 认证和授权

- 认证 Authentication：确认用户是谁，项目用账号密码 + JWT。
- 授权 Authorization：确认用户能做什么，项目用业务权限和 RBAC。

只验证 JWT 不等于有权读取任意群历史或发布活动。

## 密码

bcrypt 哈希，不保存明文；旧明文登录成功后迁移。生产还需要密码强度、登录失败限制、找回/重置和密钥轮换。

## JWT

校验签名、有效期和用户身份。Secret 放环境变量/K8s Secret。JWT payload 可被解码，不放密码和 API Key。

## WebSocket

- 握手前校验 JWT。
- 精确 Origin 白名单。
- 限制消息大小和频率。
- 心跳/超时清理死连接。
- 连接关闭时精确清理 route，避免误删新连接。

## 业务权限

- 历史消息：用户必须是会话/群成员。
- 群管理：群角色控制成员操作。
- AI 总结：复用群历史权限。
- 红包：校验会话和领取身份。
- 运营接口：独立 operator/reviewer/admin RBAC。

## 限流与背压

REST、WS 使用令牌桶限制请求速率；推送队列有界并返回 SERVER_BUSY。限流保护入口，背压保护内部队列，两者不是一回事。

## Secret

密码、JWT Secret、AI Key 不应写入 ConfigMap 或 Git。K8s 使用 Secret 注入；仓库中的演示 Secret 只能用于本地，不可直接用于生产。

## 审计

高风险运营操作记录 actor、operation、resource、result、trace 和时间。审计数据本身也需访问控制和防篡改策略。

## 常见攻击面

- SQL 注入：参数化 SQL，不拼接用户值。
- 越权：每个资源查询都校验成员/角色。
- 重放：业务 idempotency key。
- CSRF/跨站 WS：Origin、Token、SameSite/HTTPS 策略。
- 暴力登录：限流、失败计数和告警。
- 日志泄密：脱敏，不记录密码/完整 Token。
- Patch/配置注入：输入校验、允许列表和审计。

## 安全回答模板

> 我把安全分成身份、权限、输入、速率、秘密和审计六层。JWT 只解决身份；资源权限和运营 RBAC解决授权；参数化 SQL、Origin 白名单和输入校验缩小攻击面；限流和有界队列保护容量；Secret 管理敏感配置；审计保证高风险操作可追溯。

## 边界

当前是校招项目安全基线，不等于通过渗透测试、等保或生产安全审计。演示密码和本地 Secret 不能用于线上。

## 闭卷题

1. 认证和授权区别？
2. JWT 为什么不能代替 RBAC？
3. 为什么要 Origin 白名单？
4. 限流和背压区别？
5. ConfigMap 与 Secret 如何选？
6. 如何防 SQL 注入和越权？
7. 什么操作必须审计？
8. 当前安全能力有哪些边界？
