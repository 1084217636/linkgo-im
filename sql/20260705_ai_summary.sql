USE `linkgo_im`;

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
