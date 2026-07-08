# Public

`public/` 用于存放前端静态资源或本地联调页面。

当前目录只有一个单文件调试台 `index.html`，主要用于接口和 WebSocket 联调，不承载正式前端工程。

调试台会使用真实登录 token、REST 鉴权头，以及 `api.WireMessage` protobuf 二进制 WebSocket 协议，适合验证登录、好友、群组、历史消息、ACK、clientMsgId 幂等等链路。

当前调试台也提供红包测试入口，可以在选中会话后创建红包、抢红包和查看红包详情。红包金额使用“分”为单位，例如 `100` 表示 `1.00` 元。

当前版本会把默认的 `AI 助手` 作为好友展示出来，后续私聊问答和群聊总结都会沿着“AI 作为系统内虚拟用户”的方向继续收口。

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
