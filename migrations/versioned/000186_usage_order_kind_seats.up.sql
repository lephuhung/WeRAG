-- Migration: 000186_usage_order_kind_seats
-- Mid-term extra seats (chỗ) are orders of kind 'seats' (usage_billing_seats.go),
-- but chk_usage_orders_kind from 000169 only allowed 'plan' and 'addon', so
-- placing a seats order failed on Postgres with a CHECK violation.
DO $$ BEGIN RAISE NOTICE '[Migration 000186] usage_orders.kind allows seats'; END $$;

ALTER TABLE usage_orders DROP CONSTRAINT IF EXISTS chk_usage_orders_kind;
ALTER TABLE usage_orders
    ADD CONSTRAINT chk_usage_orders_kind CHECK (kind IN ('plan', 'addon', 'seats'));
