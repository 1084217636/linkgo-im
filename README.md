> **2026-11-02 最新多社区版已开发**：注册入群、帖子互动、社区任务、持久通知、S3 附件及日志告警的启动与实测结果见 [本轮实施验收](docs/evidence/community-20261102.md)。下文旧版“仅学习群”“附件只有共享卷”等描述是先前阶段记录，以最新验收为准。

按《肖宾_西电_AI软件开发工程师》当前简历练习项目介绍，请从 [2026-11-03 简历项目讲解](docs/interview/20261103_RESUME_PROJECT_WALKTHROUGH.md) 开始；旧 [IM 讲稿](docs/interview/LINKGO_IM_30_MINUTE_TALK.md) 保留早期阶段的实现说明，项目定位和测试口径以新版讲解及 [验收记录](docs/evidence/resume-verification-20261102.md) 为准。多社区产品页面位于 `web/`，旧的 `public/` 原生页面仍用于协议调试，两者不是同一套前端。

# LinkGo Chat

LinkGo 是一个使用 Go 和 go-zero 实现的 Client-Server 分布式 IM 项目。客户端通过 HTTP 与 WebSocket 接入，服务端按 Gateway 连接层、Logic 业务层和 Transfer 异步投递层拆分，主线覆盖跨 Gateway 单聊、消息持久化、ACK/重连补偿和 Kafka 群聊扩散。钱包红包与 AI 虚拟好友是可选扩展示例，不进入简历主线。

## 第一次看这个项目

如果你只会 Go 基本语法，请不要从架构图或面试题开始。

学习本项目时只看 [LinkGo 分布式 IM 学习手册](docs/handbook/README.md)，从第 00 章开始按编号顺序读到第 20 章。最后一章复盘 AI 辅助开发中的真实失败、调试方法、工作差异和面试口径；钱包红包与 AI Bot 放在附录。

完整文档分类见 [docs/README.md](docs/README.md)。

学习群任务协作、群附件、MySQL 表设计、3 分钟项目讲稿与本轮验证边界见 [项目升级讲解](docs/interview/UPGRADE_INTERVIEW_GUIDE.md)。

## 客户端

多社区的产品演示页面在 `web/`，覆盖注册、帖子、通知、附件、任务和聊天；`public/index.html` 是保留的零构建原生 HTML/CSS/JavaScript 协议调试客户端。下列能力描述的是 `public/` 调试页：

- 登录、好友、群组和会话列表。
- 单聊、群聊、历史查询。
- 心跳、ACK、`last_seq` 短期补偿和过载退避重试。
- 使用 IndexedDB 按用户保存会话、消息、发送状态和游标；收到非当前会话的消息也先保存再 ACK。登录和 WebSocket 重连后，页面按已知会话逐个查询 MySQL 历史页，把本地上次完成同步后的新增范围写回 IndexedDB；“补齐历史”可手动重试。服务端暂时不可用时可查看本地历史。
- 可选扩展：模拟充值、站内钱包红包与 AI 虚拟好友入口。

它用于验证后端链路，不定位为商业级前端；Token 只保存在页面内存而不写入 IndexedDB，刷新后需要重新登录。自动重连只重试页面配置的 Gateway 地址；跨 Gateway 故障切换需要稳定的负载均衡入口。首次本地同步以最新 100 条为基线，旧历史可手动翻页；当前不是完整多设备、连续缺口保证的同步协议。学习群任务的浏览器闭环验收见 [IM-P1 记录](docs/evidence/im-p1-evidence.md)。

## 架构

```text
Client A -> LB -> Gateway-1 -> Etcd/p2c_ewma -> Logic-2
                         |                         |-> MySQL
                         |                         |-> Redis
Client B -> LB -> Gateway-3 <- Redis Pub/Sub <-----'

群聊：Logic -> Kafka -> Transfer -> Redis -> target Gateway
```

### Gateway

- 提供 HTTP 和 `/ws` WebSocket 入口。
- 校验 JWT、Origin，维护本机连接、心跳、ACK 和 UID 分片有界推送队列。
- `/healthz` 只表示进程存活，`/readyz` 检查 Logic gRPC、Redis、MySQL；Logic 的 gRPC Health Check 还会检查 Redis、MySQL、Kafka，避免依赖故障时继续接收新流量。
- 通过 Etcd 发现 Logic 实例，使用 go-zero zRPC `p2c_ewma`。
- 当前好友、群、红包和部分 AI HTTP 接口也在 Gateway 进程直接访问 MySQL，因此不能笼统描述成“Gateway 完全不碰数据库”。

### Logic

- 处理登录、历史查询和 WebSocket 上行消息。
- 生成 `message_id`、稳定会话 ID 和会话 `seq`。
- 使用 Redis 与 MySQL 双层幂等，同步写入消息历史后再投递。
- 单聊进入 RedisDelivery；群聊先解析收件人并写 Kafka 任务。

### Transfer

- 消费已经携带收件人列表的 Kafka 群聊任务。
- 逐收件人写入投递状态并通知目标 Gateway。
- 使用 `processing(owner, lease) -> done` 做成员级幂等，并处理 retry、DLQ 和手动位点提交。

## 数据组件

- MySQL：消息最终历史、用户/好友/群/会话关系、钱包账户/资金流水/红包和 AI 记录。
- Redis：在线路由、会话 seq、短期 pending/ACK payload/timeline 和热点索引。
- Kafka：群聊逐成员投递任务、重试和死信。
- Etcd：Logic 服务注册与发现。

Redis Pub/Sub 只负责在线实时通知，不是可靠消息存储。当前 WebSocket 重连自动回放 Redis `pending_ack + ack_idx`，并可按一个 `session_id + last_seq` 从 Redis timeline 补最多 200 条；MySQL 历史通过独立 HTTP 接口查询，当前不会在重连时自动扫描所有会话完整补齐。

## 快速验证

### 只运行代码检查

```bash
make test
make build
make frontend-static-check
make compose-config
make cluster-smoke
make k8s-check
make fault-check
```

### 启动本地完整环境

```bash
make docker-up
```

推荐从本机 HTTPS/WSS 入口启动网页（首次会生成 30 天自签名证书，仅用于本机验证）：

```bash
./scripts/start_secure_demo.sh
```

打开 <https://127.0.0.1:8443/>。浏览器会提示自签名证书未受信任；确认是本机生成的证书后可在本机演示中继续。正式部署必须换用受信任证书和 TLS 入口，不能把这个自签名入口当生产配置。安全页面同源代理 API 和 WebSocket：登录走 HTTPS，连接走 WSS，浏览器先通过带 JWT 的 HTTPS 请求领取 30 秒、一次性 WebSocket ticket，URL 不再放长期 JWT。Gateway 在当前 Compose 中只映射到 `127.0.0.1`；旧 JWT 查询参数仍供本机协议压测兼容，安全入口拒绝它。

完整社区演示使用 `make community-up`，然后在浏览器打开 <https://localhost:8443/community/>。如果 Windows 浏览器访问 WSL 内的 `localhost` 超时，可在仓库目录执行 `touch .local-tls/windows-host-proxy.enabled`，再运行 `make community-up`。该本机标记让 Docker 页面绑定到 WSL `127.0.0.1:8444`，并启动仅监听 Windows `127.0.0.1:8443` 的开发桥接程序；浏览器地址保持不变。`make community-verify` 会自动选择 WSL 实际端口。桥接程序只转发加密的 TCP 字节，TLS 仍由容器中的 Nginx 终止。此配置仅用于 WSL 镜像网络无法转发本机端口的开发机。

旧的本机 HTTP 调试方式仍可使用：

```bash
python3 -m http.server 8088 --directory public
```

打开 <http://127.0.0.1:8088>。它不加密，只用于本机调试。测试账号：

```text
userA / 123456 -> 1001
userB / 123456 -> 1002
userC / 123456 -> 1003
```

本地默认端口：

```text
Gateway A/B/C  8090/8091/8092
Logic gRPC     容器网络内 9001（默认启动 2 个实例）
Logic health   9002（仅第一个实例映射到宿主机）
Redis          6379
MySQL          3306
Kafka          9092
Etcd           2379
```

详细操作见 [本地演示手册](docs/runbooks/LOCAL_DEMO.md) 和 [DevOps 参考](docs/runbooks/DEVOPS.md)。
故障停止、替换、扩容和恢复演练见 [故障先行手册](docs/runbooks/FAULT_HANDLING.md)。
面试准备见 [LinkGo IM 45 分钟逐字讲稿](docs/interview/LINKGO_IM_30_MINUTE_TALK.md)。

## 目录

```text
api/             Protobuf 和 gRPC 协议
cmd/gateway/     HTTP/WebSocket 接入服务
cmd/logic/       核心消息 gRPC 服务
cmd/transfer/    Kafka 群聊消费者
internal/        消息、投递、AI、鉴权和指标实现
public/          浏览器调试客户端
sql/             MySQL 初始化和迁移
deploy/          Kubernetes 与监控配置
docs/handbook/   唯一学习主线
docs/reference/  代码参考
docs/runbooks/   操作手册
docs/evidence/   验证记录
docs/knowledge/  AI 问答运行时语料
```

## 当前边界

- 消息正文、会话摘要事件和外部投递事件在同一 MySQL 事务提交；后台 worker 以租约抢占并重试 Redis/Kafka 投递，超过 10 次进入 dead，仍需告警与人工回放。
- Gateway 的 route 和本机连接均为单值，当前不是完整的同账号多端在线模型。
- 浏览器 IndexedDB 保存单端消息和独立的历史同步进度；登录或重连时通过现有历史接口逐个检查已知会话。仍未实现服务端驱动的多设备同步、离线发送自动重放、任意乱序提交下的严格连续缺口保证和本地全文检索。
- `ack_idx` 仍按接收用户保存完整编码 payload，尚未完全优化为纯消息 ID 引用。
- `offline_msg` 当前是标记索引，重连实际读取 `pending_ack`；单次 WebSocket 会话游标回放仍只处理一个会话，浏览器另用 HTTP 对已知会话逐个分页补历史。未知的新会话与多设备同步尚无完整自动发现协议。
- Logic 仍同步解析群成员后才写 Kafka，所以群消息发送成本并非完全与群规模无关。
- 红包已接入最小站内钱包：模拟微信/支付宝充值入账，发红包扣款，领取入账，过期余额按请求退款并记录流水；没有真实支付回调、自动过期扫描、提现、对账、风控或支付合规能力。
- AI 默认使用 mock；FAQ 是轻量文本召回，AI 回复任务是非持久 goroutine，不是可靠任务队列。
- `deploy/k8s/production` 是应用工作负载的可渲染示例，没有 Ingress/TLS、前端部署或真实生产集群证据。
- 当前应用使用 Redis 稳定单入口和 MySQL 单 DSN，不原生实现 Redis Cluster/Sentinel 客户端或 MySQL 应用层读写分离。
- 完整 Compose 故障演练已验证单实例 Redis、MySQL、Kafka、Logic、Transfer 故障和 Gateway 替换；这不等于已经验证 Redis Cluster、MySQL 主从或 Kafka 多副本 HA。

以上边界是面试时必须主动说明的当前事实；演进方案不能写成已经上线。
