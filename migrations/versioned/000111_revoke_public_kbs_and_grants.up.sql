-- Migration: 000111_revoke_public_kbs_and_grants
-- Approved demo-data narrowing (tenant/KB permission plan):
-- KBs are private to their owning tenant; cross-tenant reads happen
-- only via recipient-bound kb_invitations, never via public visibility
-- or tenant-wide grants.
--
--   1. Every 'public' knowledge base becomes 'tenant' visibility.
--   2. Every pending/approved kb_access_grants row becomes 'revoked'
--      (terminal; stops authorizing immediately). Rejected/expired rows
--      are untouched. responded_at is stamped for the audit trail.

DO $$ BEGIN RAISE NOTICE '[Migration 000111] Narrowing public KBs to tenant visibility'; END $$;

UPDATE knowledge_bases
SET visibility = 'tenant', updated_at = CURRENT_TIMESTAMP
WHERE visibility = 'public';

DO $$ BEGIN RAISE NOTICE '[Migration 000111] Revoking live tenant-wide KB grants'; END $$;

UPDATE kb_access_grants
SET status = 'revoked',
    responded_at = COALESCE(responded_at, CURRENT_TIMESTAMP),
    updated_at = CURRENT_TIMESTAMP
WHERE status IN ('pending', 'approved') AND deleted_at IS NULL;
