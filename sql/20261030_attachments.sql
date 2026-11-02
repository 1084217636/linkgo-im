USE `linkgo_im`;

CREATE TABLE IF NOT EXISTS `attachments` (
    `attachment_id` VARCHAR(64) NOT NULL,
    `group_id` VARCHAR(64) NOT NULL,
    `uploader_id` VARCHAR(64) NOT NULL,
    `file_name` VARCHAR(200) NOT NULL,
    `mime_type` VARCHAR(128) NOT NULL,
    `file_size` BIGINT UNSIGNED NOT NULL,
    `sha256` CHAR(64) NOT NULL,
    `storage_key` VARCHAR(128) NOT NULL,
    `status` ENUM('ready','deleted') NOT NULL DEFAULT 'ready',
    `created_at` BIGINT NOT NULL,
    PRIMARY KEY (`attachment_id`),
    UNIQUE KEY `uk_attachment_storage_key` (`storage_key`),
    KEY `idx_attachment_group` (`group_id`,`status`,`created_at`),
    KEY `idx_attachment_uploader` (`uploader_id`,`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='群聊附件元数据；文件字节保存在共享文件卷';
