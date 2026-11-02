-- Migration: 000176_usage_plan_term_prices
-- Quarterly and yearly packages: a plan may carry a price for a quarter (3
-- months) and for a year (12 months), per seat in seat mode. 0 = that term
-- is sold at the monthly price (or not at all when price_vnd is 0 too).
-- An order records which term its plan line was priced by.
ALTER TABLE usage_plans ADD COLUMN IF NOT EXISTS price_quarter_vnd BIGINT NOT NULL DEFAULT 0;
ALTER TABLE usage_plans ADD COLUMN IF NOT EXISTS price_year_vnd BIGINT NOT NULL DEFAULT 0;
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS billing_term VARCHAR(16) NOT NULL DEFAULT '';
UPDATE usage_orders SET billing_term = 'month' WHERE billing_term = '' AND kind = 'plan';
