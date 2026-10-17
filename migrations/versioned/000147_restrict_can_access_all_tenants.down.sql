-- The cleared can_access_all_tenants flags are deliberately not restored.
ALTER TABLE kb_invitations DROP COLUMN IF EXISTS revoked_reason;
