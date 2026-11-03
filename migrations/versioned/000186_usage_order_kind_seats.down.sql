-- Back to the 000169 kinds. Existing 'seats' orders are kept untouched (they
-- are paid history and their payments point at them), so the narrower
-- constraint is added NOT VALID: it holds for new and updated rows only and
-- the rollback never fails on, or deletes, seats rows already stored.
ALTER TABLE usage_orders DROP CONSTRAINT IF EXISTS chk_usage_orders_kind;
ALTER TABLE usage_orders
    ADD CONSTRAINT chk_usage_orders_kind CHECK (kind IN ('plan', 'addon')) NOT VALID;
