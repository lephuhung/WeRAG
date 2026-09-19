-- Rollback for 000107_tenant_orgs_kb_visibility.
ALTER TABLE tenant_invitations DROP COLUMN IF EXISTS org_id;
ALTER TABLE knowledge_bases DROP COLUMN IF EXISTS org_id;
ALTER TABLE knowledge_bases DROP COLUMN IF EXISTS visibility;
DROP TABLE IF EXISTS tenant_org_members;
DROP TABLE IF EXISTS tenant_orgs;
