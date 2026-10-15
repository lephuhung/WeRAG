-- Usage packages: SuperAdmins hold a package like everyone else, so every
-- client (web, Word add-in) shows one instead of "no package".
--
-- The plan is personal, unlimited (no limits) and not sold (price 0,
-- inactive so it stays out of the purchase lists). Every current SuperAdmin
-- gets an active subscription to it; one promoted later gets it from the
-- usage guard on the first lookup. SuperAdmin can rename the plan or add
-- limits to it like any other.

INSERT INTO usage_plans (id, code, name, description, scope_kind, mode, limits, features, price_label, sort_order, is_active)
SELECT uuid_generate_v4()::text, 'system-admin', 'Quản trị hệ thống',
       'Gói của SuperAdmin: không giới hạn câu hỏi và token.',
       'user', 'pool', '[]'::jsonb, '{}'::jsonb, '', 1000, FALSE
WHERE NOT EXISTS (SELECT 1 FROM usage_plans WHERE code = 'system-admin');

INSERT INTO usage_subscriptions (id, scope_type, scope_id, plan_id, mode, seats, starts_at, status, note)
SELECT uuid_generate_v4()::text, 'user', u.id, p.id, 'pool', 0, now(), 'active', 'SuperAdmin'
FROM users u
JOIN usage_plans p ON p.code = 'system-admin'
WHERE u.is_system_admin = TRUE
  AND u.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM usage_subscriptions s
      WHERE s.scope_type = 'user' AND s.scope_id = u.id AND s.status = 'active'
  );
