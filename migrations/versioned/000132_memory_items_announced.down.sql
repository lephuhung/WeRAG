DROP INDEX IF EXISTS idx_memory_items_unannounced;
ALTER TABLE memory_items DROP COLUMN IF EXISTS announced_at;
