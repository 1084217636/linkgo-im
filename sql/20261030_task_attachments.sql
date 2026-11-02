USE `linkgo_im`;
ALTER TABLE `task_submissions` ADD COLUMN `attachment_ids` JSON NULL;
UPDATE `task_submissions` SET `attachment_ids`=JSON_ARRAY() WHERE `attachment_ids` IS NULL;
ALTER TABLE `task_submissions` MODIFY `attachment_ids` JSON NOT NULL;
