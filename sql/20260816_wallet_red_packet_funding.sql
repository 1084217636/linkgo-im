CREATE TABLE IF NOT EXISTS `wallet_accounts` (
    `user_id` VARCHAR(64) NOT NULL COMMENT '用户ID',
    `available_balance` BIGINT NOT NULL DEFAULT 0 COMMENT '可用余额，单位分',
    `updated_at` BIGINT NOT NULL,
    PRIMARY KEY (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='站内钱包账户';

CREATE TABLE IF NOT EXISTS `wallet_ledger` (
    `id` VARCHAR(64) NOT NULL COMMENT '流水ID',
    `user_id` VARCHAR(64) NOT NULL,
    `direction` ENUM('credit', 'debit') NOT NULL COMMENT '入账或出账',
    `amount` BIGINT NOT NULL COMMENT '变动金额，单位分',
    `business_type` ENUM('mock_recharge', 'red_packet_send', 'red_packet_claim', 'red_packet_refund') NOT NULL,
    `business_id` VARCHAR(128) NOT NULL COMMENT '充值请求或红包ID',
    `balance_after` BIGINT NOT NULL COMMENT '变动后余额，单位分',
    `created_at` BIGINT NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_wallet_business` (`user_id`, `business_type`, `business_id`),
    INDEX `idx_wallet_ledger_user_time` (`user_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='站内钱包不可变流水';

CREATE TABLE IF NOT EXISTS `mock_payment_orders` (
    `id` VARCHAR(64) NOT NULL COMMENT '模拟支付订单ID',
    `request_id` VARCHAR(128) NOT NULL COMMENT '客户端充值幂等ID',
    `user_id` VARCHAR(64) NOT NULL,
    `channel` ENUM('mock_wechat', 'mock_alipay') NOT NULL,
    `amount` BIGINT NOT NULL COMMENT '充值金额，单位分',
    `status` ENUM('paid') NOT NULL DEFAULT 'paid',
    `provider_trade_no` VARCHAR(128) NOT NULL COMMENT '模拟渠道交易号',
    `created_at` BIGINT NOT NULL,
    `paid_at` BIGINT NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_mock_payment_request` (`user_id`, `request_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='仅用于本地演示的模拟充值订单';
