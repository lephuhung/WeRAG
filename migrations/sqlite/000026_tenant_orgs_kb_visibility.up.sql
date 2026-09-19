-- Mirrors versioned migration 000107_tenant_orgs_kb_visibility: tenant
-- orgs (user groups inside a tenant), org-scoped and public KB
-- visibility, and org-bound share-link invitations.
CREATE TABLE IF NOT EXISTS tenant_orgs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id   INTEGER NOT NULL,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    created_by  VARCHAR(36) NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at  DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_orgs_tenant_name
    ON tenant_orgs(tenant_id, name) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_tenant_orgs_tenant
    ON tenant_orgs(tenant_id) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS tenant_org_members (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id      INTEGER NOT NULL,
    user_id     VARCHAR(36) NOT NULL,
    role        VARCHAR(20) NOT NULL DEFAULT 'member',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_org_members_unique
    ON tenant_org_members(org_id, user_id);
CREATE INDEX IF NOT EXISTS idx_tenant_org_members_user
    ON tenant_org_members(user_id);

ALTER TABLE knowledge_bases ADD COLUMN visibility VARCHAR(16) NOT NULL DEFAULT 'tenant';
ALTER TABLE knowledge_bases ADD COLUMN org_id INTEGER NULL;
CREATE INDEX IF NOT EXISTS idx_knowledge_bases_visibility
    ON knowledge_bases(visibility);
CREATE INDEX IF NOT EXISTS idx_knowledge_bases_org
    ON knowledge_bases(org_id) WHERE org_id IS NOT NULL;

ALTER TABLE tenant_invitations ADD COLUMN org_id INTEGER NOT NULL DEFAULT 0;
