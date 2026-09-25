-- Migration: 000112_three_role_model
-- Completes the human role model: SuperAdmin (platform,
-- users.is_system_admin) plus Tenant Admin / Member memberships.
--
--   1. owner -> admin (legacy tenant-management alias removed).
--   2. contributor/viewer (pre-000109 leftovers) and any other unknown
--      role value -> member (fail-closed least privilege).
--
-- After this migration the only tenant-scope roles in the table are
-- 'admin' and 'member'. New 'owner' assignments are rejected by the
-- service layer. Legacy CanAccessAllTenants flags are NOT touched and
-- confer no role by themselves.

DO $$ BEGIN RAISE NOTICE '[Migration 000112] Converting owner memberships to admin'; END $$;

UPDATE tenant_members
SET role = 'admin', updated_at = CURRENT_TIMESTAMP
WHERE role = 'owner' AND deleted_at IS NULL;

DO $$ BEGIN RAISE NOTICE '[Migration 000112] Normalizing legacy/unknown roles to member'; END $$;

UPDATE tenant_members
SET role = 'member', updated_at = CURRENT_TIMESTAMP
WHERE role NOT IN ('admin', 'member') AND deleted_at IS NULL;
