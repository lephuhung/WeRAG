-- Migration: 000109_kb_access_grants
-- Replaces the Organization-based cross-tenant sharing model with direct
-- tenant-to-tenant KB access grants, merges the tenant roles
-- contributor/viewer into a single 'member' role (read + ingest into the
-- tenant's own knowledge bases), and removes the org-scoped KB visibility
-- level together with the tenant-org group tables.
--
-- Role model after this migration:
--   owner(40) > admin(30) > member(20)
--
-- KB visibility after this migration:
--   'tenant' (default): every member of the owning tenant can read
--   'public': readable by every authenticated user; writes stay with the
--             owning tenant's Owner / system admins
--   ('org' is removed; existing rows are remapped to 'tenant')
--
-- Grant lifecycle:
--   pending -> approved | rejected | revoked | expired
-- Only 'approved' rows grant read access to the grantee tenant.

DO $$ BEGIN RAISE NOTICE '[Migration 000109] Creating kb_access_grants'; END $$;

CREATE TABLE IF NOT EXISTS kb_access_grants (
    id                VARCHAR(36) PRIMARY KEY,
    kb_id             VARCHAR(36) NOT NULL,
    owner_tenant_id   BIGINT      NOT NULL,
    grantee_tenant_id BIGINT      NOT NULL,
    permission        VARCHAR(16) NOT NULL DEFAULT 'viewer',
    status            VARCHAR(16) NOT NULL DEFAULT 'pending',
    requested_by      VARCHAR(36) NOT NULL DEFAULT '',
    approved_by       VARCHAR(36),
    message           VARCHAR(500),
    expires_at        TIMESTAMP WITH TIME ZONE,
    responded_at      TIMESTAMP WITH TIME ZONE,
    created_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at        TIMESTAMP WITH TIME ZONE
);

-- At most one live grant/request per (kb, grantee tenant); terminal-state
-- rows accumulate for the audit trail.
CREATE UNIQUE INDEX IF NOT EXISTS uniq_kb_grant_pending
    ON kb_access_grants(kb_id, grantee_tenant_id)
    WHERE status IN ('pending', 'approved') AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_kb_grants_kb
    ON kb_access_grants(kb_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_kb_grants_grantee
    ON kb_access_grants(grantee_tenant_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_kb_grants_owner
    ON kb_access_grants(owner_tenant_id) WHERE deleted_at IS NULL;

DO $$ BEGIN RAISE NOTICE '[Migration 000109] Backfilling kb_shares into kb_access_grants'; END $$;

-- Every existing KB share becomes an approved read grant for each member
-- tenant of the target organization (excluding the source tenant itself).
INSERT INTO kb_access_grants (
    id, kb_id, owner_tenant_id, grantee_tenant_id,
    permission, status, requested_by, approved_by,
    created_at, updated_at
)
SELECT gen_random_uuid()::text,
       s.knowledge_base_id,
       s.source_tenant_id,
       m.tenant_id,
       'viewer',
       'approved',
       s.shared_by_user_id,
       s.shared_by_user_id,
       s.created_at,
       CURRENT_TIMESTAMP
FROM kb_shares s
JOIN organization_tenant_members m
    ON m.organization_id = s.organization_id
WHERE s.deleted_at IS NULL
  AND m.tenant_id <> s.source_tenant_id
ON CONFLICT DO NOTHING;

DO $$ BEGIN RAISE NOTICE '[Migration 000109] Merging tenant roles contributor/viewer -> member'; END $$;

UPDATE tenant_members SET role = 'member' WHERE role IN ('contributor', 'viewer');

DO $$ BEGIN RAISE NOTICE '[Migration 000109] Remapping org-scoped KBs to tenant visibility'; END $$;

-- Org-scoped KBs widen to tenant visibility: org membership is gone, so
-- the closest surviving scope is the owning tenant itself.
UPDATE knowledge_bases SET visibility = 'tenant', org_id = NULL WHERE visibility = 'org';

DROP INDEX IF EXISTS idx_knowledge_bases_org;
ALTER TABLE knowledge_bases DROP COLUMN IF EXISTS org_id;
ALTER TABLE tenant_invitations DROP COLUMN IF EXISTS org_id;

DO $$ BEGIN RAISE NOTICE '[Migration 000109] Dropping organization / tenant-org tables'; END $$;

DROP TABLE IF EXISTS agent_shares;
DROP TABLE IF EXISTS kb_shares;
DROP TABLE IF EXISTS organization_join_requests;
DROP TABLE IF EXISTS organization_tenant_members;
-- organization_members_pre_plan3 is the parked pre-000045 backup of
-- organization_members (kept for that migration's rollback); it still
-- holds an FK on organizations and must go before the parent table.
DROP TABLE IF EXISTS organization_members_pre_plan3;
DROP TABLE IF EXISTS organizations;
DROP TABLE IF EXISTS tenant_org_members;
DROP TABLE IF EXISTS tenant_orgs;
DROP TABLE IF EXISTS tenant_disabled_shared_agents;

DO $$ BEGIN RAISE NOTICE '[Migration 000109] done'; END $$;
