# 简历项目描述

## 版本零：27届 Golang 实习投递版

项目名称：LinkGo-IM 分布式即时通讯系统

项目描述：
基于 Go + go-zero 实现的后端即时通讯项目，将系统拆分为 Gateway 长连接层、Logic 业务路由层和 Transfer 异步扩散层，围绕多 Gateway 场景下的连接管理、跨节点消息路由、会话级顺序控制、ACK 补偿与群聊异步扩散进行设计与实现。

技术栈：
Go、go-zero、gRPC、WebSocket、Redis、MySQL、Kafka、Etcd、Protobuf、Docker、Kubernetes、GitHub Actions、Prometheus

核心工作：
- 设计 Gateway / Logic / Transfer 服务分层：Gateway 负责 WebSocket 长连接、心跳保活和 ACK 接收，Logic 负责消息校验、会话路由、在线状态查询与单聊分发，Transfer 基于 Kafka 消费群聊任务并异步扩散。
- 基于 Redis 维护 `userId -> gatewayId` 在线映射、`pending_ack` 未确认集合和 `ack_idx` 消息索引；在线用户通过定向 Pub/Sub 推送，离线或 ACK 未返回时保留 pending，支持用户重连后回放补偿。
- 基于会话级 `seq` 实现顺序控制：单聊使用 `c2c:<uid1>:<uid2>`，群聊使用 `group:<group_id>` 作为会话维度，通过 Redis Lua 原子生成递增序号，客户端可按 `session_id + seq` 展示和补齐缺失消息。
- 使用 Kafka + Transfer 服务实现群聊异步扩散，将发送链路与群成员推送链路解耦，并设计 retry / dead-letter 失败处理流程，降低大群消息同步扩散对主链路的阻塞。
- 使用 gRPC + Etcd 实现 Logic 服务发现与内部 RPC 调用，Gateway 侧结合一致性哈希选择后端节点，支持本地多 Gateway 实例联调。
- 使用 Dockerfile 和 docker-compose 编排 Redis、MySQL、Kafka、Etcd 与业务服务，支持一键启动完整本地开发环境，并为 Gateway/Transfer 增加 healthcheck。
- 补充 Kubernetes Deployment / Service / readinessProbe / livenessProbe / resources / PDB 清单，支持 Gateway、Logic、Transfer 在集群中的副本管理、健康检查和滚动发布说明。
- 基于 GitHub Actions 搭建基础 CI 流程，自动执行 Go 单元测试、服务构建、Docker 镜像构建和 K8s 清单检查，形成提交后的质量门禁。
- 补充配置覆盖、健康检查、Transfer 工具函数等单元测试，统一异常日志到 go-zero `logx`，并通过 Prometheus 指标观察连接数、消息吞吐、ACK、Kafka 重试等运行状态。
- 编写 README、模块说明、压测报告和面试问答文档，沉淀系统结构、核心链路、启动方式和常见问题。

可压缩成简历 4 条：
- 基于 Go + go-zero 构建 IM 后端项目，拆分 Gateway、Logic、Transfer 三层，完成登录鉴权、WebSocket 长连接、单聊/群聊、历史消息和接收方 ACK 补偿等核心功能。
- 设计多 Gateway 跨节点消息路由：Redis 维护 `userId -> gatewayId` 在线映射，Logic 生成会话级 `seq` 后通过定向 Pub/Sub 推送在线用户，ACK 未返回时保留 pending 并支持重连回放。
- 基于 Kafka + Transfer 实现群聊异步扩散，将发送链路与群成员推送链路解耦，并增加 retry / dead-letter 处理失败任务。
- 基于 Docker Compose 编排完整本地环境，补充 K8s 部署清单和 GitHub Actions CI，覆盖自动测试、服务构建、镜像构建、健康检查和基础清单校验，提升调试与交付前验证效率。

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
基于 Go-Zero 构建支持多网关横向扩展的 IM 后端架构，解决分布式场景下的消息路由、顺序一致性、离线补偿和异步扩散问题。

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

## 版本四：带量化指标

项目一：分布式即时通讯系统 ｜ 核心开发

项目描述：
基于 Go-Zero 构建分布式 IM 系统，围绕跨节点消息路由、可靠投递、会话有序性与长连接稳定性进行设计与实现，重点解决多实例场景下消息收发闭环、弱网环境下乱序重试以及高并发连接下的系统稳定性问题。

技术栈：
Go、Go-Zero、gRPC、Etcd、Redis、MySQL、Protobuf、Kafka、Docker、Prometheus

核心工作：
- 基于 gRPC + Etcd 实现服务注册发现，支持本地 10 个 Gateway 多实例联调，结合一致性哈希与在线状态中心完成跨节点消息路由，跨网关单聊链路验证稳定可达。
- 设计单聊直投、群聊异步扩散和历史消息查询链路，在 10 Gateway 环境下完成 1w WebSocket 长连接验证，30s 心跳收发测试 `10000/10000` 成功。
- 设计统一的 Protobuf 二进制消息协议，复用 WebSocket 与 gRPC 消息模型，减少 JSON 文本和冗余字段开销；3 Gateway 压测下等价 2400 并发请求 20s 内无 500 错误，响应稳定在 90~110ms。
- 基于 Redis Lua 生成会话级递增 `seq`，结合 `pending_ack / offline_msg / ack_idx` 实现接收方 ACK 跟踪、离线补偿与未确认消息重放；单 Gateway 场景完成 300 并发稳定验证。
- 引入 Kafka + Transfer 服务处理群聊异步扩散、重试与死信，结合心跳检测、JWT 鉴权、限流控制和 worker pool 提升稳定性；在本机 10 Gateway 多进程环境下完成 1w WebSocket 连接与心跳验证，并记录成功率和失败原因用于定位连接层瓶颈。

## 版本五：当前面试版

项目一：分布式即时通讯系统 ｜ 核心开发

项目描述：
基于 Go-Zero 构建分布式 IM 系统，围绕跨节点消息路由、可靠投递、会话有序性与长连接稳定性进行设计与实现，重点解决多实例场景下消息收发闭环、弱网重试补偿及高并发连接场景下的系统稳定性问题。

技术栈：
Go、Go-Zero、gRPC、Etcd、Redis、MySQL、Protobuf、Kafka、Docker

核心功能：
- 基于 gRPC + Etcd 实现 Logic 服务注册与发现，本地通过多实例方式模拟 10 个 Gateway 节点；Gateway 侧结合一致性哈希选择 Logic 节点，Logic 侧再结合在线状态映射与定向 Pub/Sub 完成跨节点单聊路由。
- 基于会话级 `seq`、接收方 ACK 与 pending 结构实现消息排序和失败补偿；Pub/Sub 仅用于在线实时通知，不作为可靠队列，ACK 未返回时保留未确认消息用于重连回放。
- 基于 Kafka + Transfer 服务实现群聊异步扩散，解耦网关连接层与群发链路；在网关侧补充心跳保活、JWT 鉴权、限流与 worker pool，提升高并发连接场景下的系统稳定性。
- 在本机 10 Gateway 多进程环境下完成 1w WebSocket 长连接与 30s 心跳测试，记录连接成功率、消息链路成功率和失败原因，用于定位连接层与路由链路瓶颈。
