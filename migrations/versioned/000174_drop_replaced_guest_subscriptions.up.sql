-- Migration: 000174_drop_replaced_guest_subscriptions
-- The guest plan (fixed id from 000172) is no package a person chose: when
-- they moved to a bought or assigned package its row was closed as
-- 'cancelled' and showed in their history as a cancelled guest package.
-- Plan changes now delete it; this removes the rows left from before.
DELETE FROM usage_subscriptions
WHERE plan_id = '00000000-0000-4000-8000-000000000172'
  AND status = 'cancelled';
