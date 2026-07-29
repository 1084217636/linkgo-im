# Discovery

`internal/discovery` 包含早期手写注册发现代码和当前仍在复用的地址解析工具。先区分“文件存在”和“主链路正在调用”。

## 当前实现

- 当前 Logic 的 Etcd 注册和 Gateway 的服务发现都由 go-zero zRPC 配置完成。
- Gateway 由 zRPC `p2c_ewma` 选择目标 Logic。
- 当前运行路径只直接复用本包的 `ParseEndpoints` 解析逗号分隔地址。
- `Registry`、`Resolver` 和 `rendezvousHash` 是早期遗留实现，主程序没有调用。

因此主链路没有实现按用户固定 Logic 的 Rendezvous/一致性哈希。`LogicRouterPool.GetClient` 的 `key` 参数保留了调用上下文，但不参与实例哈希。

## 后续可增强

- 如果业务将来确实需要会话粘性，再评估一致性哈希；不能把演进方案说成当前能力。
