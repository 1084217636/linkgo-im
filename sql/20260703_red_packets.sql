CREATE TABLE IF NOT EXISTS `red_packets` (
    `id` VARCHAR(64) NOT NULL COMMENT '红包ID',
    `sender_id` VARCHAR(64) NOT NULL COMMENT '发红包用户',
    `conversation_id` VARCHAR(128) NOT NULL COMMENT '所属会话',
    `to_type` ENUM('user', 'group') NOT NULL DEFAULT 'user' COMMENT '会话类型',
    `total_amount` BIGINT NOT NULL COMMENT '总金额，单位分',
    `total_count` INT NOT NULL COMMENT '总份数',
    `remaining_amount` BIGINT NOT NULL COMMENT '剩余金额，单位分',
    `remaining_count` INT NOT NULL COMMENT '剩余份数',
    `greeting` VARCHAR(255) NOT NULL DEFAULT '' COMMENT '祝福语',
    `status` ENUM('active', 'finished', 'expired') NOT NULL DEFAULT 'active',
    `created_at` BIGINT NOT NULL,
    `expires_at` BIGINT NOT NULL,
    `updated_at` BIGINT NOT NULL,
    PRIMARY KEY (`id`),
    INDEX `idx_red_packet_conversation` (`conversation_id`, `created_at`),
    INDEX `idx_red_packet_sender` (`sender_id`, `created_at`),
    INDEX `idx_red_packet_status_expire` (`status`, `expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='IM红包主表';

CREATE TABLE IF NOT EXISTS `red_packet_claims` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `red_packet_id` VARCHAR(64) NOT NULL,
    `user_id` VARCHAR(64) NOT NULL,
    `amount` BIGINT NOT NULL COMMENT '抢到金额，单位分',
    `created_at` BIGINT NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_red_packet_user` (`red_packet_id`, `user_id`),
    INDEX `idx_red_packet_claim_time` (`red_packet_id`, `created_at`),
    INDEX `idx_red_packet_claim_user` (`user_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='IM红包领取记录';
