ALTER TABLE usage_orders DROP COLUMN IF EXISTS replaces_ends_at;
ALTER TABLE usage_orders DROP COLUMN IF EXISTS replaces_subscription_id;
ALTER TABLE usage_orders DROP COLUMN IF EXISTS credit_lines;
ALTER TABLE usage_orders DROP COLUMN IF EXISTS credit_vnd;
