-- Migration: 000172_guest_chat
-- Guest chat: a Guest (a signed-in account with no active membership in any
-- Tổ chức) chats with the public-lookup agent, which answers only from the
-- platform public knowledge bases.
--
--   1. The public portal: a hidden tenant row (kind = 'public_portal') that
--      guest chat requests run in. It has no members and no usage
--      subscription; Guests are placed in it with no tenant role, on the
--      guest chat routes only. Like an organization it points at the
--      default configuration profile and starts with that profile's
--      inherited settings (engines, web search, parser, retrieval,
--      credentials); later profile saves reach it through
--      SyncProfileSettings.
--   2. The guest plan (code 'guest', fixed id): the personal package every Guest is
--      subscribed to on their first turn (by the usage guard), 10 questions
--      a day. Not sold (inactive, price 0); SuperAdmin edits its limits on
--      the usage page like any other plan. Nothing else is seeded.
--
-- Idempotent: each row is created only when missing.

WITH profile AS (
    SELECT id, retriever_engines, web_search_config, parser_engine_config, retrieval_config, credentials
    FROM tenants
    WHERE kind = 'config_profile' AND deleted_at IS NULL
    ORDER BY id
    LIMIT 1
)
INSERT INTO tenants (name, description, business, kind, status, config_profile_id,
                     retriever_engines, web_search_config, parser_engine_config, retrieval_config, credentials)
SELECT 'Tra cứu công khai',
       'Tenant ẩn của chat khách: tài khoản chưa thuộc Tổ chức nào tra cứu các kho tri thức công khai tại đây. Không có thành viên, không có gói sử dụng.',
       '', 'public_portal', 'active', p.id,
       COALESCE(p.retriever_engines, '{"engines": []}'::jsonb), p.web_search_config, p.parser_engine_config,
       p.retrieval_config, p.credentials
FROM (SELECT 1) AS one
LEFT JOIN profile p ON TRUE
WHERE NOT EXISTS (SELECT 1 FROM tenants WHERE kind = 'public_portal' AND deleted_at IS NULL);

-- The guest plan has a fixed id (types.UsageGuestPlanID): the usage guard
-- finds it by id, and the rollback removes only this row. A plan someone
-- already coded 'guest' is left alone and never taken for it; the seeded row
-- then gets the code 'guest-chat'.
INSERT INTO usage_plans (id, code, name, description, scope_kind, mode, limits, features, price_label, sort_order, is_active)
SELECT '00000000-0000-4000-8000-000000000172',
       CASE WHEN EXISTS (SELECT 1 FROM usage_plans WHERE code = 'guest') THEN 'guest-chat' ELSE 'guest' END,
       'Khách',
       'Gói của tài khoản chưa thuộc Tổ chức nào: tra cứu văn bản công khai, 10 câu hỏi mỗi ngày.',
       'user', 'pool', '[{"metric": "questions", "period": "day", "limit": 10}]'::jsonb, '{}'::jsonb, '', 900, FALSE
WHERE NOT EXISTS (SELECT 1 FROM usage_plans WHERE id = '00000000-0000-4000-8000-000000000172');

DO $$ BEGIN RAISE NOTICE '[Migration 000172] public portal tenant and guest plan present'; END $$;
