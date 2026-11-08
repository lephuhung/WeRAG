-- Migration: 000191_usage_plan_included_agents
-- A service plan may include specific specialist ("addon" distribution)
-- agents: anyone with a current active subscription to the plan sees and uses
-- them with no purchase (docs/agent-addon-plans.md, user decision 08/11/2026).
-- The list holds custom_agents ids; the API only accepts live addon agents.
-- [] = the plan includes only the basic agents, as before.
DO $$ BEGIN RAISE NOTICE '[Migration 000191] usage_plans.included_agent_ids'; END $$;

ALTER TABLE usage_plans
    ADD COLUMN IF NOT EXISTS included_agent_ids jsonb NOT NULL DEFAULT '[]'::jsonb;
