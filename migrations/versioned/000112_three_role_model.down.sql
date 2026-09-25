-- Rollback for 000112_three_role_model.
--
-- INTENTIONALLY A NO-OP: the owner-vs-admin distinction for converted
-- rows cannot be reconstructed (both were legitimate pre-migration
-- values collapsed into 'admin'), and legacy contributor/viewer rows
-- cannot be told apart from plain members. Re-creating 'owner' rows
-- from this migration would silently re-grant a removed privilege
-- level, so no automatic restore is offered.

DO $$ BEGIN RAISE NOTICE '[Migration 000112] Rollback is a no-op: role collapse is irreversible by design'; END $$;
SELECT 1;
