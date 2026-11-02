-- Migration: 000166_agent_distribution
-- "Cách phân phối" of custom agents (docs/agent-addon-plans.md §3–§6) and the
-- right to use a specialist ("addon") agent bought with a subscription.
--
--   basic    every Tổ chức (default; every existing row, so nothing changes)
--   addon    only a Tổ chức / person whose current subscription holds a
--            current usage_subscription_addons row for the agent
--   internal the "Tổ chức áp dụng" list (applies_to_tenant_ids; [] = all)
--
-- The list now only means something for internal agents, so a custom row
-- that already carries a non-empty list becomes internal: its visibility is
-- exactly what it was. (On dev: the two eval agents [10000] and "Tư vấn
-- doanh nghiệp nhỏ – thuế" [10003].)
DO $$ BEGIN RAISE NOTICE '[Migration 000166] custom_agents.distribution + usage_subscription_addons'; END $$;

ALTER TABLE custom_agents
    ADD COLUMN IF NOT EXISTS distribution VARCHAR(16) NOT NULL DEFAULT 'basic';

DO $$ BEGIN
    ALTER TABLE custom_agents ADD CONSTRAINT chk_custom_agents_distribution
        CHECK (distribution IN ('basic', 'addon', 'internal'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

UPDATE custom_agents
   SET distribution = 'internal'
 WHERE is_builtin = FALSE
   AND distribution = 'basic'
   AND COALESCE(applies_to_tenant_ids, '[]'::jsonb) <> '[]'::jsonb;

-- One row per (subscription, agent); a renewal or mid-term purchase moves
-- ends_at. ends_at NULL = until the subscription itself ends. agent_id has no
-- foreign key: custom_agents is keyed by (id, tenant_id) and a deleted agent
-- simply stops matching.
CREATE TABLE IF NOT EXISTS usage_subscription_addons (
    subscription_id VARCHAR(36) NOT NULL REFERENCES usage_subscriptions(id) ON DELETE CASCADE,
    agent_id        VARCHAR(36) NOT NULL,
    starts_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    ends_at         TIMESTAMPTZ,
    price_vnd       BIGINT      NOT NULL DEFAULT 0,
    order_id        VARCHAR(36) REFERENCES usage_orders(id) ON DELETE SET NULL,
    granted_by      VARCHAR(36) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (subscription_id, agent_id)
);

-- The visibility check (agent list, every chat turn) is an EXISTS correlated
-- on agent_id, then the subscription by primary key.
CREATE INDEX IF NOT EXISTS idx_usage_subscription_addons_agent
    ON usage_subscription_addons (agent_id, subscription_id);
CREATE INDEX IF NOT EXISTS idx_usage_subscription_addons_order
    ON usage_subscription_addons (order_id) WHERE order_id IS NOT NULL;
