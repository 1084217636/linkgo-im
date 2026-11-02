# 2026-11-02 多社区版本：实施与验收

本轮在 `linkgo-collab-next` 继续开发，保留原有未提交的 IM、任务、附件等修改，没有重置工作区、清空数据库或复制覆盖旧项目。此文件对应多社区新版本；旧资料中的“注册、帖子、通知中心未实现”“附件只有共享卷”等描述，以本文件为准。

## 完成的业务

- 注册、bcrypt 密码哈希、现有 JWT 登录；HTTPS/WSS 产品入口，一次性 WS ticket。
- 多社区创建、发现、我的社区、加入、退出、移除、创建者转让；复用现有群成员与群会话。
- 问题/笔记/资料/公告帖子、动态筛选、时间游标翻页、编辑版本冲突、置顶/精选/隐藏/恢复/删除；评论和一层回复；幂等点赞/取消。
- 复用原有实时群聊、IndexedDB 消息保存、ACK、历史读取和更早消息翻页；聊天室、帖子与任务均可引用附件。
- 复用完整学习任务：草稿、发布、提交、退回理由、修改重交、审核通过和进度。
- 持久通知中心、已读状态、WebSocket 更新提示；评论/帖子事件经 Outbox→Kafka→MySQL 收件箱；原任务事件以稳定 ID 投影，不重复执行任务动作。
- 多 Gateway 通过 S3 API 访问 MinIO；保留旧本地 storage_key 下载兼容。上传大小/类型校验、SHA-256、同社区归属校验、成员授权下载。
- Prometheus/Grafana 指标、Alloy→Loki 集中日志、Alertmanager→本地鉴权接收端的告警触发和恢复。

## 代码与表

| 模块 | 实现 |
|---|---|
| 领域逻辑 | `internal/community/{model,service,worker}.go`：事务、请求幂等、成员权限、帖子与通知 |
| HTTP 入口 | `cmd/gateway/internal/handler/communityroutes.go`，独立于原生成路由；`main.go` 注册 |
| 任务事件接入 | Logic 内随进程启动两个可取消 goroutine，分别发布社区 Outbox、消费社区事件；原三个 worker 保留 |
| 推送边界 | `websockethandler.go` 的 `AuthorizePayload`；`internal/server/manager.go` 支持通知更新提示和写前权限检查 |
| 文件字节 | `internal/attachment/object_store.go`，官方 MinIO Go SDK 的 S3 读写适配；`store.go` 保留旧键兼容 |
| 前端 | `web/src/main.tsx`、`wire.ts`、`style.css`，React/TypeScript；`web/Dockerfile` 构建后交 Nginx |
| 新增表 | `community_profiles/posts/comments/reactions/post_attachments/requests/event_outbox/notifications`，见增量迁移 |
| 部署观测 | `docker-compose.community.yml`、observability overlay、`deploy/community`、`deploy/observability` |
| 自动化 | `internal/community/service_test.go`，`scripts/verify_communities*.py/cjs`；CI 增加新前端构建与 Compose 校验 |

**必要的设计修正：**当前社区 HTTP 适配层调用同进程 `internal/community` 领域服务并访问 MySQL，没有新建 Community RPC；原 IM Logic RPC 分工保留。社区事件在 Logic 后台处理。该做法延续现有任务 API 接入方式，少改消息协议；不能宣称 Gateway 已经完全与业务数据库解耦。新社区 ID 使用 c- 前缀，旧历史查询原先只识别 G/group:；本轮补齐兼容，并让新前端显式传 group: 会话目标，修复历史查询误当单聊的问题。

原成员退出需求还揭示了消息隐私风险：Kafka 收件人是生成任务时的快照。新代码在最终写 WebSocket 前复核群成员资格，覆盖旧 Kafka 任务、Redis pending 重试和重连回放；退出某个社区不会删除用户整个在线路由。登录会话摘要也按当前 SQL 成员过滤，避免旧会话 Outbox 把已退出社区的摘要重新写回缓存而泄露内容。已经下载到用户设备的内容无法通过退出操作撤回。

## 测试结果

1. `go test ./...`：通过。
2. `go test -race ./internal/community ./internal/attachment ./internal/server ./internal/learningtask ./cmd/gateway/internal/handler`：通过。
3. `npm run build --prefix web`：TypeScript 检查及 Vite 构建通过；Docker 也独立构建产品前端。
4. `python3 scripts/verify_communities.py --faults`：真实 MySQL/Redis/Kafka 环境，注册、多社区隔离、重试请求、并发编辑、回复层级、点赞、分页、任务状态、通知已读、创建者转让、退出权限均通过；Kafka 中断时帖子仍可查询，恢复后通知补出。
5. `node scripts/verify_communities_browser.cjs`：Chromium 两个独立浏览器上下文完成注册、入群、S3 附件上传下载、发帖、评论、精选、真实 WS 群聊、退回重交和审核通过、重新登录查状态、站内通知；没有 pageerror。脚本还暂停 Transfer，在消息已入库/收件人已形成后让成员离群，恢复消费后确认旧成员未收到该消息。
6. `python3 scripts/verify_community_observability.py`：暂停本演示 Transfer，Prometheus 规则实际触发，Alertmanager 发到本地受鉴权保护的接收端；恢复后收到 resolved；Loki 查到告警审计日志。没有向真实人员发送消息。
7. 原 `verify_learning_tasks.py` 回归通过，包含消息历史、幂等提交、并发审核、过期请求重放、管理员角色、移除成员和进度分母验证。

本机 Playwright 缺少几个动态库。本轮用下载的 Ubuntu deb 解包到 `/tmp/linkgo-browser-libs/root`，设置 `LD_LIBRARY_PATH=/tmp/linkgo-browser-libs/root/usr/lib/x86_64-linux-gnu` 后运行 Chromium；没有将缺依赖当作业务失败或伪报浏览器测试通过。一般环境可按 Playwright 提示安装系统依赖。

操作截图与本轮专用账号在 `artifacts/community-feed.png`、`community-notifications.png`、`community-browser-demo.json`。测试只创建自己的账号/社区；演练没有删除真实业务数据。测试结果不包含新版社区业务容量成绩，旧 10000 连接、30000 条单聊数据没有挪作社区性能结果。

## 启动和演示

```bash
cd /home/xiaobin/myproject/linkgo-collab-next
bash scripts/start_community.sh
python3 scripts/seed_community_demo.py
```

脚本生成本地 TLS 证书、对现有数据库执行增量建表，并以 Docker 构建和启动 Go 服务、React/Nginx、对象存储、指标、日志及告警组件。它不执行 down 或删除数据库卷。已有数据库建议先做备份；本地镜像源配置在 `.env.docker-cn`，不含模型密钥。MinIO 本轮因原镜像不可获取，使用固定摘要的 Coollabsio MinIO 归档镜像；它是本地兼容性验证依赖，[镜像来源](https://github.com/coollabsio/minio/pkgs/container/minio/)，不描述为已完成生产选型评估。可通过 `MINIO_IMAGE` 替换。

打开 `https://localhost:8443/community/`，本地自签名证书首次需浏览器信任。

| 账号 | 密码 | 初始身份 |
|---|---|---|
| `demo_study_admin` | `Study-Demo-2026!` | Go 后端学习营、AI 论文读书会创建者 |
| `demo_study_member` | `Study-Demo-2026!` | Go 后端学习营成员 |
| `demo_study_guest` | `Study-Demo-2026!` | AI 论文读书会成员 |

1. 普通窗口登录 admin，隐私窗口登录 member，各自选择 Go 社区。
2. admin 发布笔记，可上传 TXT/PDF 等允许类型的附件；member 刷新动态、打开帖子、评论、点赞、下载文件。
3. 两边进入聊天；一边发送文字或附件，另一边查看实时消息。切走再切回可以查历史，有更多记录时点“加载更早消息”。
4. admin 进入任务，可以用初始“用自己的话解释 Outbox”任务，或创建新草稿再发布。member 打开任务提交，admin 刷新任务、填写理由退回。
5. member 刷新查看理由、修改重交，admin 刷新后通过，看完成进度。双方通知中心可看到相关更新，点击后重新读取业务状态。
6. member 退出登录再进入，任务通过状态仍在；guest 只能访问自己加入的社区。发现页可以申请直接加入当前可发现社区。
7. admin 转让创建者以后才能退出；成员退出后访问帖子、任务、文件都会失去权限。
8. Grafana 在 `http://localhost:3000`，`admin / linkgo`；Explore 选择 Prometheus 看指标或 Loki 查 `{project="linkgo",service="gateway-a"}`，用 request_id/event_id 继续筛选。Prometheus 为 9090，Alertmanager 为 9093。

验证命令：

```bash
python3 scripts/verify_communities.py --faults
node scripts/verify_communities_browser.cjs
python3 scripts/verify_community_observability.py
```

后两项演练会暂停本项目的 Kafka 或 Transfer，恢复写在 finally；应对本地演示环境运行。若用户中途强制杀脚本，可用 `docker start linkgo-next-kafka`、`docker unpause linkgo-next-transfer` 恢复。

## 当前限制和下一步

- 本地 MySQL、Kafka、Redis、MinIO 单节点，不宣称生产 HA；默认密码和自签名证书用于本机。
- 评论页面/接口当前最多返回 200 条，社区目录最多 100 个；大规模评论及社区目录分页还需补充。通知前端最多拉取二十页并展示最近 200 条，属于当前产品边界。
- 普通用户退出登录清除页面凭证；完整刷新 Token、撤销与设备会话管理没有在本轮扩展。
- 旧群聊扩散串行，未给大群扇出新容量成绩；限流为每 Gateway 实例，未包装成全局用户额度。
- 发帖、任务、通知是持久事实，文件对象与 MySQL 不是一个分布式事务；已处理数据库登记失败时的对象回收，崩溃窗口的周期孤儿扫描仍待补充。
- 当前有结构化日志、基础 HTTP/RPC trace 字段和事件 ID 关联，没有独立部署跨 HTTP/Kafka/DB 的完整分布式 Trace 存储。
- 后续 IM-P2 可以围绕评论分页、资料检索、用户通知偏好、对象生命周期和真实混合负载容量实验展开。
