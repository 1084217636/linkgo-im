# LinkGo 本地演示手册

## HTTPS/WSS 登录演示（推荐）

启动原有 Docker 服务后，运行 `./scripts/start_secure_demo.sh`，打开 `https://127.0.0.1:8443/`。自签名证书只用于本机；浏览器提示证书未受信任时，确认来源是本机生成的 `.local-tls/cert.pem` 后再继续。页面登录 `userA / 123456`，开发者工具 Network 中应看到 `/api/v1/login` 与 `/api/v1/ws-ticket` 均为 HTTPS、`/ws?ticket=...` 为 WSS。ticket 只能使用一次且 30 秒过期。生产需受信任证书和相应的入口部署。

可运行 `node scripts/verify_secure_login.cjs` 做浏览器验证；本机自签名证书的自动化测试只在 Playwright 上下文启用证书例外。以下 HTTP/WS 步骤保留用于协议和故障调试，不应作为加密传输的验收。

> 这份文档只负责“怎样运行和验收”。如果还不知道 Gateway、Logic、Redis 是什么，先从 [`docs/handbook/00_START_HERE.md`](../handbook/00_START_HERE.md) 开始，不要靠执行命令代替理解。

## 1. 演示前检查

在仓库根目录执行：

```bash
make test
make build
make frontend-static-check
make docs-check
make compose-config
```

每条命令都成功后再启动环境。Docker Desktop 或 Docker Engine 必须已经运行。

## 2. 启动完整后端

```bash
make local-up
docker compose ps
```

`make local-up` 固定使用仓库默认端口。它会先清理本项目遗留容器；如果发现明确占用端口的 `comet-*` 容器，会停止冲突容器；遇到其他未知容器或本机进程时会显示占用信息并退出，不会自动杀死未知进程，也不会静默切换到另一组端口。

等待 Gateway、Logic、Transfer 和依赖服务进入健康状态。快速检查：

```bash
curl http://127.0.0.1:8090/healthz
curl http://127.0.0.1:8090/readyz
```

`healthz` 表示进程活着，`readyz` 表示关键依赖可用、当前适合接流量。

## 3. 启动浏览器客户端

另开一个终端：

```bash
python3 -m http.server 8088 --directory public
```

浏览器打开 <http://127.0.0.1:8088>。测试账号：

```text
userA / 123456
userB / 123456
userC / 123456
```

页面是原生 HTML/CSS/JavaScript 调试客户端，不是生产级 App。IndexedDB 按当前用户保存会话、消息、发送状态和 `last_seq/acked_seq`；Token 只保存在页面内存，刷新后仍需重新登录。

## 4. 按顺序手工验收

### 4.1 登录和建连

1. 标签页 A 登录 userA，Gateway 地址保留 `127.0.0.1:8090`。
2. 标签页 B 登录 userB，把 Gateway 改为 `127.0.0.1:8091`。
3. 确认两个页面的 WebSocket 都已连接。

这样能演示 A、B 连接不同 Gateway；它仍运行在一台电脑的多个容器中，不等于跨物理机生产部署。

### 4.2 好友和单聊

1. 如果账号尚未成为好友，由 A 发起好友申请，B 接受。
2. A 选择 B，发送一条带明显时间的文本。
3. 确认 B 实时收到，A/B 的页面日志出现正常消息或 ACK。
4. 查询历史，确认消息可从 MySQL 历史接口读到。
5. 点击“本地数据”查看 IndexedDB 中会话、消息、pending 和游标数量；刷新并重新登录后确认本地历史先恢复，再由服务端历史校准。

你要能边演示边说出：`Gateway-A → Logic → MySQL → Redis route/PubSub → Gateway-B → WebSocket → ACK`。

### 4.3 短期断线恢复

1. 让 B 断开 WebSocket，但不要删除数据库或 Redis。
2. A 再发送一条消息。
3. B 重新连接，观察尚未 ACK 的消息是否回放。

准确口径：自动回放读取 Redis `pending_ack + ack_idx`；`offline_msg` 是标记，不是正文来源。当前不会在每次重连时扫描 MySQL 所有会话完整补齐。

### 4.4 群聊和 Kafka

建立包含 A、B 的群后发送群消息。完整环境中链路是：

```text
Logic 解析收件人并写 Kafka
→ Transfer 消费
→ 逐成员 RedisDelivery
→ 目标 Gateway
```

Kafka/Transfer 失败路径、retry/DLQ 和手动提交需要结合日志或自动脚本验证，不能仅凭页面收到一条群消息就声称全部故障场景通过。

### 4.5 红包与 AI

- 钱包红包：先用模拟微信/支付宝充值，再创建红包、领取、重复领取、查询详情和触发过期退款；它不包含真实支付回调、自动退款任务、提现和对账。
- AI：点击 AI 助手发送私聊问题。默认 mock 可离线演示；FAQ 是轻量文本召回，AI 回复任务不是持久队列。

## 5. 自动演示脚本

让脚本自行启动所需栈：

```bash
START_STACK=1 make core-im-demo
START_STACK=1 make group-transfer-demo
START_STACK=1 make ai-demo
START_STACK=1 make ai-ask-demo
```

组合演示：

```bash
START_STACK=1 make im-ai-final-demo
```

脚本产物写入 `artifacts/`。每次用于简历或面试前，都应在当前 commit 重新运行；旧产物和历史文档不能替代本次结果。

默认完整 Compose 启动 3 个 Gateway 和 2 个 Logic。验证 Gateway 确实通过 Etcd 发现多个 Logic：

```bash
make cluster-smoke
```

脚本会检查两台 Logic 的 readiness、三台 Gateway 的 readiness，并要求 Etcd 的 `services/logic/` 前缀下至少存在两个租约注册键。它证明多实例注册与发现已工作，不证明生产环境自动扩容或中间件高可用。

## 6. 常见问题

### 页面提示无法连接 Gateway

先检查：

```bash
curl http://127.0.0.1:8090/healthz
docker compose ps
docker compose logs gateway-a logic
```

页面中的 Host 使用 `127.0.0.1` 或 `localhost`，不要填写本地文件路径。

### 登录成功但单聊被拒绝

当前 Logic 会校验好友关系。先确认好友申请已经接受，而不是绕过业务校验直接发送。

### 群聊不工作

确认启动的是完整 Compose，而不是不包含 Kafka/Transfer 的 light 环境，再查看：

```bash
docker compose logs logic transfer kafka
```

### 想接真实模型

复制 `.env.ai.example` 的变量到本机环境，配置兼容 provider 和 API Key 后重启。真实 Key 只放环境变量或 Secret，不要写进 YAML、Markdown、截图或 Git commit。

## 7. 停止环境

```bash
make docker-down
```

前台运行的 Python 静态服务器使用 `Ctrl+C` 停止。保留命令输出、commit SHA 和必要截图，记录“验证环境、验证步骤、结果、仍未覆盖的边界”。

## 10 月 4 日：三用户重连与历史查询实测

这一节使用预置 `userA/userB/userC`。演示工具通过真实登录和 WebSocket 发送创建 **A↔B、A↔C 两个会话**，每个会话本轮生成 3 条消息；前两条由临时客户端离线回放并 ACK，最后一条保留给 HTML 页面登录时观察。再次运行会追加一组有新时间标记的消息，不会清空旧消息。

### 每次启动

在项目根目录打开第一个终端：

```bash
docker compose --env-file .env.docker-cn -f docker-compose.yml up -d --no-build
curl http://127.0.0.1:8090/readyz
go run ./tools/reconnect_demo
```

先运行演示工具，再打开浏览器登录 B/C：工具会临时建立 B/C 的 WebSocket，若已经在页面登录，会替换页面连接。工具现在会等待服务端真正清理 Redis pending 后才报告 ACK 成功。

`readyz` 应输出 `ready`；如果刚启动时不就绪，检查 `docker compose --env-file .env.docker-cn -f docker-compose.yml ps` 与 Kafka/Logic 日志，等依赖可用后重试。2026-10-04 本轮 Kafka 首次因 ZooKeeper 临时节点尚未过期而退出，重新 `docker compose --env-file .env.docker-cn -f docker-compose.yml start kafka` 后恢复；另一次容器端口绑定暂时冲突，重试 `up -d --no-build` 后恢复。这些是本轮本机现象，不是每次启动都要执行的固定步骤。

第二个终端保持运行：

```bash
python3 -m http.server 8088 --directory public
```

打开 `http://127.0.0.1:8088/`。如浏览器已有旧 IndexedDB，可用新隐私窗口清楚观察本轮数据；不要用刷新页面代替 WebSocket 重连，因为 token 只在页面内存。

### 用网页看“登录后历史”

1. 登录 `userB / 123456`。Gateway 默认 `127.0.0.1:8090`。页面先恢复该用户 IndexedDB，会话列表从登录响应取得；登录响应包含会话摘要/游标，**不是所有历史正文**。
2. 页面调用 `connectWebSocket()`。刚登录时往往尚未选中会话，因此连接 URL 可能没有 `session_id`；Gateway 仍先回放 B 在 Redis 的短期未 ACK 消息。
3. 页面选中第一个会话并调用 `GET /api/v1/history?target_id=1001&limit=50`。后端从 MySQL 取最新一页，按 seq 正序返回并显示；本轮 A↔B 消息可搜索 `reconnect-demo-` 标记。历史页到来时可能把刚才在线回放的当前显示重新绘制为同一页历史，这是正常的页面级对账。
4. 用 `userC / 123456` 在另一标签页重复，打开与 userA 的会话，看到 A↔C 的消息。两人都应能看到本轮最后一条“待浏览器重连”消息。登录后服务端收到 ACK，会从短期 pending 中清掉对应 ID。
5. 不刷新页面，点击“重连 WS”或在开发者工具临时 Offline 再 Online，观察日志中的断开、重连和 ACK；页面保持当前会话，并在建连时上传该会话的 `lastAckedSeq`。短期 pending 只补未确认消息，已确认过的消息不应每次全量重复推送。
6. 在登录状态点击会话列表中的别的会话，会调用该会话的 HTTP 历史查询。在线消息只对当前选中的会话立即显示，但其他会话的正文也先写入 IndexedDB 再 ACK。登录或 WebSocket 重连后，网页会对已知会话逐个查询历史并补到本地；Gateway 本身的单次 WebSocket 游标回放仍只处理一个指定会话。页面的“补齐历史”按钮可手动重试，日志会输出每个会话的 `added`、`pages` 和 `synced_seq`。

### 为什么点“历史”没出现更早消息？

HTML 页的历史页大小为 `50`。当前 A↔B 只有少量演示消息，第一页已是全部数据，`has_more=false`，没有更早消息可加载。超过 50 条后，服务端返回 `next_before_seq`；页面滚动到消息区顶部，或点击“加载更早”，会带 `before_seq` 查询前一页，再将它插入当前列表上方并保持视口位置。后端分页已验证；这次前端上拉拼接改动尚未做浏览器 E2E 验收。

2026-10-04 已用 userB 的真实登录 token 对 A↔B 历史接口把 `limit` 临时设为 2 验证分页：

```text
before_seq=0 → seq [4,5], has_more=true, next_before_seq=4
before_seq=4 → seq [2,3], has_more=true, next_before_seq=2
before_seq=2 → seq [1], has_more=false
```

这证明了服务端按游标翻页；**没有把新增的前端上拉拼接交互说成已经实测**。每次重跑演示，seq 会继续增长，上面数字仅是本轮示例。

### 本轮后端证据与边界

最新复跑的 `reconnect-demo-20261004-164228` 在 MySQL 中有 A↔B seq 9、10、11，A↔C seq 7、8、9；临时 B/C 接收端回放并 ACK 前两条，并等到相应 Redis pending 真正清除。网页登录前，Redis pending 分别留下 B 的 `c2c:1001:1002-11` 和 C 的 `c2c:1001:1003-9`。演示工具把 HTTP 历史分页设为 2 条，实测 B 的第一页 seq `[10,11]`、第二页 `[8,9]`。`readyz` 与网页 HTTP 均返回 200。

工具验证的是 HTTP/WebSocket 服务端链路；**未自动操控浏览器验证 IndexedDB、渲染或按按钮的视觉结果**。浏览器代码的多页补偿算法另可运行 `node --test scripts/history-sync.test.cjs`。若运行环境在会话结束后停止容器，照上面的启动命令重启；MySQL 数据是否保留以本机 Docker volume 为准，缺演示消息就重新运行 `go run ./tools/reconnect_demo`。

### 手动验收网页的本地持久化与已知会话补偿

1. 正常浏览器窗口登录 `userB`，在开发者工具 **Application → IndexedDB → linkgo-client** 查看 `messages` 和 `sync_progress`。首次登录的历史同步日志会显示 `session_id`、`pages`、`synced_seq`；`sync_progress` 是已完成历史页的本地进度，不等于服务端 `acked_seq`。
2. 关闭 B 的网页，分别让 `userA` 和 `userC` 给 B 发消息。B 再用同一个浏览器登录；登录返回的已知会话会逐个触发 HTTP 历史查询，补到 IndexedDB。点击各会话可看最新 50 条；“补齐历史”按钮可重试并看日志。若 B 与 C 尚无好友关系，先在页面完成好友申请与接受；单聊权限由服务端校验。
3. 刷新 B 页面，再次登录并检查 IndexedDB 仍有该用户消息。Token 只在页面内存，刷新后重新登录是正常现象。关闭所有隐私窗口或清空网站数据会移除本地数据。
4. 保持 B 页面打开，在开发者工具 Network 暂时切 Offline 再切 Online，观察 `WebSocket 已断开`、退避重连和会话历史检查日志。它重试当前入口；本地直连 `8090` 并不会自动改连 `8091`。学习群业务浏览器自动化已运行；这一步的离线重连现象仍需按现场日志核对。
