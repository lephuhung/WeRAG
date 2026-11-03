-- Migration: 000184_usage_order_target_ends_at
-- An order for extra seats (kind "seats") on the current seat subscription
-- keeps the subscription end it was priced to: confirming it after the
-- subscription was renewed or replaced is refused.
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS target_ends_at TIMESTAMPTZ;
-- A plan order (renewal, plan change) keeps the seat count of the current
-- seat subscription it was priced against: confirming it after a seat
-- order added seats is refused (the seats would be paid twice, or the
-- credit would miss them). NULL = not priced against seats.
ALTER TABLE usage_orders ADD COLUMN IF NOT EXISTS priced_seats INTEGER;
