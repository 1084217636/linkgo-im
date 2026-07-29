# 16 安全、日志、指标和故障

## 本章前置

你已经走完登录、WebSocket、单聊、群聊、红包和 AI 链路。本章不会再引入新的业务功能。

## 本章目标

理解“功能能运行”与“服务可安全维护”的区别；知道怎样发现故障，而不是只会看程序有没有退出。

## 安全不是一个开关

安全需要在不同入口分别处理：

```text
登录入口      防暴力尝试、保护密码
HTTP 接口     校验 JWT 和资源权限
WebSocket     校验 JWT 和 Origin
内部服务      限制网络边界和凭据
数据库        最小权限、参数化 SQL
配置          密钥不能写进公开 ConfigMap
日志          不记录密码和完整 Token
```

### 密码

数据库不应保存可直接读出的明文密码。LinkGo 使用 bcrypt 哈希；校验时对输入密码执行 bcrypt 比较。哈希不是加密：系统不需要把原密码解密出来。

### JWT 与权限

JWT 证明请求者身份，但“已经登录”不等于“能查看任意数据”。例如查询群历史时，还要确认当前用户属于该群。这叫资源级授权。

### Origin

浏览器发起 WebSocket 握手时会携带页面来源。Gateway 使用白名单限制来源，降低恶意网页借用户浏览器建立连接的风险。Origin 校验不能替代 JWT，两者解决不同问题。

### 限流

限流限制一段时间内接受的请求数量，用来保护登录和连接入口。超过限制应明确返回错误，而不是让数据库被无限请求拖垮。

当前 TokenBucket 保存在每个 Gateway 进程内存中，并按 key 分片管理。三个 Gateway 各有自己的桶，所以它是单实例保护，不是整个集群共享一个全局配额；需要全局配额时还要引入共享限流状态或在统一入口限流。

## 日志是什么

日志是离散事件记录。理想的消息日志需要能把一条请求串起来：

```text
trace_id
message_id
client_msg_id
user_id
gateway_id
operation
result
error
duration
```

不要记录密码、完整 JWT 或 AI API Key。上面是目标字段清单，当前并非每一条日志都完整包含它们；实际排查前必须检查具体调用点，不能仅凭文档声称已经全链路结构化。

## 指标是什么

指标是可聚合的数字时间序列。当前项目已经定义：

- 当前 WebSocket 连接数。
- 每秒处理消息数。
- 推送队列深度。
- 队列满次数。
- ACK 重试次数。
- Kafka retry/DLQ 数量。

通用 HTTP 请求延迟直方图目前没有实现；若要分析每个 REST 路由的 P95/P99，需要补统一 HTTP middleware 指标。现有 `PushProcessingLatency` 和 AI provider latency 只覆盖各自局部链路。

Prometheus 定期抓取 `/metrics`；Grafana 把指标画成图。日志适合查某一次失败，指标适合发现整体趋势。

## healthz 和 readyz

两者不能混淆：

- `healthz`：进程是否活着，通常只检查自身。
- `readyz`：当前是否适合接收新流量，可以检查关键依赖。

如果把 Redis 短暂抖动直接算作 liveness 失败，平台可能反复重启本来正常的 Gateway，形成重启风暴。因此深度依赖检查更适合 readiness。

## 故障分析固定模板

遇到任何故障按五步回答：

1. 影响：哪些请求失败，哪些数据仍安全？
2. 检测：哪个日志、指标或探针能发现？
3. 隔离：是否停止新流量或暂停消费？
4. 恢复：重连、重试、租约回收还是回源？
5. 边界：当前实现仍可能丢失什么？

例如 Redis 不可用：在线路由、pending 和 Pub/Sub 受影响；已经提交到 MySQL 的历史仍在。Gateway readiness 可以失败以停止接收新流量，Redis 恢复后客户端重连并进行现有补偿。不能说“完全无影响”。

## 代码锚点

- `internal/middleware/`：JWT 等通用校验。
- `cmd/gateway/internal/middleware/`：Gateway HTTP 中间件。
- `internal/metrics/`：指标定义。
- `internal/health/`：健康检查辅助。
- `deploy/observability/`：Prometheus、Grafana 和告警配置。

## 动手练习

1. 请求 `/healthz`、`/readyz`、`/metrics`，比较输出用途。
2. 从发送一条消息的日志中找 trace_id 和 message_id。
3. 暂停 Redis 后观察 readiness，再恢复；不要在重要环境随意做故障注入。

## 闭卷检查

1. JWT 身份认证为什么不能替代群成员权限校验？
2. 日志和指标分别适合解决什么问题？
3. healthz 与 readyz 有何区别？
4. Redis 故障时哪些数据仍在，哪些能力受影响？

下一步：[17 Docker 与 GitHub Actions](17_DOCKER_AND_CI.md)
