DELETE FROM tenants WHERE kind = 'config_profile';
DROP INDEX IF EXISTS idx_tenants_config_profile_id;
DROP INDEX IF EXISTS idx_tenants_kind;
ALTER TABLE tenants DROP COLUMN IF EXISTS default_summary_model_id;
ALTER TABLE tenants DROP COLUMN IF EXISTS default_embedding_model_id;
ALTER TABLE tenants DROP COLUMN IF EXISTS config_profile_id;
ALTER TABLE tenants DROP COLUMN IF EXISTS kind;
