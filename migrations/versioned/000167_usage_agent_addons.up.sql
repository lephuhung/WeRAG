-- Migration: 000167_usage_agent_addons
-- Catalogue of specialist ("addon") agents sold with service plans
-- (docs/agent-addon-plans.md §4, phase B). Runtime data a SuperAdmin edits;
-- nothing is seeded (same rule as usage_plans).
--   price_vnd   per month; × seats for a seat-mode plan
--   plan_ids    the plans it is sold with; [] = every plan
-- Only an agent whose distribution is 'addon' may be listed; an agent
-- deleted or converted away from addon has its row deactivated (and is never
-- offered while it is not a live addon agent).
DO $$ BEGIN RAISE NOTICE '[Migration 000167] usage_agent_addons'; END $$;

CREATE TABLE IF NOT EXISTS usage_agent_addons (
    agent_id    VARCHAR(36) PRIMARY KEY,
    price_vnd   BIGINT      NOT NULL DEFAULT 0 CHECK (price_vnd >= 0),
    plan_ids    JSONB       NOT NULL DEFAULT '[]'::jsonb,
    description TEXT        NOT NULL DEFAULT '',
    is_active   BOOLEAN     NOT NULL DEFAULT TRUE,
    sort_order  INTEGER     NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_usage_agent_addons_active
    ON usage_agent_addons (sort_order, agent_id) WHERE is_active;
