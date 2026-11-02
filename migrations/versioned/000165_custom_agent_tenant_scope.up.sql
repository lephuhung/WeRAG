-- Migration: 000165_custom_agent_tenant_scope
-- "Tổ chức áp dụng": the workspaces (Tổ chức) allowed to use a custom agent
-- they inherit from a configuration profile, as a JSON array of tenant ids.
-- [] (the default, and every existing row) = every workspace on the profile,
-- so nothing changes for agents saved before this column. Built-in agents
-- are never scoped (the API refuses a list for them).
DO $$ BEGIN RAISE NOTICE '[Migration 000165] Adding custom_agents.applies_to_tenant_ids'; END $$;

ALTER TABLE custom_agents
    ADD COLUMN IF NOT EXISTS applies_to_tenant_ids JSONB NOT NULL DEFAULT '[]'::jsonb;

-- The visibility check is "[] or contains this tenant" (jsonb @>); GIN keeps
-- it cheap even though the table is small today.
CREATE INDEX IF NOT EXISTS idx_custom_agents_applies_to_tenant_ids
    ON custom_agents USING GIN (applies_to_tenant_ids);
