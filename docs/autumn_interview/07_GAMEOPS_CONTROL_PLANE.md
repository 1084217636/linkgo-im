# 07 游戏运营控制面

## 为什么做这个功能

普通 IM 只能证明实时通信。游戏岗位还关心活动配置、审批、道具发放、审计、灰度和回滚，因此项目增加一条完整纵向链路，而不是堆商城、充值等大量浅功能。

## 角色

- operator：创建草稿、提交审批、批量发放。
- reviewer：审核活动；非管理员不能审核自己创建的版本。
- admin：发布和回滚，可执行高风险操作。

平台角色与群聊 owner/admin 完全分离。

## 活动状态机

```text
draft -> pending -> approved -> published
                              -> superseded（新版本发布后）
published -> rolled_back（恢复历史版本时）
```

每次修改创建新版本，不覆盖历史配置。活动主表保存 current/published 指针，版本表保存完整 JSON 和创建/审核信息。

## 发布链路

```text
operator 创建并提交
-> reviewer 审核，写 approved_by
-> admin 发布
-> DB 事务更新版本/主表
-> 同一事务写 audit + outbox
-> COMMIT
-> Outbox 更新 Redis 生效配置
```

Redis 失败时数据库状态和 Outbox 已提交，API 返回 `202 cache sync pending`，后台周期重放。不能返回普通 500 诱导调用方重复发布。

## 灰度发布

`rollout_percent` 限制 0–100。生效配置写入 Redis，业务方可根据稳定用户哈希判断是否落入灰度范围。当前项目完成配置控制面，不夸大为完整游戏客户端活动系统。

## 回滚

admin 必须指定 `target_version`，目标必须是 superseded 历史版本。事务把当前线上版本改为 rolled_back，把目标恢复为 published，审计记录 from/to，并通过 Outbox 恢复 Redis 完整配置。

## 道具批量发放

请求包含 `grant_request_id` 和多条 user/item/quantity，最多 1000 条。事务写请求、明细、玩家库存和审计。

幂等键：`grant_request_id + user_id + item_id`。同一请求重放返回历史结果，不重复增加库存；任一 SQL 失败整批回滚。

## 审计

记录操作者、角色、operation、resource、request_id、result、detail、trace_id、IP 和时间。查询最多 200 条，避免管理查询拖垮主库。

审计作用：追责、问题定位、合规证据；不能只依赖普通应用日志。

## 为什么用 Outbox

本地事务不能原子提交 MySQL 和 Redis。业务状态与 Outbox 同事务，保证“只要业务提交，就一定留下待同步事件”；后台重试实现最终一致。

## 闭卷题

1. 为什么 IM 项目增加游戏运营链路？
2. 三种角色如何分工？
3. 为什么版本不可覆盖？
4. 为什么发布需要 Outbox？
5. Redis 失败为什么返回 202？
6. 道具发放幂等键是什么？
7. 整批中一条失败怎么办？
8. 回滚如何恢复历史配置？
9. 审计和普通日志有什么不同？
