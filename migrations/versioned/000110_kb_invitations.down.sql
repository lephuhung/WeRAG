-- Rollback for 000110_kb_invitations.
-- Drops the recipient-bound KB invitation table. Legacy tenant-wide
-- kb_access_grants rows are untouched.

DROP TABLE IF EXISTS kb_invitations;
