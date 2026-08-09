USE `linkgo_im`;

SET @column_exists := (
    SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'conversation_members'
      AND COLUMN_NAME = 'acked_seq'
);
SET @ddl := IF(
    @column_exists = 0,
    'ALTER TABLE `conversation_members` ADD COLUMN `acked_seq` BIGINT NOT NULL DEFAULT 0 COMMENT ''客户端可靠收到并 ACK 到的 seq'' AFTER `read_seq`',
    'SELECT 1'
);
PREPARE stmt FROM @ddl;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

UPDATE `conversation_members`
SET `acked_seq` = `read_seq`
WHERE `acked_seq` = 0 AND `read_seq` > 0;

INSERT INTO `conversation_members` (`conversation_id`, `user_id`, `read_seq`, `acked_seq`, `joined_at`)
SELECT CONCAT('group:', gm.group_id), gm.user_id, 0, 0, gm.joined_at
FROM `group_members` gm
LEFT JOIN `conversation_members` cm
  ON cm.conversation_id = CONCAT('group:', gm.group_id)
 AND cm.user_id = gm.user_id
WHERE gm.status = 'active' AND cm.user_id IS NULL;

CREATE TABLE IF NOT EXISTS `conversation_outbox` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `message_id` VARCHAR(160) NOT NULL,
    `session_id` VARCHAR(128) NOT NULL,
    `from_uid` VARCHAR(64) NOT NULL,
    `to_id` VARCHAR(64) NOT NULL,
    `to_type` ENUM('user', 'group') NOT NULL,
    `seq` BIGINT NOT NULL,
    `sent_at` BIGINT NOT NULL,
    `status` ENUM('pending', 'processing', 'done') NOT NULL DEFAULT 'pending',
    `attempts` INT NOT NULL DEFAULT 0,
    `available_at` BIGINT NOT NULL,
    `last_error` VARCHAR(512) NOT NULL DEFAULT '',
    `created_at` BIGINT NOT NULL,
    `processed_at` BIGINT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_conversation_outbox_message` (`message_id`),
    INDEX `idx_conversation_outbox_ready` (`status`, `available_at`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='会话摘要可靠投递 Outbox';
