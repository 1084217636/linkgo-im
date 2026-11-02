USE `linkgo_im`;

CREATE TABLE IF NOT EXISTS `group_tasks` (
  `task_id` VARCHAR(64) NOT NULL,
  `group_id` VARCHAR(64) NOT NULL,
  `title` VARCHAR(120) NOT NULL,
  `description` TEXT NOT NULL,
  `due_at` BIGINT NULL,
  `status` ENUM('draft','published','closed') NOT NULL DEFAULT 'draft',
  `version` BIGINT NOT NULL DEFAULT 1,
  `created_by` VARCHAR(64) NOT NULL,
  `published_at` BIGINT NULL,
  `closed_at` BIGINT NULL,
  `created_at` BIGINT NOT NULL,
  `updated_at` BIGINT NOT NULL,
  PRIMARY KEY (`task_id`),
  KEY `idx_group_tasks_list` (`group_id`,`status`,`created_at`,`task_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `task_submissions` (
  `submission_id` VARCHAR(64) NOT NULL,
  `task_id` VARCHAR(64) NOT NULL,
  `user_id` VARCHAR(64) NOT NULL,
  `content_text` TEXT NOT NULL,
  `content_link` VARCHAR(2048) NOT NULL DEFAULT '',
  `attachment_ids` JSON NULL,
  `status` ENUM('submitted','approved','returned') NOT NULL,
  `attempt_no` INT NOT NULL DEFAULT 1,
  `version` BIGINT NOT NULL DEFAULT 1,
  `reviewer_id` VARCHAR(64) NULL,
  `review_note` VARCHAR(1000) NOT NULL DEFAULT '',
  `submitted_at` BIGINT NOT NULL,
  `reviewed_at` BIGINT NULL,
  PRIMARY KEY (`submission_id`),
  UNIQUE KEY `uk_task_submitter` (`task_id`,`user_id`),
  KEY `idx_task_submission_status` (`task_id`,`status`,`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
UPDATE `task_submissions` SET `attachment_ids`=JSON_ARRAY() WHERE `attachment_ids` IS NULL;
ALTER TABLE `task_submissions` MODIFY `attachment_ids` JSON NOT NULL;

CREATE TABLE IF NOT EXISTS `task_events` (
  `event_id` VARCHAR(64) NOT NULL,
  `task_id` VARCHAR(64) NOT NULL,
  `submission_id` VARCHAR(64) NULL,
  `actor_uid` VARCHAR(64) NOT NULL,
  `action` VARCHAR(32) NOT NULL,
  `request_id` VARCHAR(128) NOT NULL,
  `request_hash` CHAR(64) NOT NULL,
  `payload_json` JSON NOT NULL,
  `result_json` JSON NOT NULL,
  `created_at` BIGINT NOT NULL,
  PRIMARY KEY (`event_id`),
  UNIQUE KEY `uk_task_request` (`actor_uid`,`request_id`),
  KEY `idx_task_events_task` (`task_id`,`created_at`,`event_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `task_notification_outbox` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `event_id` VARCHAR(64) NOT NULL,
  `target_type` ENUM('group','user') NOT NULL,
  `target_id` VARCHAR(64) NOT NULL,
  `status` ENUM('pending','processing','done','dead','skipped') NOT NULL DEFAULT 'pending',
  `attempts` INT NOT NULL DEFAULT 0,
  `available_at` BIGINT NOT NULL,
  `im_message_id` VARCHAR(160) NULL,
  `last_error` VARCHAR(512) NOT NULL DEFAULT '',
  `created_at` BIGINT NOT NULL,
  `updated_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_task_notification` (`event_id`,`target_type`,`target_id`),
  KEY `idx_task_notification_ready` (`status`,`available_at`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- A disabled, reserved sender for internal task notifications. It cannot log in.
INSERT IGNORE INTO `users` (`user_id`,`username`,`password`,`status`,`created_at`,`updated_at`)
VALUES ('9002','task_notifier','!disabled',0,0,0);
