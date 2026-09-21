-- Mirrors versioned migration 000109_kb_access_grants: direct
-- tenant-to-tenant KB access grants replace Organization sharing,
-- contributor/viewer merge into 'member', org-scoped visibility is
-- remapped to 'tenant', and the org/tenant-org tables are dropped.
CREATE TABLE IF NOT EXISTS kb_access_grants (
    id                VARCHAR(36) PRIMARY KEY,
    kb_id             VARCHAR(36) NOT NULL,
    owner_tenant_id   INTEGER NOT NULL,
    grantee_tenant_id INTEGER NOT NULL,
    permission        VARCHAR(16) NOT NULL DEFAULT 'viewer',
    status            VARCHAR(16) NOT NULL DEFAULT 'pending',
    requested_by      VARCHAR(36) NOT NULL DEFAULT '',
    approved_by       VARCHAR(36),
    message           VARCHAR(500),
    expires_at        DATETIME,
    responded_at      DATETIME,
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at        DATETIME
);

CREATE UNIQUE INDEX IF NOT EXISTS uniq_kb_grant_pending
    ON kb_access_grants(kb_id, grantee_tenant_id)
    WHERE status IN ('pending', 'approved') AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_kb_grants_kb
    ON kb_access_grants(kb_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_kb_grants_grantee
    ON kb_access_grants(grantee_tenant_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_kb_grants_owner
    ON kb_access_grants(owner_tenant_id) WHERE deleted_at IS NULL;

-- Backfill: kb_shares -> approved viewer grant per member tenant of the org.
INSERT INTO kb_access_grants (
    id, kb_id, owner_tenant_id, grantee_tenant_id,
    permission, status, requested_by, approved_by,
    created_at, updated_at
)
SELECT lower(hex(randomblob(16))),
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
  AND m.tenant_id <> s.source_tenant_id;

UPDATE tenant_members SET role = 'member' WHERE role IN ('contributor', 'viewer');

UPDATE knowledge_bases SET visibility = 'tenant', org_id = NULL WHERE visibility = 'org';

DROP INDEX IF EXISTS idx_knowledge_bases_org;
ALTER TABLE knowledge_bases DROP COLUMN org_id;
ALTER TABLE tenant_invitations DROP COLUMN org_id;

DROP TABLE IF EXISTS agent_shares;
DROP TABLE IF EXISTS kb_shares;
DROP TABLE IF EXISTS organization_join_requests;
DROP TABLE IF EXISTS organization_tenant_members;
DROP TABLE IF EXISTS organizations;
DROP TABLE IF EXISTS tenant_org_members;
DROP TABLE IF EXISTS tenant_orgs;
DROP TABLE IF EXISTS tenant_disabled_shared_agents;
