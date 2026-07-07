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
