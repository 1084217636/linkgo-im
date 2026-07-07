# AI FAQ

## 群聊为什么用 Kafka

群聊扩散是高扇出操作，如果 Logic 在发送请求里同步遍历所有群成员，会直接拖慢主消息链路。项目把群聊扩散拆到 Kafka + Transfer，是为了把发送确认和大规模 fanout 解耦，支持异步扩容、失败重试和死信处理。

## ACK 是什么

当前 ACK 是投递确认，不是已读回执。客户端收到消息后回传 ACK，服务端据此清理 pending_ack、offline_msg、ack_idx 和 ack_retry。它表示“客户端已收到”，不表示“用户已阅读”。

## Redis 和 MySQL 分别负责什么

MySQL 保存最终历史消息、会话元信息、群成员关系、红包记录和 AI 结果。Redis 保存在线路由、pending_ack、offline_msg、session_timeline、message_payload 和会话列表热索引。Redis 可以丢热数据后回源，MySQL 是最终事实来源。

## 为什么 Redis Pub/Sub 不能当可靠队列

Redis Pub/Sub 没有持久化和消费确认，订阅者不在线时消息会丢。项目里只把 Pub/Sub 用作在线实时通知，可靠性依赖 pending_ack、offline_msg、session_timeline 和 MySQL 历史消息。

## 为什么 AI 不进入 WebSocket 主链路

AI 能力是企业协同增强，不应该阻塞实时消息投递。群聊总结和知识库问答都做成独立 HTTP 接口，只读取已经落库的消息或项目文档，不影响 Gateway、Logic、Transfer 的实时链路。

## 为什么还要保留 mock provider

mock provider 让本地演示、单元测试和秋招答辩不依赖外部模型 key。真实模型接入只替换 Provider，不改业务接口和权限逻辑。这样既能证明工程抽象，也能控制网络、限流和成本风险。

## 知识库问答的最小闭环是什么

最小闭环不是一上来接向量库，而是先把 FAQ 和项目文档做成可检索语料，完成 question -> retrieve docs -> provider answer -> save ai_qa_records 的流程。这样先证明业务闭环，再讨论 embedding、BM25 或更复杂的索引。

## 为什么问答结果也要落库

企业场景下，谁问了什么、系统基于哪些资料回答、回答是否命中知识库，都需要留痕。项目通过 ai_qa_records 保存 question、answer、sources_json、provider、knowledge_hits、status 和 error_message，方便复盘和面试说明。
