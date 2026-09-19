-- Rollback for 000026_tenant_orgs_kb_visibility (sqlite mirror of
-- versioned 000107). SQLite has no DROP COLUMN IF EXISTS on old
-- versions; recent SQLite (>=3.35) supports DROP COLUMN.
ALTER TABLE tenant_invitations DROP COLUMN org_id;
DROP INDEX IF EXISTS idx_knowledge_bases_org;
DROP INDEX IF EXISTS idx_knowledge_bases_visibility;
ALTER TABLE knowledge_bases DROP COLUMN org_id;
ALTER TABLE knowledge_bases DROP COLUMN visibility;
DROP TABLE IF EXISTS tenant_org_members;
DROP TABLE IF EXISTS tenant_orgs;
