-- Migration: 000168_agent_enabled_switch
-- The SuperAdmin's runtime on/off switch for agents (built-in and custom).
-- NULL = the default: builtin_agents.yaml `enabled` for a built-in, on for a
-- custom agent; so every existing row keeps its behaviour. For a built-in
-- the switch lives on its row in the configuration profile (created on the
-- first toggle, like any other built-in customisation).
DO $$ BEGIN RAISE NOTICE '[Migration 000168] custom_agents.enabled'; END $$;

ALTER TABLE custom_agents ADD COLUMN IF NOT EXISTS enabled BOOLEAN;
