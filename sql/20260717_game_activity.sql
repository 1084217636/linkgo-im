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
