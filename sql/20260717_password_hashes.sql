-- Existing installations can apply this migration directly.
-- The login path also upgrades any other legacy plaintext password after a
-- successful comparison, using an optimistic WHERE password = ? update.
UPDATE `users`
SET `password` = '$2b$10$msHwvw.T/fpIilP9oGc3GuIkXKv1m1HtGzWkU.UHzFaEoj.r83SvK'
WHERE `username` IN ('userA', 'userB', 'userC') AND `password` = '123456';

UPDATE `users`
SET `password` = '$2b$10$THQMtV0aOUVUUUoRC7Sj.unXkjj2.DEKIFQ9nuPj86yaqIc9AbB0q'
WHERE `username` = 'ai_assistant' AND `password` = 'bot-only';
