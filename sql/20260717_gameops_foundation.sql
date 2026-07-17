CREATE TABLE IF NOT EXISTS `platform_user_roles` (
    `user_id` VARCHAR(64) NOT NULL,
    `role` VARCHAR(32) NOT NULL COMMENT 'operator/reviewer/admin',
    `status` VARCHAR(16) NOT NULL DEFAULT 'active',
    `created_at` BIGINT NOT NULL,
    `updated_at` BIGINT NOT NULL,
    PRIMARY KEY (`user_id`),
    INDEX `idx_platform_role_status` (`role`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='游戏运营控制面用户角色';

CREATE TABLE IF NOT EXISTS `operation_audit_logs` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `audit_id` VARCHAR(64) NOT NULL,
    `operator_id` VARCHAR(64) NOT NULL,
    `operator_role` VARCHAR(32) NOT NULL,
    `operation` VARCHAR(64) NOT NULL,
    `resource_type` VARCHAR(32) NOT NULL,
    `resource_id` VARCHAR(128) NOT NULL,
    `request_id` VARCHAR(128) NOT NULL DEFAULT '',
    `result` VARCHAR(16) NOT NULL,
    `detail_json` JSON NOT NULL,
    `trace_id` VARCHAR(64) NOT NULL DEFAULT '',
    `client_ip` VARCHAR(64) NOT NULL DEFAULT '',
    `created_at` BIGINT NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_operation_audit_id` (`audit_id`),
    INDEX `idx_operation_audit_operator_time` (`operator_id`, `created_at`),
    INDEX `idx_operation_audit_resource_time` (`resource_type`, `resource_id`, `created_at`),
    INDEX `idx_operation_audit_operation_time` (`operation`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='运营管理操作审计日志';

INSERT INTO `platform_user_roles` (`user_id`, `role`, `status`, `created_at`, `updated_at`) VALUES
('1001', 'operator', 'active', 1710100000000, 1710100000000),
('1002', 'reviewer', 'active', 1710100000000, 1710100000000),
('1003', 'admin', 'active', 1710100000000, 1710100000000)
ON DUPLICATE KEY UPDATE
  `role` = VALUES(`role`),
  `status` = VALUES(`status`),
  `updated_at` = VALUES(`updated_at`);
