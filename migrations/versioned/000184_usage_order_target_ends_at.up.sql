-- Migration: 000184_usage_order_target_ends_at
-- An order for extra seats (kind "seats") on the current seat subscription
-- keeps the subscription end it was priced to: confirming it after the
-- subscription was renewed or replaced is refused.
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS target_ends_at TIMESTAMPTZ;
