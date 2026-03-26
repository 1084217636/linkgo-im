# 简历项目描述

## 版本一：浓缩版

分布式即时通讯系统，核心开发。基于 Go-Zero 构建 Gateway + Logic + Transfer 分层架构，Gateway 使用 go-zero REST 脚手架承接登录接口与 WebSocket 握手，Logic 使用 go-zero zRPC 脚手架承接内部 gRPC 服务；通过 Etcd 实现 Logic 服务注册发现，并结合一致性哈希完成用户维度的稳定路由；使用 Protobuf 二进制协议优化消息传输，使用 Redis Lua 生成会话级 Sequence ID，结合 ACK 未确认消息补偿解决弱网下乱序与漏发；引入 Kafka 异步扩散群聊消息并增加重试/死信处理，降低同步扇出压力；网关层接入 JWT 鉴权、令牌桶限流、双向心跳和 worker pool，提升长连接场景下的稳定性与安全性。

## 版本二：适合写到简历里的三段式

项目描述：
基于 Go-Zero 实现分布式即时通讯系统，采用 Gateway 接入层、Logic 业务层、Transfer 异步投递层分层设计，支持单聊、群聊、历史消息查询、离线消息补偿和 ACK 回执。

技术方案：
基于 go-zero REST + zRPC 脚手架解耦长连接接入与消息逻辑，通过 Etcd 做服务注册发现并结合一致性哈希实现跨节点精准路由；使用 Protobuf 二进制消息协议替代业务 JSON；利用 Redis 维护在线路由、Pub/Sub 投递、离线消息补偿与 ACK 未确认消息集合，使用 Lua 生成会话级递增 Sequence ID；引入 Kafka 异步处理群聊扩散并加入 retry / dead-letter 机制。

项目亮点：
设计 JWT 鉴权、令牌桶限流、双向心跳和 worker pool，提升高并发长连接稳定性；通过 Prometheus 指标暴露连接数、消息吞吐、ACK、Kafka 重试等运行状态，增强可观测性与问题定位效率。

## 版本三：更贴近你截图风格

项目一：分布式即时通讯系统

项目描述：
基于 Go-Zero 构建支持多网关横向扩展的工业级 IM 架构，解决分布式场景下的消息路由、顺序一致性、离线补偿和异步扩散问题。

技术栈：
Go、Go-Zero、gRPC、Etcd、Redis、MySQL、Protobuf、Kafka、Prometheus、Docker

核心工作：
- 基于 go-zero REST + zRPC 官方脚手架重构 Gateway/Logic 服务入口，统一 `config/handler/logic/svc` 与 RPC `server/logic/svc` 分层。
- 基于 gRPC + Etcd 实现 Logic 服务注册发现，结合一致性哈希完成用户维度精准路由。
- 设计 Protobuf 二进制消息协议，统一 WebSocket 与内部服务消息模型。
- 利用 Redis Lua 生成会话 Sequence ID，结合 ACK 未确认消息补偿解决弱网下乱序与漏发。
- 引入 Kafka + Transfer 服务异步扩散群聊消息，并设计 retry / dead-letter 机制降低同步链路压力。
- 网关层接入 JWT、令牌桶限流、双向心跳和 worker pool，提升长连接稳定性与安全性。
- 暴露 Prometheus 指标，支持连接数、消息吞吐、ACK 与 Kafka 异常的可观测监控。
