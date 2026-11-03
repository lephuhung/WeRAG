-- Migration: 000180_usage_order_plan_change_credit
-- A plan change credits the unused paid days of the replaced subscription
-- (and of the add-ons the buyer drops) against the order; the order keeps
-- the credit lines and the subscription it was priced against.
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS credit_vnd BIGINT NOT NULL DEFAULT 0;
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS credit_lines JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS replaces_subscription_id VARCHAR(36);
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS replaces_ends_at TIMESTAMPTZ;
