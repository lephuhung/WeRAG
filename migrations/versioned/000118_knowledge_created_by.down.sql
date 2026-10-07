-- Rollback for 000118_knowledge_created_by. Members lose edit/delete on
-- their own uploads; Tenant Admins are unaffected.

DROP INDEX IF EXISTS idx_knowledges_created_by;
ALTER TABLE knowledges DROP COLUMN IF EXISTS created_by;
