ALTER TABLE messages DROP COLUMN IF EXISTS usage_metered_at;
DROP INDEX IF EXISTS idx_usage_counters_period;
DROP TABLE IF EXISTS usage_counters;
