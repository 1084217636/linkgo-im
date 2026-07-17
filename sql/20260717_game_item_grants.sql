CREATE TABLE IF NOT EXISTS `game_item_grant_requests` (
    `grant_request_id` VARCHAR(128) NOT NULL,
    `operator_id` VARCHAR(64) NOT NULL,
    `status` VARCHAR(16) NOT NULL,
    `item_count` INT NOT NULL,
    `created_at` BIGINT NOT NULL,
    `updated_at` BIGINT NOT NULL,
    PRIMARY KEY (`grant_request_id`),
    INDEX `idx_grant_request_operator_time` (`operator_id`, `created_at`),
    INDEX `idx_grant_request_status_time` (`status`, `updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='道具批量发放请求';

CREATE TABLE IF NOT EXISTS `game_item_grants` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `grant_request_id` VARCHAR(128) NOT NULL,
    `user_id` VARCHAR(128) NOT NULL,
    `item_id` VARCHAR(128) NOT NULL,
    `quantity` BIGINT NOT NULL,
    `status` VARCHAR(16) NOT NULL,
    `operator_id` VARCHAR(64) NOT NULL,
    `created_at` BIGINT NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_item_grant_idempotency` (`grant_request_id`, `user_id`, `item_id`),
    INDEX `idx_item_grant_user_time` (`user_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='道具发放明细';

CREATE TABLE IF NOT EXISTS `player_items` (
    `user_id` VARCHAR(128) NOT NULL,
    `item_id` VARCHAR(128) NOT NULL,
    `quantity` BIGINT NOT NULL DEFAULT 0,
    `updated_at` BIGINT NOT NULL,
    PRIMARY KEY (`user_id`, `item_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='玩家道具余额';

CREATE TABLE IF NOT EXISTS `game_item_grant_failures` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `failure_id` VARCHAR(64) NOT NULL,
    `grant_request_id` VARCHAR(128) NOT NULL,
    `operator_id` VARCHAR(64) NOT NULL,
    `error_message` VARCHAR(512) NOT NULL,
    `created_at` BIGINT NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_item_grant_failure` (`failure_id`),
    INDEX `idx_item_grant_failure_request` (`grant_request_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='道具发放失败记录';
