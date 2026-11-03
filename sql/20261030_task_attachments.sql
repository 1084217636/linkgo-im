USE `linkgo_im`;
-- Older installations lack this column; new installations already create it
-- in 20261028_learning_tasks.sql. The startup script reapplies migrations.
SET @has_attachment_ids = (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'task_submissions'
    AND COLUMN_NAME = 'attachment_ids'
);
SET @ddl = IF(@has_attachment_ids = 0,
  'ALTER TABLE task_submissions ADD COLUMN attachment_ids JSON NULL',
  'SELECT 1');
PREPARE task_attachment_migration FROM @ddl;
EXECUTE task_attachment_migration;
DEALLOCATE PREPARE task_attachment_migration;
UPDATE `task_submissions` SET `attachment_ids`=JSON_ARRAY() WHERE `attachment_ids` IS NULL;
ALTER TABLE `task_submissions` MODIFY `attachment_ids` JSON NOT NULL;
