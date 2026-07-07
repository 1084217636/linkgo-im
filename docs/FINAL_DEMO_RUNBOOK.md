# 项目一最终 Demo Runbook

## 1. 最推荐的标准演示

```bash
START_STACK=1 make im-ai-final-demo
```

这个命令会顺序执行：

```text
core IM demo
AI group summary demo
AI knowledge ask demo
```

输出目录：

```text
artifacts/final_im_ai_demo/
  core_im/
  ai_summary/
  ai_ask/
```

## 2. 单独演示命令

核心 IM：

```bash
START_STACK=1 make core-im-demo
```

AI 群聊总结：

```bash
START_STACK=1 make ai-demo
```

AI FAQ/RAG 问答：

```bash
START_STACK=1 make ai-ask-demo
```

完整群聊 Transfer：

```bash
START_STACK=1 make group-transfer-demo
```

## 3. 演示时重点看什么

core IM：

```text
login
websocket connect
single chat receive + ack
offline replay + ack
mysql messages persisted
gateway metrics exposed
```

AI summary：

```text
summary_id
conversation_id = group:G_AI_DEMO
todos / risks
provider = mock
```

AI ask：

```text
answer_id
question
knowledge_hits
sources
provider = mock
```

## 4. V8 实际 ask demo 证据

当前已验证的 ask 响应摘要：

```text
question = 群聊为什么用 Kafka？
knowledge_hits = 3
provider = mock
sources = docs/AI_FAQ.md, docs/INTERVIEW_QA.md, docs/CORE_LINKS.md
```

## 5. 为什么这个 demo 组合适合秋招

因为它能把项目讲成两条主线：

```text
1. IM 主链路工程能力：登录、长连接、ACK、离线补偿、群聊 fanout。
2. 协同增强能力：群聊总结、FAQ/RAG、provider 抽象、审计留痕。
```

这比只跑一个聊天接口更像真实公司项目。
