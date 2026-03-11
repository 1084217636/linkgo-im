## LinkGo-IM 
基于 Go 语言实现的高性能分布式即时通讯系统。

### 核心亮点：
*   **分布式架构**：支持多节点水平扩展，利用 Redis Pub/Sub 实现跨节点消息推送。
*   **消息可靠性**：集成 MySQL 实现消息持久化，支持离线消息拉取与历史记录查询。
*   **自研网关**：基于 WebSocket + gRPC 架构，实现长连接管理与业务逻辑解耦。
*   **工程化部署**：全镜像 Docker-Compose 一键编排，包含 MySQL 健康检查与自动初始化脚本。

[![Go Version](https://img.shields.io/badge/Go-1.20+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-Supported-2496ED?style=flat&logo=docker)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

LinkGo-IM 是一个解耦了**长连接接入层 (Gateway)** 与 **业务逻辑层 (Logic)** 的分布式即时通讯系统。支持多节点部署，利用 Redis 实现了跨节点的分布式消息路由与离线消息可靠同步。

---

## 🏗️ 核心架构 (Architecture)
本项目采用典型的“网关-逻辑-存储”三层架构，旨在解决单机连接瓶颈与复杂业务逻辑的耦合问题：

*   **接入层 (Gateway)**: 基于 Gin + WebSocket。负责维持海量长连接，执行心跳检测与安全鉴权，不处理复杂业务，支持横向水平扩容。
*   **逻辑层 (Logic)**: 系统的“大脑”。处理消息路由、群组管理、状态同步。与网关层通过高性能 **gRPC** 通信。
*   **存储层 (Storage)**: 
    *   **Redis**: 负责维护分布式路由表 (Registry)、实现发布/订阅 (Pub/Sub) 跨机推送、以及利用 **ZSet** 实现离线消息暂存。
    *   **MySQL**: 负责用户信息及聊天历史记录的持久化。

---

## ✨ 核心技术亮点 (Key Features)

-   **分布式消息路由**: 配合 Redis Pub/Sub 机制，实现用户在不同网关节点间的透明通信。
-   **离线消息补偿**: 采用 Redis ZSet (Score 为纳秒时间戳) 存储离线消息，确保用户上线后能按**时序**准确拉取未读消息。
-   **心跳检测机制**: 前后端双向心跳检测，服务端自动清理僵尸连接，优化内存占用。
-   **微服务通信**: Gateway 与 Logic 之间基于 Protobuf 定义接口，通过 gRPC 实现低延迟调用。
-   **容器化生态**: 预置 `docker-compose` 环境，一键编排 MySQL、Redis 及微服务集群。

---

## 🚀 快速开始 (Quick Start)

### 1. 环境准备
确保已安装 `Docker` 和 `Docker Compose`。

### 2. 一键启动
在项目根目录下执行：
```bash
docker-compose up --build