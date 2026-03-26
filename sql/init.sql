-- 1. 创建数据库（如果不存在）
CREATE DATABASE IF NOT EXISTS `linkgo_im` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;
USE `linkgo_im`;

-- 2. 用户表：增加索引优化和默认值
CREATE TABLE IF NOT EXISTS `users` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '自增主键',
    `user_id` VARCHAR(64) NOT NULL UNIQUE COMMENT '对外公开的唯一用户标识',
    `username` VARCHAR(32) NOT NULL UNIQUE COMMENT '登录名',
    `password` VARCHAR(128) NOT NULL COMMENT '密码哈希值（实验期存明文）',
    `avatar` VARCHAR(255) DEFAULT '' COMMENT '头像地址',
    `status` TINYINT(1) DEFAULT 1 COMMENT '状态: 1正常 0禁用',
    `created_at` BIGINT NOT NULL COMMENT '毫秒级创建时间',
    `updated_at` BIGINT NOT NULL COMMENT '毫秒级更新时间',
    PRIMARY KEY (`id`),
    INDEX `idx_user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户基础信息表';

-- 3. 消息表：针对 IM 场景优化的索引设计
CREATE TABLE IF NOT EXISTS `messages` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `message_id` VARCHAR(160) NOT NULL COMMENT '消息唯一标识: session_id + seq',
    `session_id` VARCHAR(128) NOT NULL COMMENT '会话ID: 单聊(sorted_uid_uid), 群聊(G_id)',
    `seq` BIGINT NOT NULL COMMENT '会话内单调递增序列，用于乱序保护与补偿同步',
    `from_uid` VARCHAR(64) NOT NULL COMMENT '发送者ID',
    `to_id` VARCHAR(64) NOT NULL COMMENT '接收者ID(用户ID或群ID)',
    `to_type` ENUM('user', 'group') NOT NULL DEFAULT 'user' COMMENT '接收类型',
    `content` TEXT NOT NULL COMMENT '消息内容',
    `msg_type` TINYINT(1) DEFAULT 1 COMMENT '消息类型: 1文字 2图片 3语音',
    `create_time` BIGINT NOT NULL COMMENT '毫秒级时间戳',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_message_id` (`message_id`),
    -- 核心索引：历史记录查询全靠它
    INDEX `idx_session_seq` (`session_id`, `seq`),
    INDEX `idx_session_time` (`session_id`, `create_time`),
    INDEX `idx_from_uid` (`from_uid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='聊天消息持久化表';

-- 4. 预置实验账号（create_time 随便填的当前毫秒值）
INSERT INTO `users` (`user_id`, `username`, `password`, `created_at`, `updated_at`) VALUES 
('1001', 'userA', '123456', 1710100000000, 1710100000000), 
('1002', 'userB', '123456', 1710100000000, 1710100000000), 
('1003', 'userC', '123456', 1710100000000, 1710100000000);
-- CREATE TABLE IF NOT EXISTS `messages` (
--   `id` int(11) NOT NULL AUTO_INCREMENT,
--   `session_id` varchar(255) NOT NULL COMMENT '单聊为sorted_uid_uid，群聊为群ID',
--   `from_uid` varchar(255) NOT NULL,
--   `to_id` varchar(255) NOT NULL,
--   `to_type` varchar(20) NOT NULL COMMENT 'user / group',
--   `content` text NOT NULL,
--   `create_time` bigint(20) NOT NULL,
--   PRIMARY KEY (`id`),
--   KEY `idx_session` (`session_id`)
-- ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
