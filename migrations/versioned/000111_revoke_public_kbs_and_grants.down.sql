-- Rollback for 000111_revoke_public_kbs_and_grants.
--
-- INTENTIONALLY A NO-OP: this migration performs an approved,
-- irreversible data narrowing (demo data; no old access is preserved).
-- The pre-migration set of public KBs and the pending-vs-approved state
-- of each revoked grant cannot be reconstructed from the narrowed rows,
-- so there is nothing safe to restore. If cross-tenant reads are needed
-- again, issue recipient-bound kb_invitations instead.

DO $$ BEGIN RAISE NOTICE '[Migration 000111] Rollback is a no-op: public/grant narrowing is irreversible by design'; END $$;
SELECT 1;
