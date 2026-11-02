# IM-P1 学习群任务：实现与验收记录

2026-10-28。此页记录实际代码与运行结果；原始需求和架构在 `docs/architecture/`。

## 已落地的业务链

群主/管理员创建和编辑草稿、发布、查看所有提交与进度、退回/通过审核、关闭任务。普通成员查看已发布任务、提交、看到退回理由并重交。非成员和已移除成员不能访问任务。任务事实在 MySQL，WebSocket/IM 消息负责提醒；页面收到提醒后重新查询任务状态。

| 模块 | 实际位置 | 关键约束 |
| --- | --- | --- |
| 四张业务表与系统通知账号 | `sql/20261028_learning_tasks.sql`、`sql/init.sql` | `(task_id,user_id)` 唯一、`(actor_uid,request_id)` 唯一 |
| 任务领域服务 | `internal/learningtask/` | MySQL 群成员角色鉴权；事务中锁任务、成员、提交行；版本条件、幂等事件、通知待办同事务 |
| 任务 HTTP API | `cmd/gateway/internal/handler/learningtaskroutes.go` | 复用 JWT 与限流中间件；服务端取操作者；409 版本冲突返回 `current_version` |
| 通知交接 | `internal/logic/task_notifications.go`、`cmd/logic/internal/svc/servicecontext.go` | Logic 轮询任务待办；先确认 9002 是禁用登录的保留账号，再用稳定 `client_msg_id` 转成现有 IM 消息与 `delivery_outbox`；通知正文不含提交答案和退回理由 |
| 可操作网页 | `public/task-panel.js`、`public/index.html` | 群任务列表、草稿、发布、提交/重交、审核、进度、关闭；群或私聊提醒可点开权威详情 |

## 实际执行

- `go test ./...`：通过，包含现有消息、历史、Gateway、Transfer 及新增任务/通知单测。
- 新库使用 `sql/init.sql` 初始化，随后两次执行任务增量迁移：四张任务表均存在，重复执行通过；测试库随后删除，未动业务库。
- `python3 scripts/verify_learning_tasks.py`：三个账号验证权限、草稿隐藏、发布通知、并发重复提交、退回重交、并发审核、关闭、截止时间后的幂等重放、管理员角色、移除成员后的拒绝访问、当前成员进度和历史接口；通过。脚本每次建独立验收群。
- 最后一轮任务 `lt_3e3cf5bc0bf0b9c60c79a141102075d5` 的数据库核对：`task_events` 为创建 1、发布 1、提交 2、审核 2、关闭 1；最终提交为 `approved/attempt_no=2/version=4`；6 条任务通知待办和对应 6 条消息投递待办均为 `done`，历史消息中有 6 条任务提醒。
- `python3 scripts/verify_learning_task_recovery.py`：停两个 Logic 后发布，事务与待通知行保留，恢复后历史中只有一条通知；通过。
- `python3 scripts/verify_learning_task_recovery.py --stop-service redis`：Redis 停止期间发布仍入库，worker 记录失败，恢复后只形成一条持久通知；通过。脚本用 `finally` 重启依赖。
- `node scripts/verify_learning_tasks_browser.cjs`：Chromium 两个独立页面，WebSocket 连接成功；实际点击草稿编辑/发布、成员查看群通知并提交、管理员退回、成员点击私聊状态通知并重交、管理员通过/查看进度/关闭、成员刷新后仍能看到状态；通过。该运行使用本机额外安装的 Playwright 与 Chromium。
- 2026-10-28 在 `https://127.0.0.1:8443/` 下再次完成同一浏览器闭环，登录、ticket 和 WSS 均走本机 TLS 入口；额外运行 `scripts/verify_secure_login.cjs`，确认浏览器没有 HTTP/WS 请求和长期 JWT WebSocket URL。证书为自签名，自动化测试单独允许它。
- 浏览器额外模拟“草稿已入库但 HTTP 响应丢失”：同内容重试沿用 `request_id`，数据库仍只有一份草稿；通过。任务列表和管理员提交列表均使用 cursor，后者有跨页单测。
- `python3 scripts/validate_frontend.py`、`node --check public/task-panel.js`、`git diff --check`：通过。

## 与原设计的实施修正和边界

- 任务路由注册在现有 Gateway，但没有重新生成整份 goctl 代码，避免覆盖工作区中已有未提交改动；规则集中在独立 `internal/learningtask` 包。后续如统一 API 生成，需要把这组路由纳入生成输入并删除手动注册，不能双重注册。
- 通知租约代次使用 `attempts` 的条件更新，没有单独增加 `lease_generation` 列。任务列表和管理员提交列表都使用稳定 cursor 分页。
- 任务通知 `done` 仅表示 IM 消息与现有消息投递待办已持久化，不代表客户端 ACK 或已读；外部分发由 `delivery_outbox` 接着处理。任务详情和权限始终以 MySQL 为准。
- 本轮没有课程、积分、附件上传或生产环境高可用部署。验收证明本地 Docker 配置下的业务闭环，未声称大规模性能指标。
- 验收脚本会保留自己创建的 `GIMP1*` 群及数据，便于核对事件和历史；多次运行会累积测试群。没有自动清理现有业务数据。

## Docker 手动演示

在仓库根目录运行，**全程使用同一份 `.env.docker-cn`**：

```bash
docker compose --env-file .env.docker-cn up -d mysql
docker exec -i linkgo-next-mysql mariadb -uroot -proot < sql/20260903_delivery_outbox.sql
docker exec -i linkgo-next-mysql mariadb -uroot -proot < sql/20261028_learning_tasks.sql
docker compose --env-file .env.docker-cn up -d --build
python3 -m http.server 8088 --directory public
```

新数据库由 `sql/init.sql` 自动建表；两个增量迁移也可重复执行。**不要清空已有数据库卷。**打开 `http://127.0.0.1:8088/`：

1. 窗口 A 登录 `userA / 123456`，创建群，例如 `G_STUDY_DEMO_01`，成员填 `1001,1002`。在该群“学习群任务”面板新建草稿、可修改标题/说明，再发布。
2. 隐私窗口 B 登录 `userB / 123456`，打开该群，看到群通知和任务；填文字或 http(s) 链接提交。
3. A 点“刷新任务”，查看进度和提交，填退回理由并退回。B 打开与 `9002`（学习任务助手）的私聊，点“审核退回”提醒，看到理由后修改重交。
4. A 刷新后点“通过”，进度显示通过数，再关闭任务。B 刷新或重新登录后仍能看到已通过状态。`userC / 123456` 未入群，任务 API 返回 404。

可用上述三个脚本复核 API、故障和浏览器链路。浏览器脚本另需 `npm install --prefix scripts/browser`、`scripts/browser/node_modules/.bin/playwright install chromium`，并以 `LINKGO_WEB=http://127.0.0.1:8088` 运行；Linux 缺 Chromium 系统库时按 Playwright 提示安装依赖。
