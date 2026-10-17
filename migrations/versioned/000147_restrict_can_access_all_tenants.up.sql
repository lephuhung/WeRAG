-- Tenant/KB permissions, phase 1 (2026-10-17).
--
-- 1. Only SuperAdmins may carry users.can_access_all_tenants, and the flag
--    no longer grants anything on its own (platform authority follows
--    is_system_admin). Clear it on every other account. Idempotent: a
--    second run changes no rows.
DO $$
DECLARE
    cleared INTEGER;
BEGIN
    UPDATE users
       SET can_access_all_tenants = false
     WHERE is_system_admin = false
       AND can_access_all_tenants = true;
    GET DIAGNOSTICS cleared = ROW_COUNT;
    RAISE NOTICE '000147: cleared can_access_all_tenants on % non-SuperAdmin user(s)', cleared;
END $$;

-- 2. Why a KB invitation was revoked by the system: 'owner_changed' when
--    the KB moved to another owner tenant; '' for manual revokes.
ALTER TABLE kb_invitations
    ADD COLUMN IF NOT EXISTS revoked_reason VARCHAR(32) NOT NULL DEFAULT '';
