DROP TABLE IF EXISTS kb_subscriptions;
DROP INDEX IF EXISTS idx_knowledges_created_by;
ALTER TABLE knowledges DROP COLUMN created_by;
