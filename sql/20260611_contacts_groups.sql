USE `linkgo_im`;

CREATE TABLE IF NOT EXISTS `friend_requests` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `from_user_id` VARCHAR(64) NOT NULL COMMENT '申请人',
    `to_user_id` VARCHAR(64) NOT NULL COMMENT '接收人',
    `message` VARCHAR(255) NOT NULL DEFAULT '' COMMENT '申请备注',
    `status` ENUM('pending', 'accepted', 'rejected') NOT NULL DEFAULT 'pending',
    `created_at` BIGINT NOT NULL,
    `updated_at` BIGINT NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_friend_request_pair` (`from_user_id`, `to_user_id`),
    INDEX `idx_friend_request_to_status` (`to_user_id`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='好友申请表';

CREATE TABLE IF NOT EXISTS `friend_relations` (
    `user_id` VARCHAR(64) NOT NULL,
    `friend_id` VARCHAR(64) NOT NULL,
    `status` ENUM('normal', 'blocked', 'deleted') NOT NULL DEFAULT 'normal',
    `created_at` BIGINT NOT NULL,
    `updated_at` BIGINT NOT NULL,
    PRIMARY KEY (`user_id`, `friend_id`),
    INDEX `idx_friend_user_status` (`user_id`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='双向好友关系表';

CREATE TABLE IF NOT EXISTS `im_groups` (
    `group_id` VARCHAR(64) NOT NULL,
    `name` VARCHAR(128) NOT NULL DEFAULT '',
    `owner_id` VARCHAR(64) NOT NULL,
    `status` ENUM('active', 'dismissed') NOT NULL DEFAULT 'active',
    `created_at` BIGINT NOT NULL,
    `updated_at` BIGINT NOT NULL,
    PRIMARY KEY (`group_id`),
    INDEX `idx_group_owner` (`owner_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='群组基础信息表';

CREATE TABLE IF NOT EXISTS `group_members` (
    `group_id` VARCHAR(64) NOT NULL,
    `user_id` VARCHAR(64) NOT NULL,
    `role` ENUM('owner', 'admin', 'member') NOT NULL DEFAULT 'member',
    `mute_until` BIGINT NOT NULL DEFAULT 0,
    `status` ENUM('active', 'left', 'removed') NOT NULL DEFAULT 'active',
    `joined_at` BIGINT NOT NULL,
    PRIMARY KEY (`group_id`, `user_id`),
    INDEX `idx_group_member_user` (`user_id`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='群成员关系表';

INSERT INTO `friend_relations` (`user_id`, `friend_id`, `status`, `created_at`, `updated_at`) VALUES
('1001', '1002', 'normal', 1710100000000, 1710100000000),
('1002', '1001', 'normal', 1710100000000, 1710100000000),
('1001', '1003', 'normal', 1710100000000, 1710100000000),
('1003', '1001', 'normal', 1710100000000, 1710100000000)
ON DUPLICATE KEY UPDATE status = VALUES(status), updated_at = VALUES(updated_at);
