# Public

## 本机安全入口

推荐在仓库根目录运行 `./scripts/start_secure_demo.sh`，打开 `https://127.0.0.1:8443/`。页面与 API 同源走 HTTPS，WebSocket 走 WSS；浏览器先领取一次性 ticket，不把长期 JWT 放到 WebSocket URL。证书是本机自签名演示证书，正式部署须换受信任证书。直接用 `python3 -m http.server 8088 --directory public` 仍是明文 HTTP 调试方式。

`public/` 用于存放前端静态资源或本地联调页面。

`index.html` 是用于 IM、红包、AI 和 WebSocket 联调的零构建页面。它是可运行的项目验收客户端，不定位为商业级视觉产品。

调试台会使用真实登录 token、REST 鉴权头，以及 `api.WireMessage` protobuf 二进制 WebSocket 协议，适合验证登录、好友、群组、历史消息、ACK、clientMsgId 幂等等链路。

`client-store.js` 使用浏览器 IndexedDB 按用户保存会话、消息、发送状态与游标。实时消息不论属于哪个会话，都会先写本地消息，再向服务端 ACK。`history-sync.js` 在登录、WebSocket 重连或点击“补齐历史”后，对已知会话逐个查 MySQL 历史：有上次完成的同步进度时，向前翻页直到与该进度重叠；本地消息和新进度在同一个 IndexedDB 事务提交。首次同步只把最新 100 条作为基线，旧历史继续用“历史”按钮翻页。服务端不可用时可以回退显示本地历史；JWT 不写入 IndexedDB，刷新后仍需重新登录。

本地数据库只承担客户端缓存、发送箱状态和验收证据，MySQL 仍是服务端消息事实来源。自动重连会重试页面填写的 Host/Port，跨 Gateway 故障切换需要该地址本身是稳定负载均衡入口。当前还不保证乱序提交后的所有低序号缺口都能被自动发现，也没有多设备同步协议或关闭页面后的离线发送队列。

当前调试台也提供红包测试入口，可以在选中会话后创建红包、抢红包和查看红包详情。红包金额使用“分”为单位，例如 `100` 表示 `1.00` 元。

学习群任务面板在打开群聊后出现：群主/管理员可新建和修改草稿、发布、审核、查看进度及关闭；普通成员可提交和在退回后重交。任务提醒是 IM 消息，点击会从任务 HTTP API 读取最新状态。完整 Docker 演示和测试结果见 [IM-P1 验收记录](../docs/evidence/im-p1-evidence.md)。

当前版本会把默认的 `AI 助手` 作为好友展示，并支持私聊问答入口。默认 provider 是 mock；FAQ 是轻量文本检索，AI 回复任务当前由 Logic 内非持久 goroutine 执行，不能描述为可靠 Agent 任务队列。群聊总结是独立 HTTP 功能，不等于群内 `@AI` 自动回复。

前端本地验收可以直接使用：

```bash
START_STACK=1 COMPOSE_FILE_PATH=docker-compose.light.yml FRONTEND_PORT=8088 make frontend-smoke
```

不启动后端时可先检查两个页面的关键控件与 API 契约：

```bash
make frontend-static-check
```

验收通过后访问 `http://127.0.0.1:8088/`，可以开两个浏览器标签页分别登录 `userA` 和 `userB` 做对聊；单个标签页登录 `userA` 后点击 `AI 助手`，即可测试用户与 AI 好友私聊。

如果要把 AI 好友从 mock provider 切到 DeepSeek，在本机 shell 设置环境变量后重启 light 栈：

```bash
export AI_PROVIDER=deepseek
export AI_MODEL=deepseek-v4-flash
export AI_BASE_URL=https://api.deepseek.com
export DEEPSEEK_API_KEY=你的真实 key
START_STACK=1 COMPOSE_FILE_PATH=docker-compose.light.yml FRONTEND_PORT=8088 make frontend-smoke
```

真实 key 不要写进仓库，可以参考根目录下的 `.env.ai.example`。

如果只是做页面联调，可以先启动轻量环境：

```bash
make docker-light-up
```

轻量环境不启动 Kafka/Transfer，适合先验证登录、好友、单聊、历史和幂等。

完整工程化环境可以使用：

```bash
make docker-cn-up
```

它会启动 Redis、MariaDB、Etcd、Zookeeper、Kafka、Logic、Transfer 和 3 个 Gateway。

如果需要边测功能边看指标，使用：

```bash
make observability-cn-up
make ops-smoke
```

Grafana 地址是 `http://127.0.0.1:3000`，账号 `admin`，密码 `linkgo`。

如果页面日志出现 `无法连接 Gateway` 或 `Failed to fetch`：

- 先确认后端已启动，`http://127.0.0.1:8090/healthz` 能访问。
- 从 Windows 打开 WSL 文件时，Host 不要填 `wsl$`，改成 `127.0.0.1` 或 `localhost`。
- 如果 Docker 拉镜像超时，需要先在 Docker Desktop/daemon 中配置可用镜像源，再执行启动命令。
