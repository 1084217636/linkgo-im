USE `linkgo_im`;

-- Existing installations need an additive column. Run safely on repeated starts.
SET @has_trace_id = (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'delivery_outbox' AND COLUMN_NAME = 'trace_id'
);
SET @ddl = IF(@has_trace_id = 0,
  'ALTER TABLE delivery_outbox ADD COLUMN trace_id VARCHAR(128) NOT NULL DEFAULT '''' AFTER message_id',
  'SELECT 1');
PREPARE trace_migration FROM @ddl;
EXECUTE trace_migration;
DEALLOCATE PREPARE trace_migration;
