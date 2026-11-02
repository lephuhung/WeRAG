DROP INDEX IF EXISTS idx_custom_agents_applies_to_tenant_ids;
ALTER TABLE custom_agents DROP COLUMN IF EXISTS applies_to_tenant_ids;
