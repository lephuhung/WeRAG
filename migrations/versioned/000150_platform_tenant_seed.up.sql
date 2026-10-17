-- Platform tenant seed.
--
-- Tenant 10000 is the platform's home: built-in models are catalogued
-- under tenant_id 10000 (types.DefaultBuiltinModelTenantID) and the
-- platform-wide public knowledge bases (laws, decrees, ...) are created
-- there before a SuperAdmin promotes them to visibility 'public'
-- (owner_tenant_id 0). Until now the row only existed because the
-- tenants_id_seq starts at 10000 and the first registered user happened
-- to receive it. A fresh install seeds it explicitly so the id and name
-- survive reinstalls. Existing databases keep their row untouched.
INSERT INTO tenants (id, name, description, status, kind, retriever_engines)
SELECT 10000, 'SuperAdmin',
       'Tenant nền tảng: nơi SuperAdmin tạo và nuôi các KB công khai (Luật, Nghị định, ...) và chứa catalog model dùng chung.',
       'active', 'workspace', '[]'::jsonb
WHERE NOT EXISTS (SELECT 1 FROM tenants WHERE id = 10000);

-- Keep the sequence past the seeded id so the first self-registered
-- workspace does not collide with it.
SELECT setval('tenants_id_seq', GREATEST((SELECT last_value FROM tenants_id_seq), 10000), true);

DO $$ BEGIN RAISE NOTICE '[Migration 000150] platform tenant 10000 (SuperAdmin) present'; END $$;
