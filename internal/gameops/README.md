# Game Operations Control Plane

`internal/gameops` 承载面向游戏运营场景的确定性控制面能力。所有写操作必须先经过 Gateway JWT 与平台角色校验，再在业务事务中写入 `operation_audit_logs`。

当前角色边界：

- `operator`：创建和提交活动草稿、发起道具发放。
- `reviewer`：审批活动版本，不能审批自己创建的版本。
- `admin`：拥有运营控制面全部权限，并负责回滚。

`platform_user_roles` 与 IM 群角色相互独立，避免把群管理员误当成平台管理员。审计日志记录 operator、role、operation、resource、request/trace、result 和脱敏后的 detail，不记录密码、Token 或 Secret。
