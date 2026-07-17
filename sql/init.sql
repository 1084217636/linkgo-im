-- 1. 创建数据库（如果不存在）
CREATE DATABASE IF NOT EXISTS `linkgo_im` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;
USE `linkgo_im`;

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
    `joined_at` BIGINT NOT NULL COMMENT '加入会话时间',
    PRIMARY KEY (`conversation_id`, `user_id`),
    INDEX `idx_user_conversation` (`user_id`, `conversation_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='会话成员关系表';

-- 6. 好友申请与好友关系：用于单聊权限校验和联系人列表
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

-- 8. 红包：第一版使用普通等额红包，抢红包通过 InnoDB 行锁防止超卖
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

-- 9. AI群聊总结：保存总结、待办、风险和消息范围，支撑审计与回放
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

-- 11. 游戏运营控制面角色与审计
CREATE TABLE IF NOT EXISTS `platform_user_roles` (
    `user_id` VARCHAR(64) NOT NULL,
    `role` VARCHAR(32) NOT NULL COMMENT 'operator/reviewer/admin',
    `status` VARCHAR(16) NOT NULL DEFAULT 'active',
    `created_at` BIGINT NOT NULL,
    `updated_at` BIGINT NOT NULL,
    PRIMARY KEY (`user_id`),
    INDEX `idx_platform_role_status` (`role`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='游戏运营控制面用户角色';

CREATE TABLE IF NOT EXISTS `operation_audit_logs` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `audit_id` VARCHAR(64) NOT NULL,
    `operator_id` VARCHAR(64) NOT NULL,
    `operator_role` VARCHAR(32) NOT NULL,
    `operation` VARCHAR(64) NOT NULL,
    `resource_type` VARCHAR(32) NOT NULL,
    `resource_id` VARCHAR(128) NOT NULL,
    `request_id` VARCHAR(128) NOT NULL DEFAULT '',
    `result` VARCHAR(16) NOT NULL,
    `detail_json` JSON NOT NULL,
    `trace_id` VARCHAR(64) NOT NULL DEFAULT '',
    `client_ip` VARCHAR(64) NOT NULL DEFAULT '',
    `created_at` BIGINT NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_operation_audit_id` (`audit_id`),
    INDEX `idx_operation_audit_operator_time` (`operator_id`, `created_at`),
    INDEX `idx_operation_audit_resource_time` (`resource_type`, `resource_id`, `created_at`),
    INDEX `idx_operation_audit_operation_time` (`operation`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='运营管理操作审计日志';

CREATE TABLE IF NOT EXISTS `game_activities` (
    `activity_id` VARCHAR(64) NOT NULL,
    `name` VARCHAR(128) NOT NULL,
    `status` VARCHAR(24) NOT NULL,
    `current_version` INT NOT NULL DEFAULT 0,
    `published_version` INT NOT NULL DEFAULT 0,
    `rollout_percent` INT NOT NULL DEFAULT 0,
    `created_by` VARCHAR(64) NOT NULL,
    `created_at` BIGINT NOT NULL,
    `updated_at` BIGINT NOT NULL,
    PRIMARY KEY (`activity_id`),
    INDEX `idx_game_activity_status_time` (`status`, `updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='游戏运营活动主记录';

CREATE TABLE IF NOT EXISTS `game_activity_versions` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `activity_id` VARCHAR(64) NOT NULL,
    `version` INT NOT NULL,
    `status` VARCHAR(24) NOT NULL,
    `config_json` JSON NOT NULL,
    `rollout_percent` INT NOT NULL,
    `created_by` VARCHAR(64) NOT NULL,
    `approved_by` VARCHAR(64) NOT NULL DEFAULT '',
    `created_at` BIGINT NOT NULL,
    `updated_at` BIGINT NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_game_activity_version` (`activity_id`, `version`),
    INDEX `idx_game_activity_version_status` (`activity_id`, `status`, `version`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='活动配置版本';

CREATE TABLE IF NOT EXISTS `gameops_outbox` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `event_id` VARCHAR(64) NOT NULL,
    `event_type` VARCHAR(32) NOT NULL,
    `aggregate_id` VARCHAR(64) NOT NULL,
    `payload_json` JSON NOT NULL,
    `status` VARCHAR(16) NOT NULL DEFAULT 'pending',
    `created_at` BIGINT NOT NULL,
    `updated_at` BIGINT NOT NULL,
    `processed_at` BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_gameops_outbox_event` (`event_id`),
    INDEX `idx_gameops_outbox_status` (`event_type`, `status`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='运营配置缓存同步 Outbox';

-- 12. 预置实验账号（create_time 随便填的当前毫秒值）
INSERT INTO `users` (`user_id`, `username`, `password`, `created_at`, `updated_at`) VALUES 
('1001', 'userA', '$2b$10$msHwvw.T/fpIilP9oGc3GuIkXKv1m1HtGzWkU.UHzFaEoj.r83SvK', 1710100000000, 1710100000000),
('1002', 'userB', '$2b$10$msHwvw.T/fpIilP9oGc3GuIkXKv1m1HtGzWkU.UHzFaEoj.r83SvK', 1710100000000, 1710100000000),
('1003', 'userC', '$2b$10$msHwvw.T/fpIilP9oGc3GuIkXKv1m1HtGzWkU.UHzFaEoj.r83SvK', 1710100000000, 1710100000000),
('9001', 'ai_assistant', '$2b$10$THQMtV0aOUVUUUoRC7Sj.unXkjj2.DEKIFQ9nuPj86yaqIc9AbB0q', 1710100000000, 1710100000000)
ON DUPLICATE KEY UPDATE
  username = VALUES(username),
  password = VALUES(password),
  updated_at = VALUES(updated_at);

INSERT INTO `platform_user_roles` (`user_id`, `role`, `status`, `created_at`, `updated_at`) VALUES
('1001', 'operator', 'active', 1710100000000, 1710100000000),
('1002', 'reviewer', 'active', 1710100000000, 1710100000000),
('1003', 'admin', 'active', 1710100000000, 1710100000000)
ON DUPLICATE KEY UPDATE
  `role` = VALUES(`role`),
  `status` = VALUES(`status`),
  `updated_at` = VALUES(`updated_at`);

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
