# Discovery

`internal/discovery` 负责服务注册与发现。

## 当前实现

- Logic 启动时向 Etcd 注册自身地址。
- Gateway 通过 Etcd 查询 Logic 节点列表。
- 基于 Rendezvous Hash 按用户维度选择目标 Logic 节点，降低跨节点抖动。

## 后续可增强

- 本地 watch 缓存，减少高频查询 Etcd。
- 节点健康权重和故障摘除。
