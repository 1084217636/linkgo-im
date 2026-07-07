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
