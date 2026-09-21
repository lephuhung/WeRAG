-- Rollback for sqlite 000028_kb_access_grants (mirrors versioned 000109).
-- Schema shape only: contributor/viewer distinction, org membership,
-- share rows and org bindings are not recoverable.
CREATE TABLE IF NOT EXISTS organizations (
    id                       VARCHAR(36) PRIMARY KEY,
    name                     VARCHAR(255) NOT NULL,
    description              TEXT,
    avatar                   VARCHAR(512),
    owner_id                 VARCHAR(36) NOT NULL,
    owner_tenant_id          INTEGER NOT NULL,
    invite_code              VARCHAR(32),
    invite_code_expires_at   DATETIME,
    invite_code_validity_days INTEGER DEFAULT 7,
    require_approval         INTEGER DEFAULT 0,
    searchable               INTEGER DEFAULT 0,
    member_limit             INTEGER DEFAULT 50,
    created_at               DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at               DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at               DATETIME
);

CREATE TABLE IF NOT EXISTS organization_tenant_members (
    id                     VARCHAR(36) PRIMARY KEY,
    organization_id        VARCHAR(36) NOT NULL,
    tenant_id              INTEGER NOT NULL,
    role                   VARCHAR(32) NOT NULL DEFAULT 'viewer',
    representative_user_id VARCHAR(36) DEFAULT '',
    joined_at              DATETIME,
    created_at             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS organization_join_requests (
    id              VARCHAR(36) PRIMARY KEY,
    organization_id VARCHAR(36) NOT NULL,
    user_id         VARCHAR(36) NOT NULL,
    tenant_id       INTEGER NOT NULL,
    request_type    VARCHAR(32) NOT NULL DEFAULT 'join',
    prev_role       VARCHAR(32),
    requested_role  VARCHAR(32) NOT NULL DEFAULT 'viewer',
    status          VARCHAR(32) NOT NULL DEFAULT 'pending',
    message         TEXT,
    reviewed_by     VARCHAR(36),
    reviewed_at     DATETIME,
    review_message  TEXT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS kb_shares (
    id                VARCHAR(36) PRIMARY KEY,
    knowledge_base_id VARCHAR(36) NOT NULL,
    organization_id   VARCHAR(36) NOT NULL,
    shared_by_user_id VARCHAR(36) NOT NULL,
    source_tenant_id  INTEGER NOT NULL,
    permission        VARCHAR(32) NOT NULL DEFAULT 'viewer',
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at        DATETIME
);

CREATE TABLE IF NOT EXISTS agent_shares (
    id                VARCHAR(36) PRIMARY KEY,
    agent_id          VARCHAR(36) NOT NULL,
    organization_id   VARCHAR(36) NOT NULL,
    shared_by_user_id VARCHAR(36) NOT NULL,
    source_tenant_id  INTEGER NOT NULL,
    permission        VARCHAR(32) NOT NULL DEFAULT 'viewer',
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at        DATETIME
);

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

CREATE TABLE IF NOT EXISTS tenant_org_members (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id     INTEGER NOT NULL,
    user_id    VARCHAR(36) NOT NULL,
    role       VARCHAR(20) NOT NULL DEFAULT 'member',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tenant_disabled_shared_agents (
    tenant_id        INTEGER NOT NULL,
    agent_id         VARCHAR(36) NOT NULL,
    source_tenant_id INTEGER NOT NULL,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, agent_id, source_tenant_id)
);

ALTER TABLE knowledge_bases ADD COLUMN org_id INTEGER NULL;
CREATE INDEX IF NOT EXISTS idx_knowledge_bases_org
    ON knowledge_bases(org_id) WHERE org_id IS NOT NULL;
ALTER TABLE tenant_invitations ADD COLUMN org_id INTEGER NOT NULL DEFAULT 0;

UPDATE tenant_members SET role = 'viewer' WHERE role = 'member';

DROP TABLE IF EXISTS kb_access_grants;
