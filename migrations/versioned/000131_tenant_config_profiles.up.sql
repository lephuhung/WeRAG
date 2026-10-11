-- Migration: 000131_tenant_config_profiles
-- Platform-wide configuration lives in a configuration profile: a hidden
-- tenant row (kind = 'config_profile') that owns the shared models, agents,
-- sandbox, MCP and web-search rows and the shared tenant-level settings.
-- Every workspace points at one profile through config_profile_id and
-- inherits what it holds; only the SuperAdmin edits a profile.
--
--   - kind: 'workspace' (a real unit with members and data) or
--     'config_profile' (configuration only, never listed or joined).
--   - config_profile_id: the profile a workspace inherits from.
--   - default_embedding_model_id / default_summary_model_id: models a new
--     knowledge base gets when it is created without them (profile rows).

ALTER TABLE tenants ADD COLUMN IF NOT EXISTS kind VARCHAR(16) NOT NULL DEFAULT 'workspace';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS config_profile_id INTEGER NULL;
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS default_embedding_model_id VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS default_summary_model_id VARCHAR(64) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_tenants_kind ON tenants(kind);
CREATE INDEX IF NOT EXISTS idx_tenants_config_profile_id ON tenants(config_profile_id);

-- The default profile every workspace starts on.
INSERT INTO tenants (name, description, business, kind, status, retriever_engines)
SELECT 'Cấu hình chung', 'Platform-wide configuration shared by every workspace', '', 'config_profile', 'active', '{"engines": []}'::jsonb
WHERE NOT EXISTS (SELECT 1 FROM tenants WHERE kind = 'config_profile' AND deleted_at IS NULL);

UPDATE tenants
SET config_profile_id = (
    SELECT id FROM tenants WHERE kind = 'config_profile' AND deleted_at IS NULL ORDER BY id LIMIT 1
)
WHERE kind = 'workspace' AND config_profile_id IS NULL;
