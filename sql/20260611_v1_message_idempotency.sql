USE `linkgo_im`;

SET @column_exists := (
    SELECT COUNT(*)
    FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'messages'
      AND COLUMN_NAME = 'client_msg_id'
);
SET @ddl := IF(
    @column_exists = 0,
    'ALTER TABLE `messages` ADD COLUMN `client_msg_id` VARCHAR(128) NULL COMMENT ''客户端生成的发送幂等ID，同一发送者内唯一'' AFTER `message_id`',
    'SELECT 1'
);
PREPARE stmt FROM @ddl;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @index_exists := (
    SELECT COUNT(*)
    FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'messages'
      AND INDEX_NAME = 'uk_sender_client_msg'
);
SET @ddl := IF(
    @index_exists = 0,
    'ALTER TABLE `messages` ADD UNIQUE KEY `uk_sender_client_msg` (`from_uid`, `client_msg_id`)',
    'SELECT 1'
);
PREPARE stmt FROM @ddl;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
