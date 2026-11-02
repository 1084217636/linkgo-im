USE `linkgo_im`;

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
