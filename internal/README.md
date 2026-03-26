# Internal

`internal/` 存放项目核心业务实现，不对外暴露。这里的代码决定系统是否真的具备“分布式 IM”能力。

## 子目录

- `logic/`：消息归一化、序列号分配、消息投递、历史查询。
- `delivery/`：Redis 在线投递、离线补偿、ACK 跟踪。
- `discovery/`：Etcd 注册发现与一致性哈希选址。
- `middleware/`：JWT 鉴权与请求上下文注入。
- `server/`：连接管理、Redis 订阅和离线消息同步。

## 设计原则

- `internal` 层是项目真正的业务核心。
- `cmd` 只负责装配，`internal` 负责实现。
- 这里的结构也是面试里解释项目分层的重点。
