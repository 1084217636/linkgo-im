-- 1. 创建数据库（如果不存在）
CREATE DATABASE IF NOT EXISTS `linkgo_im` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;
USE `linkgo_im`;

CREATE TABLE IF NOT EXISTS `attachments` (
    `attachment_id` VARCHAR(64) NOT NULL,
    `group_id` VARCHAR(64) NOT NULL,
    `uploader_id` VARCHAR(64) NOT NULL,
    `file_name` VARCHAR(200) NOT NULL,
    `mime_type` VARCHAR(128) NOT NULL,
    `file_size` BIGINT UNSIGNED NOT NULL,
    `sha256` CHAR(64) NOT NULL,
    `storage_key` VARCHAR(128) NOT NULL,
    `status` ENUM('ready','deleted') NOT NULL DEFAULT 'ready',
    `created_at` BIGINT NOT NULL,
    PRIMARY KEY (`attachment_id`),
    UNIQUE KEY `uk_attachment_storage_key` (`storage_key`),
    KEY `idx_attachment_group` (`group_id`,`status`,`created_at`),
    KEY `idx_attachment_uploader` (`uploader_id`,`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='群聊附件元数据；文件字节保存在共享文件卷';

-- 2. 用户表：增加索引优化和默认值
CREATE TABLE IF NOT EXISTS `users` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '自增主键',
    `user_id` VARCHAR(64) NOT NULL UNIQUE COMMENT '对外公开的唯一用户标识',
    `username` VARCHAR(32) NOT NULL UNIQUE COMMENT '登录名',
    `password` VARCHAR(128) NOT NULL COMMENT 'bcrypt 密码哈希值',
    `avatar` VARCHAR(255) DEFAULT '' COMMENT '头像地址',
    `status` TINYINT(1) DEFAULT 1 COMMENT '状态: 1正常 0禁用',
    `created_at` BIGINT NOT NULL COMMENT '毫秒级创建时间',
    `updated_at` BIGINT NOT NULL COMMENT '毫秒级更新时间',
    PRIMARY KEY (`id`),
    INDEX `idx_user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户基础信息表';

-- 3. 消息表：针对 IM 场景优化的索引设计
CREATE TABLE IF NOT EXISTS `messages` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `message_id` VARCHAR(160) NOT NULL COMMENT '消息唯一标识: session_id + seq',
    `client_msg_id` VARCHAR(128) NULL COMMENT '客户端生成的发送幂等ID，同一发送者内唯一',
    `conversation_id` VARCHAR(128) NOT NULL COMMENT '会话ID，和 session_id 保持一致，用于会话列表与唯一序列索引',
    `session_id` VARCHAR(128) NOT NULL COMMENT '会话ID: 单聊(sorted_uid_uid), 群聊(G_id)',
    `seq` BIGINT NOT NULL COMMENT '会话内单调递增序列，用于乱序保护与补偿同步',
    `from_uid` VARCHAR(64) NOT NULL COMMENT '发送者ID',
    `to_id` VARCHAR(64) NOT NULL COMMENT '接收者ID(用户ID或群ID)',
    `to_type` ENUM('user', 'group') NOT NULL DEFAULT 'user' COMMENT '接收类型',
    `content` TEXT NOT NULL COMMENT '消息内容',
    `msg_type` TINYINT(1) DEFAULT 1 COMMENT '消息类型: 1文字 2图片 3语音',
    `create_time` BIGINT NOT NULL COMMENT '毫秒级时间戳',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_message_id` (`message_id`),
    UNIQUE KEY `uk_sender_client_msg` (`from_uid`, `client_msg_id`),
    UNIQUE KEY `uk_conversation_seq` (`conversation_id`, `seq`),
    -- 核心索引：历史记录查询全靠它
    INDEX `idx_session_seq` (`session_id`, `seq`),
    INDEX `idx_session_time` (`session_id`, `create_time`),
    INDEX `idx_from_uid` (`from_uid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='聊天消息持久化表';

-- 4. 会话元信息：登录后按 updated_at 拉取最近会话列表
CREATE TABLE IF NOT EXISTS `conversations` (
    `id` VARCHAR(128) NOT NULL COMMENT '会话ID: c2c:uid1:uid2 或 group:gid',
    `type` ENUM('user', 'group') NOT NULL DEFAULT 'user' COMMENT '会话类型',
    `created_at` BIGINT NOT NULL COMMENT '毫秒级创建时间',
    `updated_at` BIGINT NOT NULL COMMENT '最近消息时间',
    `last_seq` BIGINT NOT NULL DEFAULT 0 COMMENT '会话最新 seq',
    PRIMARY KEY (`id`),
    INDEX `idx_updated_at` (`updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='会话元信息表';

-- 5. 用户会话关系：read_seq 用于登录会话列表计算未读数
CREATE TABLE IF NOT EXISTS `conversation_members` (
    `conversation_id` VARCHAR(128) NOT NULL COMMENT '会话ID',
    `user_id` VARCHAR(64) NOT NULL COMMENT '成员用户ID',
    `read_seq` BIGINT NOT NULL DEFAULT 0 COMMENT '该用户在该会话已确认/已读到的 seq',
    `acked_seq` BIGINT NOT NULL DEFAULT 0 COMMENT '该用户客户端可靠收到并 ACK 到的 seq',
    `joined_at` BIGINT NOT NULL COMMENT '加入会话时间',
    PRIMARY KEY (`conversation_id`, `user_id`),
    INDEX `idx_user_conversation` (`user_id`, `conversation_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='会话成员关系表';

-- 6. 会话摘要 Outbox：与消息落库同事务写入，失败后由 logic worker 重试
CREATE TABLE IF NOT EXISTS `conversation_outbox` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `message_id` VARCHAR(160) NOT NULL,
    `session_id` VARCHAR(128) NOT NULL,
    `from_uid` VARCHAR(64) NOT NULL,
    `to_id` VARCHAR(64) NOT NULL,
    `to_type` ENUM('user', 'group') NOT NULL,
    `seq` BIGINT NOT NULL,
    `sent_at` BIGINT NOT NULL,
    `status` ENUM('pending', 'processing', 'done', 'dead') NOT NULL DEFAULT 'pending',
    `attempts` INT NOT NULL DEFAULT 0,
    `available_at` BIGINT NOT NULL,
    `last_error` VARCHAR(512) NOT NULL DEFAULT '',
    `created_at` BIGINT NOT NULL,
    `processed_at` BIGINT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_conversation_outbox_message` (`message_id`),
    INDEX `idx_conversation_outbox_ready` (`status`, `available_at`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='会话摘要可靠投递 Outbox';

-- 7. 消息投递 Outbox：与消息事实同事务提交，Redis/Kafka 失败后异步重试
CREATE TABLE IF NOT EXISTS `delivery_outbox` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `message_id` VARCHAR(160) NOT NULL,
    `trace_id` VARCHAR(128) NOT NULL DEFAULT '',
    `recipients_json` JSON NOT NULL,
    `status` ENUM('pending', 'processing', 'done', 'dead') NOT NULL DEFAULT 'pending',
    `attempts` INT NOT NULL DEFAULT 0,
    `available_at` BIGINT NOT NULL,
    `last_error` VARCHAR(512) NOT NULL DEFAULT '',
    `created_at` BIGINT NOT NULL,
    `updated_at` BIGINT NOT NULL,
    `processed_at` BIGINT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_delivery_outbox_message` (`message_id`),
    INDEX `idx_delivery_outbox_ready` (`status`, `available_at`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='消息外部投递事务 Outbox';

-- 8. 好友申请与好友关系：用于单聊权限校验和联系人列表
CREATE TABLE IF NOT EXISTS `friend_requests` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `from_user_id` VARCHAR(64) NOT NULL COMMENT '申请人',
    `to_user_id` VARCHAR(64) NOT NULL COMMENT '接收人',
    `message` VARCHAR(255) NOT NULL DEFAULT '' COMMENT '申请备注',
    `status` ENUM('pending', 'accepted', 'rejected') NOT NULL DEFAULT 'pending',
    `created_at` BIGINT NOT NULL,
    `updated_at` BIGINT NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_friend_request_pair` (`from_user_id`, `to_user_id`),
    INDEX `idx_friend_request_to_status` (`to_user_id`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='好友申请表';

CREATE TABLE IF NOT EXISTS `friend_relations` (
    `user_id` VARCHAR(64) NOT NULL,
    `friend_id` VARCHAR(64) NOT NULL,
    `status` ENUM('normal', 'blocked', 'deleted') NOT NULL DEFAULT 'normal',
    `created_at` BIGINT NOT NULL,
    `updated_at` BIGINT NOT NULL,
    PRIMARY KEY (`user_id`, `friend_id`),
    INDEX `idx_friend_user_status` (`user_id`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='双向好友关系表';

-- 7. 群组与群成员：用于群消息权限校验和 Kafka 扩散成员来源
CREATE TABLE IF NOT EXISTS `im_groups` (
    `group_id` VARCHAR(64) NOT NULL,
    `name` VARCHAR(128) NOT NULL DEFAULT '',
    `owner_id` VARCHAR(64) NOT NULL,
    `status` ENUM('active', 'dismissed') NOT NULL DEFAULT 'active',
    `created_at` BIGINT NOT NULL,
    `updated_at` BIGINT NOT NULL,
    PRIMARY KEY (`group_id`),
    INDEX `idx_group_owner` (`owner_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='群组基础信息表';

CREATE TABLE IF NOT EXISTS `group_members` (
    `group_id` VARCHAR(64) NOT NULL,
    `user_id` VARCHAR(64) NOT NULL,
    `role` ENUM('owner', 'admin', 'member') NOT NULL DEFAULT 'member',
    `mute_until` BIGINT NOT NULL DEFAULT 0,
    `status` ENUM('active', 'left', 'removed') NOT NULL DEFAULT 'active',
    `joined_at` BIGINT NOT NULL,
    PRIMARY KEY (`group_id`, `user_id`),
    INDEX `idx_group_member_user` (`user_id`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='群成员关系表';

-- IM-P1 学习群任务。与 20261028_learning_tasks.sql 保持同一结构。
CREATE TABLE IF NOT EXISTS `group_tasks` (
  `task_id` VARCHAR(64) NOT NULL,
  `group_id` VARCHAR(64) NOT NULL,
  `title` VARCHAR(120) NOT NULL,
  `description` TEXT NOT NULL,
  `due_at` BIGINT NULL,
  `status` ENUM('draft','published','closed') NOT NULL DEFAULT 'draft',
  `version` BIGINT NOT NULL DEFAULT 1,
  `created_by` VARCHAR(64) NOT NULL,
  `published_at` BIGINT NULL,
  `closed_at` BIGINT NULL,
  `created_at` BIGINT NOT NULL,
  `updated_at` BIGINT NOT NULL,
  PRIMARY KEY (`task_id`),
  KEY `idx_group_tasks_list` (`group_id`,`status`,`created_at`,`task_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS `task_submissions` (
  `submission_id` VARCHAR(64) NOT NULL,
  `task_id` VARCHAR(64) NOT NULL,
  `user_id` VARCHAR(64) NOT NULL,
  `content_text` TEXT NOT NULL,
  `content_link` VARCHAR(2048) NOT NULL DEFAULT '',
  `attachment_ids` JSON NOT NULL,
  `status` ENUM('submitted','approved','returned') NOT NULL,
  `attempt_no` INT NOT NULL DEFAULT 1,
  `version` BIGINT NOT NULL DEFAULT 1,
  `reviewer_id` VARCHAR(64) NULL,
  `review_note` VARCHAR(1000) NOT NULL DEFAULT '',
  `submitted_at` BIGINT NOT NULL,
  `reviewed_at` BIGINT NULL,
  PRIMARY KEY (`submission_id`),
  UNIQUE KEY `uk_task_submitter` (`task_id`,`user_id`),
  KEY `idx_task_submission_status` (`task_id`,`status`,`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS `task_events` (
  `event_id` VARCHAR(64) NOT NULL,
  `task_id` VARCHAR(64) NOT NULL,
  `submission_id` VARCHAR(64) NULL,
  `actor_uid` VARCHAR(64) NOT NULL,
  `action` VARCHAR(32) NOT NULL,
  `request_id` VARCHAR(128) NOT NULL,
  `request_hash` CHAR(64) NOT NULL,
  `payload_json` JSON NOT NULL,
  `result_json` JSON NOT NULL,
  `created_at` BIGINT NOT NULL,
  PRIMARY KEY (`event_id`),
  UNIQUE KEY `uk_task_request` (`actor_uid`,`request_id`),
  KEY `idx_task_events_task` (`task_id`,`created_at`,`event_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS `task_notification_outbox` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `event_id` VARCHAR(64) NOT NULL,
  `target_type` ENUM('group','user') NOT NULL,
  `target_id` VARCHAR(64) NOT NULL,
  `status` ENUM('pending','processing','done','dead','skipped') NOT NULL DEFAULT 'pending',
  `attempts` INT NOT NULL DEFAULT 0,
  `available_at` BIGINT NOT NULL,
  `im_message_id` VARCHAR(160) NULL,
  `last_error` VARCHAR(512) NOT NULL DEFAULT '',
  `created_at` BIGINT NOT NULL,
  `updated_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_task_notification` (`event_id`,`target_type`,`target_id`),
  KEY `idx_task_notification_ready` (`status`,`available_at`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
INSERT IGNORE INTO `users` (`user_id`,`username`,`password`,`status`,`created_at`,`updated_at`)
VALUES ('9002','task_notifier','!disabled',0,0,0);

-- 8. 站内钱包：外部支付只负责充值，红包在站内账本中转账
CREATE TABLE IF NOT EXISTS `wallet_accounts` (
    `user_id` VARCHAR(64) NOT NULL COMMENT '用户ID',
    `available_balance` BIGINT NOT NULL DEFAULT 0 COMMENT '可用余额，单位分',
    `updated_at` BIGINT NOT NULL,
    PRIMARY KEY (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='站内钱包账户';

CREATE TABLE IF NOT EXISTS `wallet_ledger` (
    `id` VARCHAR(64) NOT NULL COMMENT '流水ID',
    `user_id` VARCHAR(64) NOT NULL,
    `direction` ENUM('credit', 'debit') NOT NULL COMMENT '入账或出账',
    `amount` BIGINT NOT NULL COMMENT '变动金额，单位分',
    `business_type` ENUM('mock_recharge', 'red_packet_send', 'red_packet_claim', 'red_packet_refund') NOT NULL,
    `business_id` VARCHAR(128) NOT NULL COMMENT '充值请求或红包ID',
    `balance_after` BIGINT NOT NULL COMMENT '变动后余额，单位分',
    `created_at` BIGINT NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_wallet_business` (`user_id`, `business_type`, `business_id`),
    INDEX `idx_wallet_ledger_user_time` (`user_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='站内钱包不可变流水';

CREATE TABLE IF NOT EXISTS `mock_payment_orders` (
    `id` VARCHAR(64) NOT NULL COMMENT '模拟支付订单ID',
    `request_id` VARCHAR(128) NOT NULL COMMENT '客户端充值幂等ID',
    `user_id` VARCHAR(64) NOT NULL,
    `channel` ENUM('mock_wechat', 'mock_alipay') NOT NULL,
    `amount` BIGINT NOT NULL COMMENT '充值金额，单位分',
    `status` ENUM('paid') NOT NULL DEFAULT 'paid',
    `provider_trade_no` VARCHAR(128) NOT NULL COMMENT '模拟渠道交易号',
    `created_at` BIGINT NOT NULL,
    `paid_at` BIGINT NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_mock_payment_request` (`user_id`, `request_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='仅用于本地演示的模拟充值订单';

-- 9. 红包：发出时扣钱包，领取时入钱包，过期余额退回钱包
CREATE TABLE IF NOT EXISTS `red_packets` (
    `id` VARCHAR(64) NOT NULL COMMENT '红包ID',
    `sender_id` VARCHAR(64) NOT NULL COMMENT '发红包用户',
    `conversation_id` VARCHAR(128) NOT NULL COMMENT '所属会话',
    `to_type` ENUM('user', 'group') NOT NULL DEFAULT 'user' COMMENT '会话类型',
    `total_amount` BIGINT NOT NULL COMMENT '总金额，单位分',
    `total_count` INT NOT NULL COMMENT '总份数',
    `remaining_amount` BIGINT NOT NULL COMMENT '剩余金额，单位分',
    `remaining_count` INT NOT NULL COMMENT '剩余份数',
    `greeting` VARCHAR(255) NOT NULL DEFAULT '' COMMENT '祝福语',
    `status` ENUM('active', 'finished', 'expired') NOT NULL DEFAULT 'active',
    `created_at` BIGINT NOT NULL,
    `expires_at` BIGINT NOT NULL,
    `updated_at` BIGINT NOT NULL,
    PRIMARY KEY (`id`),
    INDEX `idx_red_packet_conversation` (`conversation_id`, `created_at`),
    INDEX `idx_red_packet_sender` (`sender_id`, `created_at`),
    INDEX `idx_red_packet_status_expire` (`status`, `expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='IM红包主表';

CREATE TABLE IF NOT EXISTS `red_packet_claims` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `red_packet_id` VARCHAR(64) NOT NULL,
    `user_id` VARCHAR(64) NOT NULL,
    `amount` BIGINT NOT NULL COMMENT '抢到金额，单位分',
    `created_at` BIGINT NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_red_packet_user` (`red_packet_id`, `user_id`),
    INDEX `idx_red_packet_claim_time` (`red_packet_id`, `created_at`),
    INDEX `idx_red_packet_claim_user` (`user_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='IM红包领取记录';

-- 10. AI群聊总结：保存总结、待办、风险和消息范围，支撑审计与回放
CREATE TABLE IF NOT EXISTS `ai_summary_records` (
    `summary_id` VARCHAR(64) NOT NULL COMMENT 'AI总结ID',
    `group_id` VARCHAR(64) NOT NULL COMMENT '群组ID',
    `conversation_id` VARCHAR(128) NOT NULL COMMENT '群聊会话ID: group:<group_id>',
    `operator_id` VARCHAR(64) NOT NULL COMMENT '触发总结的用户',
    `message_start_seq` BIGINT NOT NULL COMMENT '总结覆盖的起始消息序号',
    `message_end_seq` BIGINT NOT NULL COMMENT '总结覆盖的结束消息序号',
    `summary` TEXT NOT NULL COMMENT '群聊总结',
    `todos_json` TEXT NOT NULL COMMENT '待办事项JSON',
    `risks_json` TEXT NOT NULL COMMENT '风险点JSON',
    `provider` VARCHAR(32) NOT NULL DEFAULT 'mock' COMMENT 'AI提供方',
    `created_at` BIGINT NOT NULL COMMENT '毫秒级创建时间',
    PRIMARY KEY (`summary_id`),
    INDEX `idx_ai_summary_group_time` (`group_id`, `created_at`),
    INDEX `idx_ai_summary_conversation_seq` (`conversation_id`, `message_end_seq`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='AI群聊总结记录表';

-- 10. AI调用日志：保存 provider 调用耗时、状态和失败原因，支撑审计与优化
CREATE TABLE IF NOT EXISTS `ai_call_logs` (
    `call_id` VARCHAR(64) NOT NULL COMMENT 'AI调用ID',
    `provider` VARCHAR(64) NOT NULL COMMENT 'AI提供方',
    `group_id` VARCHAR(64) NOT NULL COMMENT '群组ID',
    `conversation_id` VARCHAR(128) NOT NULL COMMENT '群聊会话ID',
    `operator_id` VARCHAR(64) NOT NULL COMMENT '触发用户',
    `message_count` INT NOT NULL COMMENT '输入消息数',
    `message_start_seq` BIGINT NOT NULL DEFAULT 0 COMMENT '覆盖起始seq',
    `message_end_seq` BIGINT NOT NULL DEFAULT 0 COMMENT '覆盖结束seq',
    `duration_ms` BIGINT NOT NULL COMMENT 'provider调用耗时',
    `status` VARCHAR(32) NOT NULL COMMENT 'success/error',
    `error_message` VARCHAR(512) NOT NULL DEFAULT '' COMMENT '失败原因',
    `created_at` BIGINT NOT NULL COMMENT '毫秒级创建时间',
    PRIMARY KEY (`call_id`),
    INDEX `idx_ai_call_provider_time` (`provider`, `created_at`),
    INDEX `idx_ai_call_group_time` (`group_id`, `created_at`),
    INDEX `idx_ai_call_status_time` (`status`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='AI调用审计日志';

CREATE TABLE IF NOT EXISTS `ai_provider_attempt_logs` (
    `attempt_id` VARCHAR(64) NOT NULL COMMENT 'AI provider attempt ID',
    `call_id` VARCHAR(64) NOT NULL COMMENT 'AI调用ID',
    `attempt_order` INT NOT NULL COMMENT '第几次provider尝试',
    `provider` VARCHAR(64) NOT NULL COMMENT 'AI提供方',
    `status` VARCHAR(32) NOT NULL COMMENT 'success/error',
    `duration_ms` BIGINT NOT NULL COMMENT 'provider尝试耗时',
    `error_message` VARCHAR(512) NOT NULL DEFAULT '' COMMENT '失败原因',
    `created_at` BIGINT NOT NULL COMMENT '毫秒级创建时间',
    PRIMARY KEY (`attempt_id`),
    INDEX `idx_ai_attempt_call_order` (`call_id`, `attempt_order`),
    INDEX `idx_ai_attempt_provider_time` (`provider`, `created_at`),
    INDEX `idx_ai_attempt_status_time` (`status`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='AI provider attempt 明细日志';

CREATE TABLE IF NOT EXISTS `ai_qa_records` (
    `answer_id` VARCHAR(64) NOT NULL COMMENT 'AI问答ID',
    `operator_id` VARCHAR(64) NOT NULL COMMENT '提问用户',
    `question` VARCHAR(1024) NOT NULL COMMENT '问题',
    `answer` TEXT NOT NULL COMMENT '回答',
    `sources_json` LONGTEXT NOT NULL COMMENT '命中的知识库资料JSON',
    `provider` VARCHAR(64) NOT NULL DEFAULT 'mock' COMMENT 'AI提供方',
    `knowledge_hits` INT NOT NULL DEFAULT 0 COMMENT '命中的知识片段数',
    `status` VARCHAR(32) NOT NULL DEFAULT 'success' COMMENT 'success/error',
    `error_message` VARCHAR(512) NOT NULL DEFAULT '' COMMENT '失败原因',
    `created_at` BIGINT NOT NULL COMMENT '毫秒级创建时间',
    PRIMARY KEY (`answer_id`),
    INDEX `idx_ai_qa_operator_time` (`operator_id`, `created_at`),
    INDEX `idx_ai_qa_provider_time` (`provider`, `created_at`),
    INDEX `idx_ai_qa_status_time` (`status`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='AI知识问答记录表';

-- 11. 预置实验账号（create_time 随便填的当前毫秒值）
INSERT INTO `users` (`user_id`, `username`, `password`, `created_at`, `updated_at`) VALUES 
('1001', 'userA', '$2b$10$msHwvw.T/fpIilP9oGc3GuIkXKv1m1HtGzWkU.UHzFaEoj.r83SvK', 1710100000000, 1710100000000),
('1002', 'userB', '$2b$10$msHwvw.T/fpIilP9oGc3GuIkXKv1m1HtGzWkU.UHzFaEoj.r83SvK', 1710100000000, 1710100000000),
('1003', 'userC', '$2b$10$msHwvw.T/fpIilP9oGc3GuIkXKv1m1HtGzWkU.UHzFaEoj.r83SvK', 1710100000000, 1710100000000),
('9001', 'ai_assistant', '$2b$10$THQMtV0aOUVUUUoRC7Sj.unXkjj2.DEKIFQ9nuPj86yaqIc9AbB0q', 1710100000000, 1710100000000)
ON DUPLICATE KEY UPDATE
  username = VALUES(username),
  password = VALUES(password),
  updated_at = VALUES(updated_at);

INSERT INTO `friend_relations` (`user_id`, `friend_id`, `status`, `created_at`, `updated_at`) VALUES
('1001', '1002', 'normal', 1710100000000, 1710100000000),
('1002', '1001', 'normal', 1710100000000, 1710100000000),
('1001', '1003', 'normal', 1710100000000, 1710100000000),
('1003', '1001', 'normal', 1710100000000, 1710100000000),
('1001', '9001', 'normal', 1710100000000, 1710100000000),
('9001', '1001', 'normal', 1710100000000, 1710100000000),
('1002', '9001', 'normal', 1710100000000, 1710100000000),
('9001', '1002', 'normal', 1710100000000, 1710100000000),
('1003', '9001', 'normal', 1710100000000, 1710100000000),
('9001', '1003', 'normal', 1710100000000, 1710100000000)
ON DUPLICATE KEY UPDATE status = VALUES(status), updated_at = VALUES(updated_at);
-- CREATE TABLE IF NOT EXISTS `messages` (
--   `id` int(11) NOT NULL AUTO_INCREMENT,
--   `session_id` varchar(255) NOT NULL COMMENT '单聊为sorted_uid_uid，群聊为群ID',
--   `from_uid` varchar(255) NOT NULL,
--   `to_id` varchar(255) NOT NULL,
--   `to_type` varchar(20) NOT NULL COMMENT 'user / group',
--   `content` text NOT NULL,
--   `create_time` bigint(20) NOT NULL,
--   PRIMARY KEY (`id`),
--   KEY `idx_session` (`session_id`)
-- ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Multi-community additive schema
-- Additive migration. Existing groups, messages and tasks are preserved.
USE linkgo_im;
CREATE TABLE IF NOT EXISTS community_profiles (
 group_id VARCHAR(64) PRIMARY KEY, description VARCHAR(1000) NOT NULL DEFAULT '',
 discoverable TINYINT NOT NULL DEFAULT 1,
 FOREIGN KEY (group_id) REFERENCES im_groups(group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS community_posts (
 post_id VARCHAR(64) PRIMARY KEY, group_id VARCHAR(64) NOT NULL, author_id VARCHAR(64) NOT NULL,
 kind ENUM('question','note','resource','announcement') NOT NULL,
 title VARCHAR(160) NOT NULL, body TEXT NOT NULL,
 status ENUM('published','hidden','deleted') NOT NULL DEFAULT 'published',
 pinned TINYINT NOT NULL DEFAULT 0, featured TINYINT NOT NULL DEFAULT 0,
 version BIGINT NOT NULL DEFAULT 1, created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL,
 KEY idx_community_feed(group_id,status,pinned,created_at,post_id),
 FOREIGN KEY (group_id) REFERENCES im_groups(group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS community_comments (
 comment_id VARCHAR(64) PRIMARY KEY, post_id VARCHAR(64) NOT NULL, author_id VARCHAR(64) NOT NULL,
 parent_id VARCHAR(64) NULL, body TEXT NOT NULL, created_at BIGINT NOT NULL,
 KEY idx_post_comments(post_id,created_at,comment_id), FOREIGN KEY(post_id) REFERENCES community_posts(post_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS community_reactions (
 post_id VARCHAR(64) NOT NULL, user_id VARCHAR(64) NOT NULL, created_at BIGINT NOT NULL,
 PRIMARY KEY(post_id,user_id), FOREIGN KEY(post_id) REFERENCES community_posts(post_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS community_post_attachments (
 post_id VARCHAR(64) NOT NULL, attachment_id VARCHAR(64) NOT NULL,
 PRIMARY KEY(post_id,attachment_id), FOREIGN KEY(post_id) REFERENCES community_posts(post_id),
 FOREIGN KEY(attachment_id) REFERENCES attachments(attachment_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS community_requests (
 actor_id VARCHAR(64) NOT NULL, request_id VARCHAR(128) NOT NULL, request_hash CHAR(64) NOT NULL,
 result_json JSON NOT NULL, created_at BIGINT NOT NULL, PRIMARY KEY(actor_id,request_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS community_event_outbox (
 event_id VARCHAR(128) PRIMARY KEY, group_id VARCHAR(64) NOT NULL, actor_id VARCHAR(64) NOT NULL,
 kind VARCHAR(32) NOT NULL, object_id VARCHAR(64) NOT NULL, recipient_id VARCHAR(64) NOT NULL DEFAULT '',
 status ENUM('pending','processing','done','dead') NOT NULL DEFAULT 'pending', attempts INT NOT NULL DEFAULT 0,
 available_at BIGINT NOT NULL, lease_token VARCHAR(64) NOT NULL DEFAULT '', last_error VARCHAR(512) NOT NULL DEFAULT '',
 created_at BIGINT NOT NULL, KEY idx_community_event_ready(status,available_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS community_notifications (
 id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, event_id VARCHAR(128) NOT NULL, recipient_id VARCHAR(64) NOT NULL,
 group_id VARCHAR(64) NOT NULL, kind VARCHAR(32) NOT NULL, object_id VARCHAR(64) NOT NULL,
 created_at BIGINT NOT NULL, read_at BIGINT NULL, UNIQUE KEY uk_notification(event_id,recipient_id),
 KEY idx_notification_user(recipient_id,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
