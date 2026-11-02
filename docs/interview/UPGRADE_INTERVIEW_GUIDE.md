> **2026-11-02 最新多社区版已开发**：注册入群、帖子互动、社区任务、持久通知、S3 附件及日志告警的启动与实测结果见 [本轮实施验收](../evidence/community-20261102.md)。下文旧版“仅学习群”“附件只有共享卷”等描述是先前阶段记录，以最新验收为准。

# LinkGo 面试讲解与实现边界

## 项目定位

LinkGo 从“分布式消息链路练习”扩展为“学习群协作”：群里既能讨论和传资料，也能发布任务、提交作业、审核和看进度。底层仍复用 Gateway、Logic、Transfer、MySQL、Redis、Kafka 与 WebSocket。本轮真实新增的是学习任务闭环和群附件；不是完整微信替代品。

## 3 分钟项目介绍

> 我做的是一个面向学习小组的协作 IM。最初的核心是多 Gateway 下的实时消息和断线补偿；为了让消息系统承载实际业务，我加了群任务协作：群管理员发布任务，成员提交文字、链接或附件，管理员可以退回要求修改，也可以通过，页面能看到成员完成进度。任务状态变化会通过现有 IM 消息通知双方。
>
> 系统分为 Gateway、Logic 和 Transfer。Gateway 维护 WebSocket 连接并处理客户端 HTTP/WebSocket 请求；Logic 做用户和群成员权限、消息/任务状态校验及 MySQL 事务；单聊沿 Redis 路由和 Pub/Sub 投递，群聊由 Logic 把扩散任务写入 Kafka，再由 Transfer 消费并逐成员投递。客户端的业务 ACK、Redis 短期待确认状态和 MySQL 消息事实各有职责，不能把 Kafka offset 当成客户端已收到。
>
> 任务数据放 MySQL。创建、提交、审核时先锁定任务或提交记录，在同一事务里校验角色和版本、更新当前状态、写事件和通知 Outbox。前端每次操作带 request_id，重复请求会回放已保存结果；同一 request_id 换了内容会冲突。并发审核通过 expected_version 和行锁保证只有一个更新成功。通知失败由后台 worker 重试，不会回滚已经提交的任务事实。
>
> 群附件与消息正文分开。上传接口先检查当前群成员，限制大小和允许类型，生成随机附件 ID、算 SHA-256，将文件放在 Docker 共享卷，MySQL 记录群、上传者、文件名、MIME、大小、摘要、存储键和状态。消息只存附件 ID 与展示元数据；服务端发送时重新从数据库读取规范元数据，下载时再次检查当前群成员。文件字节目前是本地共享卷，不是云对象存储。任务提交可以引用同群 ready 附件。
>
> 我验证了 Go 全量单测、前端脚本语法以及 Compose 配置。这个环境没能拉取 MySQL/Kafka 镜像，所以本轮没有声称 Docker 端到端流程已经实跑；可运行脚本和浏览器验收步骤留在仓库中。下一步如果进入真实部署，我会把文件层换为 S3/MinIO 类对象存储、加入分片上传和孤儿文件清理，并按消息/附件/任务分别做负载测试。

## 一条任务的完整故事

1. 管理员在群页创建草稿并发布。后端查 `group_members` 确认 `owner/admin`，创建 `group_tasks` 记录；发布更新状态与版本，并在事务中写 `task_events` 与 `task_notification_outbox`。
2. 成员打开任务列表。Logic 只返回当前仍是群成员可见的任务。通知消息是一个带 task ID 的提示，不重复塞入任务正文；页面收到后可重新请求最新任务状态。
3. 成员提交文本/链接/附件。后端锁任务、检查任务处于 published、检查用户仍是 active member、验证附件属于同群且 ready；按 `(task_id,user_id)` 仅保留一份当前提交，退回后再交会增加 `attempt_no` 和 `version`。
4. 管理员审核时提交 expected version。SQL 行锁串行化竞态；一次成功后另一个旧版本审核得到冲突，不能重复通过或覆盖新提交。
5. 状态更新和待通知事件同事务提交。后台通知 worker 扫描到期 pending 行，写入现有 IM 消息；失败退避重试。超过策略上限会进入 dead 状态，保留错误信息供排查。
6. 页面刷新/重登后从 HTTP API 读取任务和提交状态；实时通知用于提示刷新，不是状态事实本身。

## MySQL 主要表怎么讲

| 表 | 保存什么 | 面试时抓住的字段 |
| --- | --- | --- |
| `users` | 用户和密码哈希 | `user_id`,`username`,`password`；密码是 bcrypt 哈希，不保存明文 |
| `im_groups`,`group_members` | 群、成员、角色和成员状态 | `group_id`,`user_id`,`role`,`status`；服务端每个受限操作都重新校验 |
| `messages` | 持久消息事实 | `message_id`,`client_msg_id`,`session_id`,`seq`,`from_uid`,`to_id`,`content`,`msg_type` |
| `conversations`,`conversation_members` | 会话水位和成员状态 | `last_seq`,`acked_seq`,`read_seq`；ACK 和已读概念分开 |
| `delivery_outbox` | 消息写库后必须继续处理的投递待办 | `message_id`,`status`,`attempts`,`available_at`,`last_error`；消息与 Outbox 在同一事务 |
| `group_tasks`,`task_submissions` | 任务当前状态和每人当前提交 | 任务 `status/version`; 提交 `status/attempt_no/version/attachment_ids` |
| `task_events`,`task_notification_outbox` | 操作审计/幂等结果与后续通知 | `(actor_uid,request_id)` 唯一；通知按 `event_id,target` 去重和重试 |
| `attachments` | 附件元数据及访问控制所需归属 | `attachment_id,group_id,uploader_id,file_name,mime_type,file_size,sha256,storage_key,status`；字节在共享卷 |

一条消息示例：`messages(message_id='group:G7-42', client_msg_id='cli-9c…', session_id='group:G7', seq=42, from_uid='U1', to_id='G7', to_type='group', content='{"kind":"attachment","attachment_id":"…"}', msg_type=2)`。重试用同一个 client_msg_id；唯一索引阻止相同发送被重复入库。会话 seq 用于排序，不是 message_id，也不代表 ACK。

任务数据示例：`group_tasks(task_id='T9',group_id='G7',status='published',version=2,created_by='U1')`；`task_submissions(task_id='T9',user_id='U2',content_text='完成前三题',attachment_ids='["a1"]',status='submitted',attempt_no=1,version=1)`。`task_events` 留每次操作的 request_id 与结果快照；通知待办完成后才标 done。

## 选择这些设计的理由

- **MySQL 做业务事实**：任务、成员、提交和消息都需要事务、唯一约束及可查询状态；不用 Redis 存长期业务事实。
- **Outbox 做提交后续工作**：MySQL 和 Kafka/Redis 不能放进一个普通本地事务里。先在一个 DB 事务写业务事实和待办，再异步投递；worker 重试依靠幂等键收敛。
- **附件元数据和文件字节分离**：消息适合存小型引用，文件走上传/下载 HTTP；避免把文件塞进 WebSocket 和消息行。共享卷适合本地演示，不适合跨机房生产。
- **通知发 task ID**：通知丢失或重复时，页面仍可从业务 API 读取真实状态；不把消息投递成功误当审核状态成功。

## 启动和演示

在仓库根目录：

```bash
docker compose --env-file .env.docker-cn up -d --build
curl http://127.0.0.1:8090/healthz
python3 -m http.server 8088 --directory public
```

新副本容器名带 `linkgo-next-` 前缀，MySQL 容器为 `linkgo-next-mysql`。创建/登录两个账号；建立群后，需在隔离的演示数据库把一个群成员角色改成 admin（不要对真实数据执行示例更新）。浏览器打开 `http://127.0.0.1:8088`，管理员发布并发布任务，成员提交文字或上传文件，管理员退回后成员重交，最后审核通过并查看进度。自动 API 验收：`python3 scripts/verify_learning_tasks.py`；浏览器脚本见 `scripts/verify_learning_tasks_browser.cjs`，需按仓库说明安装 Playwright。

本次提交后的检查：`go test ./...`、`node --check`、Docker Compose 配置渲染通过；Prometheus Compose 配置也可渲染。学习任务 API 与浏览器闭环、通知恢复故障演练此前有 2026-10-28 的真实 Docker/浏览器验收记录，详见 [`docs/evidence/im-p1-evidence.md`](../evidence/im-p1-evidence.md)。本轮新加的附件上传/下载页面链路尚未重新做整栈浏览器验收；当前 registry 拉取超时，因此不把附件端到端验收说成已通过。

## 仍需明确的范围

- 本轮没有实现群内全文搜索、音视频通话、端到端加密、真正的云对象存储、断点续传或自动孤儿附件清理。
- 单机 MySQL/Redis/Kafka 的既有压力结果不代表附件上传能力；不要把纯文字压测说成富媒体容量测试。
- 当前环境重新拉取镜像超时，附件加入后的全栈浏览器验收尚待重跑；不能据任务历史闭环验收推断附件端到端也已验收。
