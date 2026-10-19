-- Migration: 000171_usage_ends_vn_midnight
-- Billing calendar (docs/agent-addon-plans.md §7.8): a day ends at 24:00
-- Vietnam time, and subscription / add-on ends are stored as that instant
-- (00:00 VN of the next day, in UTC). Rows written before the rule keep an
-- end at the hour of purchase; round the ones still running up to the next
-- 24:00 VN so day counting (CalendarDaysVN) never charges for a day the
-- right does not reach. Expired and open-ended rows are left alone; rows
-- already on a VN midnight are unchanged (idempotent).
DO $$ BEGIN RAISE NOTICE '[Migration 000171] round running usage ends up to 24:00 Vietnam time'; END $$;

UPDATE usage_subscriptions
   SET ends_at = (date_trunc('day', ends_at AT TIME ZONE 'Asia/Ho_Chi_Minh') + interval '1 day')
                 AT TIME ZONE 'Asia/Ho_Chi_Minh',
       updated_at = now()
 WHERE status = 'active'
   AND ends_at IS NOT NULL
   AND ends_at > now()
   AND ends_at <> date_trunc('day', ends_at AT TIME ZONE 'Asia/Ho_Chi_Minh') AT TIME ZONE 'Asia/Ho_Chi_Minh';

UPDATE usage_subscription_addons
   SET ends_at = (date_trunc('day', ends_at AT TIME ZONE 'Asia/Ho_Chi_Minh') + interval '1 day')
                 AT TIME ZONE 'Asia/Ho_Chi_Minh',
       updated_at = now()
 WHERE ends_at IS NOT NULL
   AND ends_at > now()
   AND ends_at <> date_trunc('day', ends_at AT TIME ZONE 'Asia/Ho_Chi_Minh') AT TIME ZONE 'Asia/Ho_Chi_Minh';
