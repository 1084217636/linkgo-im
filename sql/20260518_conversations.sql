USE `linkgo_im`;

SET @column_exists := (
    SELECT COUNT(*)
    FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'messages'
      AND COLUMN_NAME = 'conversation_id'
);
SET @ddl := IF(
    @column_exists = 0,
    'ALTER TABLE `messages` ADD COLUMN `conversation_id` VARCHAR(128) NOT NULL DEFAULT '''' COMMENT ''会话ID，和 session_id 保持一致，用于会话列表与唯一序列索引'' AFTER `message_id`',
    'SELECT 1'
);
PREPARE stmt FROM @ddl;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

UPDATE `messages` SET `conversation_id` = `session_id` WHERE `conversation_id` = '';

SET @index_exists := (
    SELECT COUNT(*)
    FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'messages'
      AND INDEX_NAME = 'uk_conversation_seq'
);
SET @ddl := IF(
    @index_exists = 0,
    'ALTER TABLE `messages` ADD UNIQUE KEY `uk_conversation_seq` (`conversation_id`, `seq`)',
    'SELECT 1'
);
PREPARE stmt FROM @ddl;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

CREATE TABLE IF NOT EXISTS `conversations` (
    `id` VARCHAR(128) NOT NULL COMMENT '会话ID: c2c:uid1:uid2 或 group:gid',
    `type` ENUM('user', 'group') NOT NULL DEFAULT 'user' COMMENT '会话类型',
    `created_at` BIGINT NOT NULL COMMENT '毫秒级创建时间',
    `updated_at` BIGINT NOT NULL COMMENT '最近消息时间',
    `last_seq` BIGINT NOT NULL DEFAULT 0 COMMENT '会话最新 seq',
    PRIMARY KEY (`id`),
    INDEX `idx_updated_at` (`updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='会话元信息表';

CREATE TABLE IF NOT EXISTS `conversation_members` (
    `conversation_id` VARCHAR(128) NOT NULL COMMENT '会话ID',
    `user_id` VARCHAR(64) NOT NULL COMMENT '成员用户ID',
    `read_seq` BIGINT NOT NULL DEFAULT 0 COMMENT '该用户在该会话已确认/已读到的 seq',
    `joined_at` BIGINT NOT NULL COMMENT '加入会话时间',
    PRIMARY KEY (`conversation_id`, `user_id`),
    INDEX `idx_user_conversation` (`user_id`, `conversation_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='会话成员关系表';
