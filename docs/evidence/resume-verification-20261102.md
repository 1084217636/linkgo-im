# LinkGo 新版简历功能核验（2026-11-02）

## 已运行

- `go test ./...`：全部包通过。
- `python3 scripts/verify_communities.py --faults`：注册、跨社区权限、帖子并发编辑与幂等、分页、任务发布/提交/退回/通过、通知、暂停 Kafka 后 Outbox 恢复、退出后的权限撤销均通过。
- `node scripts/verify_communities_browser.cjs` 在 Playwright 1.56.1 容器内执行：真实页面注册、发帖、MinIO 附件、群聊 WebSocket、好友申请/接受/私聊、重新登录后私聊历史、任务完整流转、故障延迟和登录后持久状态均通过，无页面错误。宿主缺少 Chromium 的 `libnspr4`，使用官方 Playwright 容器运行；脚本的故障注入还需容器挂载 Docker CLI/socket。
- `python3 scripts/verify_community_observability.py`：暂停 Transfer 后 Prometheus 告警触发、Alertmanager 本地接收、恢复通知、Alloy→Loki 日志及 Grafana 数据源均通过。
- `python3 scripts/eval_community_workload.py`：本地 Docker、2 人、8 并发写入 100 帖，100 个不同帖子全部持久化并出现在 5 页动态中；20 条评论可查询，任务可审核通过。发帖墙钟 3.405 秒，约 29.37 次成功写入/秒；含限流等待的单请求 P95 为 768.35 ms，145 次 429 经退避重试。它衡量的是本地这组业务请求，不是生产容量，也没有测大群扇出。
- 新消息的 `trace_id` 与 Outbox 在同一事务写入，并被 worker 重新读出。实测同一条消息的编号可在 Logic-b、Transfer、Gateway-a 日志中找到；老 Outbox 行在迁移前没有编号，值为空，不追溯补造。

## 简历表述与代码的对应

| 描述 | 代码与证据 |
|---|---|
| Gateway/Logic/Transfer、Etcd、Redis、gRPC、ACK、离线补偿 | `cmd/gateway`、`cmd/logic`、`cmd/transfer`；`internal/server`、`internal/logic`、`internal/delivery`，已有单元/集成与浏览器验证。Gateway→Logic 是 gRPC；Logic→目标 Gateway 当前是 Redis 定向 Pub/Sub，不应说成 Gateway 间 gRPC。 |
| 多社区、帖子互动、任务审核 | `internal/community`、`internal/learningtask`、`cmd/gateway/internal/handler/communityroutes.go`、`learningtaskroutes.go`、React 社区页；API 和页面验收。 |
| 事务 Outbox、Kafka、幂等与失败重试 | `internal/logic/delivery_outbox.go`、社区事件 Outbox/Transfer；暂停 Kafka 后持久业务数据与通知恢复。客户端 ACK 与 Kafka offset 不等价。 |
| MinIO 附件与权限 | `internal/attachment`、附件路由；浏览器上传与下载权限测试。 |
| Prometheus/Grafana/Loki 与异步关联 | `docker-compose.observability.yml`、`deploy/observability`、结构化日志中的 `trace_id`/`message_id`。当前是日志关联，不是 OpenTelemetry 分布式 span 追踪。 |

## 可复现命令

```bash
./scripts/start_community.sh
go test ./...
python3 scripts/verify_communities.py --faults
python3 scripts/eval_community_workload.py
python3 scripts/verify_community_observability.py
docker run --rm --network host --user "$(id -u):$(id -g)" --group-add "$(stat -c %g /var/run/docker.sock)" \
  -v "$PWD:/work" -v /usr/bin/docker:/usr/bin/docker:ro \
  -v /var/run/docker.sock:/var/run/docker.sock -w /work \
  mcr.microsoft.com/playwright:v1.56.1-noble node scripts/verify_communities_browser.cjs
```

所有脚本使用新建测试用户和社区；不会清空已有数据库。测试账号信息由脚本写入被 Git 忽略的 `artifacts/`。
