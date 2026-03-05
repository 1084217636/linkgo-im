CREATE TABLE IF NOT EXISTS `messages` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `session_id` varchar(255) NOT NULL COMMENT '单聊为sorted_uid_uid，群聊为群ID',
  `from_uid` varchar(255) NOT NULL,
  `to_id` varchar(255) NOT NULL,
  `to_type` varchar(20) NOT NULL COMMENT 'user / group',
  `content` text NOT NULL,
  `create_time` bigint(20) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_session` (`session_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;