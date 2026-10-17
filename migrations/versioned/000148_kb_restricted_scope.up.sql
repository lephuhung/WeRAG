-- Tenant/KB permissions, phase 3 (2026-10-17).
--
-- 1. visibility = 'restricted': a tenant-owned KB (owner_tenant_id > 0)
--    that only ACL holders read (kb_invitations / kb_access_grants), plus
--    the owner tenant's Admins and SuperAdmins. The owner/visibility CHECK
--    from 000119 is extended; the invariant itself is unchanged
--    (public <=> owner 0, every other scope <=> owner > 0).
-- 2. knowledge_bases.member_default: what an active member of the owner
--    tenant may do on a 'tenant' KB: 'view' or 'contribute' (view +
--    upload + edit/delete own documents). Defaults to today's behaviour.
-- 3. kb_invitations: drop chk_kb_invites_no_self_grant. Same-tenant
--    invites are the per-user ACL of restricted KBs.
--
-- Idempotent: every statement can run twice.

DO $$ BEGIN RAISE NOTICE '[Migration 000148] Allowing restricted visibility'; END $$;

ALTER TABLE knowledge_bases
    DROP CONSTRAINT IF EXISTS chk_knowledge_bases_owner_visibility;

ALTER TABLE knowledge_bases
    ADD CONSTRAINT chk_knowledge_bases_owner_visibility CHECK (
        (visibility = 'public' AND owner_tenant_id = 0)
        OR (visibility IN ('tenant', 'published', 'restricted') AND owner_tenant_id > 0)
    );

DO $$ BEGIN RAISE NOTICE '[Migration 000148] Adding knowledge_bases.member_default'; END $$;

ALTER TABLE knowledge_bases
    ADD COLUMN IF NOT EXISTS member_default VARCHAR(16) NOT NULL DEFAULT 'contribute';

ALTER TABLE knowledge_bases
    DROP CONSTRAINT IF EXISTS chk_knowledge_bases_member_default;

ALTER TABLE knowledge_bases
    ADD CONSTRAINT chk_knowledge_bases_member_default CHECK (
        member_default IN ('view', 'contribute')
    );

DO $$ BEGIN RAISE NOTICE '[Migration 000148] Allowing same-tenant KB invitations'; END $$;

ALTER TABLE kb_invitations
    DROP CONSTRAINT IF EXISTS chk_kb_invites_no_self_grant;
