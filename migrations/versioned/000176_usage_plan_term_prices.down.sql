ALTER TABLE usage_orders DROP COLUMN IF EXISTS billing_term;
ALTER TABLE usage_plans DROP COLUMN IF EXISTS price_year_vnd;
ALTER TABLE usage_plans DROP COLUMN IF EXISTS price_quarter_vnd;
