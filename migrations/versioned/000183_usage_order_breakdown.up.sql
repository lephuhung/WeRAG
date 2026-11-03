-- Migration: 000183_usage_order_breakdown
-- An order keeps its quote's breakdown so the order page adds up: the
-- prorated extra seats (amount and days) and what the quarter/year term
-- saved.
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS proration_vnd BIGINT NOT NULL DEFAULT 0;
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS prorated_days INTEGER NOT NULL DEFAULT 0;
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS savings_vnd BIGINT NOT NULL DEFAULT 0;
