ALTER TABLE usage_orders DROP CONSTRAINT IF EXISTS chk_usage_orders_kind;
ALTER TABLE usage_orders
    DROP COLUMN IF EXISTS addons,
    DROP COLUMN IF EXISTS target_subscription_id,
    DROP COLUMN IF EXISTS kind;
