# LinkGo 本地演示手册

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
make docker-up
docker compose ps
```

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

页面是原生 HTML/CSS/JavaScript 调试客户端，不是生产级 App。Token 只保存在当前页面内存，刷新后需要重新登录。

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

- 红包：创建、领取、重复领取、详情查询。它是等额账本，不包含钱包扣款和退款。
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
