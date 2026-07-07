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
