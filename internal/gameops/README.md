# Game Operations Control Plane

`internal/gameops` 承载面向游戏运营场景的确定性控制面能力。所有写操作必须先经过 Gateway JWT 与平台角色校验，再在业务事务中写入 `operation_audit_logs`。

审核员和管理员可通过 `GET /api/v1/admin/audits` 按操作者、资源、结果筛选最近审计记录；单次最多返回 200 条，避免管理查询拖垮主库。指标只使用 operation/result 等有限标签，不把用户、活动或请求 ID 放入 Prometheus 标签。

当前角色边界：

- `operator`：创建和提交活动草稿、发起道具发放。
- `reviewer`：审批活动版本，不能审批自己创建的版本。
- `admin`：拥有运营控制面全部权限，并负责回滚。

`platform_user_roles` 与 IM 群角色相互独立，避免把群管理员误当成平台管理员。审计日志记录 operator、role、operation、resource、request/trace、result 和脱敏后的 detail，不记录密码、Token 或 Secret。

活动配置使用 `draft → pending → published → rolled_back` 状态机，每次修改创建不可覆盖的版本。reviewer 不能发布自己创建的版本；发布事务同时写审计和 `gameops_outbox`，事务提交后同步刷新 Redis，失败的 Outbox 由 Gateway 周期重放。灰度比例限制为 0–100，配置必须具有有效起止时间和正数奖励。

新版本通过锁定 `game_activities` 主行分配 `current_version + 1`，避免并发草稿拿到相同版本。数据库状态已提交但 Redis 暂时不可用时，API 返回 `202 Accepted` 和 `cache synchronization is pending`，不会用普通 500 诱导调用方重复执行发布。

道具发放以 `grant_request_id` 标识一次批量请求，并以数据库唯一键 `(grant_request_id, user_id, item_id)` 做最终幂等防线。同一请求重放只查询原结果，不再次增加 `player_items`；新请求在单个事务内写请求、明细、玩家余额和成功审计，任一步失败整体回滚，并额外写失败记录与失败审计。单批限制 1000 条且拒绝重复玩家/道具组合。
