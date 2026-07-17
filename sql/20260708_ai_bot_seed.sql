INSERT INTO `users` (`user_id`, `username`, `password`, `created_at`, `updated_at`) VALUES
('9001', 'ai_assistant', '$2b$10$THQMtV0aOUVUUUoRC7Sj.unXkjj2.DEKIFQ9nuPj86yaqIc9AbB0q', 1710100000000, 1710100000000)
ON DUPLICATE KEY UPDATE
  username = VALUES(username),
  password = VALUES(password),
  updated_at = VALUES(updated_at);

INSERT INTO `friend_relations` (`user_id`, `friend_id`, `status`, `created_at`, `updated_at`) VALUES
('1001', '9001', 'normal', 1710100000000, 1710100000000),
('9001', '1001', 'normal', 1710100000000, 1710100000000),
('1002', '9001', 'normal', 1710100000000, 1710100000000),
('9001', '1002', 'normal', 1710100000000, 1710100000000),
('1003', '9001', 'normal', 1710100000000, 1710100000000),
('9001', '1003', 'normal', 1710100000000, 1710100000000)
ON DUPLICATE KEY UPDATE
  status = VALUES(status),
  updated_at = VALUES(updated_at);
