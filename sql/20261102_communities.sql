-- Additive migration. Existing groups, messages and tasks are preserved.
USE linkgo_im;
CREATE TABLE IF NOT EXISTS community_profiles (
 group_id VARCHAR(64) PRIMARY KEY, description VARCHAR(1000) NOT NULL DEFAULT '',
 discoverable TINYINT NOT NULL DEFAULT 1,
 FOREIGN KEY (group_id) REFERENCES im_groups(group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS community_posts (
 post_id VARCHAR(64) PRIMARY KEY, group_id VARCHAR(64) NOT NULL, author_id VARCHAR(64) NOT NULL,
 kind ENUM('question','note','resource','announcement') NOT NULL,
 title VARCHAR(160) NOT NULL, body TEXT NOT NULL,
 status ENUM('published','hidden','deleted') NOT NULL DEFAULT 'published',
 pinned TINYINT NOT NULL DEFAULT 0, featured TINYINT NOT NULL DEFAULT 0,
 version BIGINT NOT NULL DEFAULT 1, created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL,
 KEY idx_community_feed(group_id,status,pinned,created_at,post_id),
 FOREIGN KEY (group_id) REFERENCES im_groups(group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS community_comments (
 comment_id VARCHAR(64) PRIMARY KEY, post_id VARCHAR(64) NOT NULL, author_id VARCHAR(64) NOT NULL,
 parent_id VARCHAR(64) NULL, body TEXT NOT NULL, created_at BIGINT NOT NULL,
 KEY idx_post_comments(post_id,created_at,comment_id), FOREIGN KEY(post_id) REFERENCES community_posts(post_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS community_reactions (
 post_id VARCHAR(64) NOT NULL, user_id VARCHAR(64) NOT NULL, created_at BIGINT NOT NULL,
 PRIMARY KEY(post_id,user_id), FOREIGN KEY(post_id) REFERENCES community_posts(post_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS community_post_attachments (
 post_id VARCHAR(64) NOT NULL, attachment_id VARCHAR(64) NOT NULL,
 PRIMARY KEY(post_id,attachment_id), FOREIGN KEY(post_id) REFERENCES community_posts(post_id),
 FOREIGN KEY(attachment_id) REFERENCES attachments(attachment_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS community_requests (
 actor_id VARCHAR(64) NOT NULL, request_id VARCHAR(128) NOT NULL, request_hash CHAR(64) NOT NULL,
 result_json JSON NOT NULL, created_at BIGINT NOT NULL, PRIMARY KEY(actor_id,request_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS community_event_outbox (
 event_id VARCHAR(128) PRIMARY KEY, group_id VARCHAR(64) NOT NULL, actor_id VARCHAR(64) NOT NULL,
 kind VARCHAR(32) NOT NULL, object_id VARCHAR(64) NOT NULL, recipient_id VARCHAR(64) NOT NULL DEFAULT '',
 status ENUM('pending','processing','done','dead') NOT NULL DEFAULT 'pending', attempts INT NOT NULL DEFAULT 0,
 available_at BIGINT NOT NULL, lease_token VARCHAR(64) NOT NULL DEFAULT '', last_error VARCHAR(512) NOT NULL DEFAULT '',
 created_at BIGINT NOT NULL, KEY idx_community_event_ready(status,available_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS community_notifications (
 id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, event_id VARCHAR(128) NOT NULL, recipient_id VARCHAR(64) NOT NULL,
 group_id VARCHAR(64) NOT NULL, kind VARCHAR(32) NOT NULL, object_id VARCHAR(64) NOT NULL,
 created_at BIGINT NOT NULL, read_at BIGINT NULL, UNIQUE KEY uk_notification(event_id,recipient_id),
 KEY idx_notification_user(recipient_id,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
