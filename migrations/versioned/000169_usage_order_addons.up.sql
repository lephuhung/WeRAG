-- Migration: 000169_usage_order_addons
-- Specialist agents bought with a plan or on a running subscription
-- (docs/agent-addon-plans.md §5, phase C).
--   kind                    'plan' (a plan, optional add-ons) | 'addon'
--                           (add-ons on the current subscription)
--   target_subscription_id  the subscription an 'addon' order is for
--   addons                  add-on lines with prices frozen at order time
-- Existing orders are plan orders without add-ons.
DO $$ BEGIN RAISE NOTICE '[Migration 000169] usage_orders add-on lines'; END $$;

ALTER TABLE usage_orders
    ADD COLUMN IF NOT EXISTS kind VARCHAR(16) NOT NULL DEFAULT 'plan',
    ADD COLUMN IF NOT EXISTS target_subscription_id VARCHAR(36),
    ADD COLUMN IF NOT EXISTS addons JSONB NOT NULL DEFAULT '[]'::jsonb;

DO $$ BEGIN
    ALTER TABLE usage_orders ADD CONSTRAINT chk_usage_orders_kind CHECK (kind IN ('plan', 'addon'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
