-- Rollback for 000148_kb_restricted_scope.
--
-- Restricted KBs fall back to 'tenant' visibility (every member of the
-- owner tenant reads again) so the old owner/visibility CHECK holds.
UPDATE knowledge_bases SET visibility = 'tenant', updated_at = CURRENT_TIMESTAMP
WHERE visibility = 'restricted';

ALTER TABLE knowledge_bases
    DROP CONSTRAINT IF EXISTS chk_knowledge_bases_owner_visibility;

ALTER TABLE knowledge_bases
    ADD CONSTRAINT chk_knowledge_bases_owner_visibility CHECK (
        (visibility = 'public' AND owner_tenant_id = 0)
        OR (visibility IN ('tenant', 'published') AND owner_tenant_id > 0)
    );

ALTER TABLE knowledge_bases
    DROP CONSTRAINT IF EXISTS chk_knowledge_bases_member_default;

ALTER TABLE knowledge_bases DROP COLUMN IF EXISTS member_default;

-- Same-tenant invitations issued while 000148 was applied would violate
-- the restored CHECK, so it comes back NOT VALID: existing rows are not
-- re-checked (they stay; under the pre-000148 rules a same-tenant member
-- already reads every tenant KB, so they grant nothing extra), while every
-- new or updated row must again cross tenants.
ALTER TABLE kb_invitations
    DROP CONSTRAINT IF EXISTS chk_kb_invites_no_self_grant;

ALTER TABLE kb_invitations
    ADD CONSTRAINT chk_kb_invites_no_self_grant
    CHECK (owner_tenant_id <> recipient_tenant_id) NOT VALID;
