-- Migration: 000187_usage_order_result_ends
-- A paid order keeps the period it bought, set when it is confirmed:
-- result_ends_at is the end the order produced (the receipt shows it instead
-- of the package's current end, which later renewals move), period_starts_at
-- where that bought time starts (a renewal: the old end). The plan-change
-- credit values each paid order by the unused share of that period, so it
-- never gives back more than was paid. NULL on older orders.
DO $$ BEGIN RAISE NOTICE '[Migration 000187] usage_orders.period_starts_at, result_ends_at'; END $$;

ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS period_starts_at TIMESTAMPTZ;
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS result_ends_at TIMESTAMPTZ;
