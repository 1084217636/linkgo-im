# 游戏 AI 工程化方向简历版本

这版用于投递“游戏 AI 工程师 / AI 工程化 / AI Agent 研发流程”方向。项目排序建议是：AI Agent 平台放第一，IM 项目作为服务端工程能力补充。

## 技能特长

编程语言：熟悉 Python、Go，具备脚本工具开发、服务端接口开发与工程化落地经验。

AI 工程：了解 LLM 应用接入、Prompt 设计、Function Call / Tool Calling、RAG、Agent Workflow，具备基于 LangGraph 构建多阶段 Agent 工作流的实践经验。

研发流程自动化：具备将 AI 能力接入研发流程的实践经验，熟悉需求拆解、上下文检索、代码生成、审查验证、结果留痕与失败回滚等 AI 辅助研发闭环设计。

后端基础：熟悉 Redis、MySQL、Kafka、gRPC、Etcd 等常见组件，了解服务注册发现、消息异步解耦、缓存、可靠投递与高并发连接处理机制。

工程能力：熟悉 Docker、Kubernetes、Git、GitHub Actions 与 Linux 开发调试，具备本地验证、Docker 沙盒验证、K8s 部署清单、Diff 产物管理、日志留痕与回滚保护等工程化验证经验。

游戏与 AI 场景理解：关注 AI 在游戏研发流程中的落地，包括策划配置检查、代码辅助生成、测试用例补全、QA 回归辅助、研发知识库问答与版本交付质量验证等场景。

## 项目一：面向研发流程的 AI Agent 辅助平台｜核心开发｜2026.01 - 2026.04

项目描述：
基于 Python 与 LangGraph 构建面向 Go 单仓代码仓库的 AI 辅助研发平台，围绕需求理解、任务拆解、上下文检索、代码修改、测试验证、质量评估与失败回滚形成闭环。平台支持以 CLI / 工具方式接入研发流程，可辅助完成接口修改、配置检查、测试补全、代码审查与交付前验证，重点解决 AI 生成结果上下文不足、质量不可控、缺少验证与难以回溯的问题。

技术栈：
Python、LLM、LangGraph、Agent Workflow、AST、Hybrid RAG、Docker、Git、Diff/Artifacts

核心功能：
- 设计“需求理解 -> 任务拆解 -> 上下文增强 -> 代码修改 -> 执行验证 -> 结果留痕”的多阶段 AI 工作流，模拟研发流程中的实现、测试与回归闭环。
- 基于 LangGraph / StateGraph 设计 planner / implementer / reviewer 三阶段 Agent 协作流程，planner 负责任务拆解与风险判断，implementer 负责局部代码修改，reviewer 负责代码审查、边界检查与修正建议。
- 基于检索、AST 代码分析、文件读取、Diff 生成、执行验证等工具节点设计 Agent 工具调用链路，支持 Agent 按阶段完成上下文获取、代码修改、结果校验与失败处理。
- 基于 Go 轻量 AST 与语义 embedding 构建仓内上下文增强能力，提取 package、import、function、method、call relation 等结构信息，并结合代码、文档、Dockerfile、go.mod、Makefile 等工程文件实现 Hybrid RAG。
- 设计 AI 生成结果质量验证机制，支持 apply / validate / rollback 流程，对生成代码进行 Diff 应用、本地验证、Docker 沙盒验证、失败回滚与日志留痕，降低 AI 直接修改代码带来的风险。
- 支持对配置类文件进行字段完整性、引用关系、重复 ID 与数值范围检查，可用于策划配置/服务端配置的交付前校验。
- 支持根据函数签名、调用关系与历史测试文件生成测试建议，辅助补全基础单元测试与边界用例。
- 设计任务执行质量评估记录，统计修改文件数、验证结果、回滚状态、失败原因与人工接管标记，用于复盘 AI 工作质量、验证通过率、失败类型与人工接管点。

本仓库补充 demo：
- `tools/ai_agent_workflow/config_check.py`：配置检查 demo，输出配置问题、严重级别与修复建议。
- `tools/ai_agent_workflow/test_suggest.py`：测试补全建议 demo，扫描 Go 函数和已有测试，输出待补测函数。
- `tools/ai_agent_workflow/quality_summary.py`：质量评估 summary demo，记录变更文件、验证命令、测试结果、失败原因与人工接管标记。
- `scripts/ai_demo.sh`：一键运行上述 demo，生成 `artifacts/config_check_report.json`、`artifacts/test_suggestions.json`、`artifacts/quality_summary.json`。

## 项目二：分布式即时通讯系统｜核心开发｜2025.10 - 2026.01

项目描述：
基于 Go-Zero 构建分布式 IM 系统，围绕多网关接入、跨节点消息路由、可靠投递、会话有序性与长连接稳定性进行设计与实现，重点提升高并发连接场景下的消息收发稳定性与系统可扩展性。

技术栈：
Go、Go-Zero、gRPC、Etcd、Redis、MySQL、Kafka、Protobuf、Docker、Kubernetes、GitHub Actions

核心功能：
- 设计 Gateway / Logic / Transfer 服务分层：Gateway 负责 WebSocket 长连接、心跳保活和 ACK 接收，Logic 负责消息校验、会话路由、在线状态查询与单聊分发，Transfer 基于 Kafka 消费群聊任务并异步扩散。
- 结合 zRPC `p2c_ewma`、在线状态映射与 Redis Pub/Sub 实现单聊定向推送，支持用户连接分散在不同 Gateway 节点时的消息转发。
- 基于会话级 `seq`、接收方 ACK 与 pending 结构实现消息排序和失败补偿；Pub/Sub 仅用于在线实时通知，不作为可靠队列，ACK 未返回时保留未确认消息用于重连回放。
- 基于 Kafka + Transfer 服务实现群聊异步扩散，解耦网关连接层与群发链路，并在网关侧补充心跳保活、JWT 鉴权、限流与 worker pool。
- 基于 Docker Compose 完成本地多服务联调，补充 Kubernetes 部署清单和 GitHub Actions CI，覆盖自动测试、服务构建、镜像构建和交付前清单检查。
- 在本机 10 Gateway 多进程环境下完成 1w WebSocket 长连接与 30s 心跳测试，记录连接成功率、消息链路成功率和失败原因，用于定位连接层与路由链路瓶颈。

## 面试表达重点

- 不要把项目说成“AI Coding 工具”，而是说成“AI Agent 辅助研发流程平台”。
- 重点讲 AI 如何介入需求、代码、配置、测试、验证、回滚这些研发环节。
- 重点讲质量验证：Diff、Artifacts、validation log、summary、rollback、manual review。
- IM 项目用于证明服务端基础、网络连接、消息系统、中间件和压测能力，不要强行包装成游戏服务器。
