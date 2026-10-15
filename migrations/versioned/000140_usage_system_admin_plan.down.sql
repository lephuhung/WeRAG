DELETE FROM usage_subscriptions
WHERE plan_id IN (SELECT id FROM usage_plans WHERE code = 'system-admin');
DELETE FROM usage_plans WHERE code = 'system-admin';
