-- Migration: 000191_usage_plan_included_agents (down)
-- Subscribers of a plan lose the agents it included; bought add-ons
-- (usage_subscription_addons) are untouched.
DO $$ BEGIN RAISE NOTICE '[Migration 000191 down] drop usage_plans.included_agent_ids'; END $$;

ALTER TABLE usage_plans DROP COLUMN IF EXISTS included_agent_ids;
