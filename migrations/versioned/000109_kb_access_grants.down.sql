-- Rollback for 000109_kb_access_grants.
--
-- NOTE: this restores schema shape only. Data loss is NOT recoverable:
--   - contributor vs viewer distinction was collapsed into 'member'
--     (down maps member -> 'viewer', the safer lower-privilege value);
--   - org membership, join requests, share rows and org bindings are
--     gone for good — grants are preserved by re-seeding kb_shares /
--     organizations is impossible (org IDs were uuid strings tied to
--     rows that no longer exist).

DO $$ BEGIN RAISE NOTICE '[Migration 000109 down] Restoring org tables (empty)'; END $$;

CREATE TABLE IF NOT EXISTS organizations (
    id                       VARCHAR(36) PRIMARY KEY,
    name                     VARCHAR(255) NOT NULL,
    description              TEXT,
    avatar                   VARCHAR(512),
    owner_id                 VARCHAR(36) NOT NULL,
    owner_tenant_id          BIGINT NOT NULL,
    invite_code              VARCHAR(32),
    invite_code_expires_at   TIMESTAMP WITH TIME ZONE,
    invite_code_validity_days INTEGER DEFAULT 7,
    require_approval         BOOLEAN DEFAULT FALSE,
    searchable               BOOLEAN DEFAULT FALSE,
    member_limit             INTEGER DEFAULT 50,
    created_at               TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at               TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at               TIMESTAMP WITH TIME ZONE
);

CREATE TABLE IF NOT EXISTS organization_tenant_members (
    id                     VARCHAR(36) PRIMARY KEY,
    organization_id        VARCHAR(36) NOT NULL,
    tenant_id              BIGINT NOT NULL,
    role                   VARCHAR(32) NOT NULL DEFAULT 'viewer',
    representative_user_id VARCHAR(36) DEFAULT '',
    joined_at              TIMESTAMP WITH TIME ZONE,
    created_at             TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at             TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS organization_join_requests (
    id              VARCHAR(36) PRIMARY KEY,
    organization_id VARCHAR(36) NOT NULL,
    user_id         VARCHAR(36) NOT NULL,
    tenant_id       BIGINT NOT NULL,
    request_type    VARCHAR(32) NOT NULL DEFAULT 'join',
    prev_role       VARCHAR(32),
    requested_role  VARCHAR(32) NOT NULL DEFAULT 'viewer',
    status          VARCHAR(32) NOT NULL DEFAULT 'pending',
    message         TEXT,
    reviewed_by     VARCHAR(36),
    reviewed_at     TIMESTAMP WITH TIME ZONE,
    review_message  TEXT,
    created_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS kb_shares (
    id                VARCHAR(36) PRIMARY KEY,
    knowledge_base_id VARCHAR(36) NOT NULL,
    organization_id   VARCHAR(36) NOT NULL,
    shared_by_user_id VARCHAR(36) NOT NULL,
    source_tenant_id  BIGINT NOT NULL,
    permission        VARCHAR(32) NOT NULL DEFAULT 'viewer',
    created_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at        TIMESTAMP WITH TIME ZONE
);

CREATE TABLE IF NOT EXISTS agent_shares (
    id                VARCHAR(36) PRIMARY KEY,
    agent_id          VARCHAR(36) NOT NULL,
    organization_id   VARCHAR(36) NOT NULL,
    shared_by_user_id VARCHAR(36) NOT NULL,
    source_tenant_id  BIGINT NOT NULL,
    permission        VARCHAR(32) NOT NULL DEFAULT 'viewer',
    created_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at        TIMESTAMP WITH TIME ZONE
);

CREATE TABLE IF NOT EXISTS tenant_orgs (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   INTEGER NOT NULL,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    created_by  VARCHAR(36) NOT NULL DEFAULT '',
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at  TIMESTAMP WITH TIME ZONE
);

CREATE TABLE IF NOT EXISTS tenant_org_members (
    id         BIGSERIAL PRIMARY KEY,
    org_id     BIGINT NOT NULL,
    user_id    VARCHAR(36) NOT NULL,
    role       VARCHAR(20) NOT NULL DEFAULT 'member',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tenant_disabled_shared_agents (
    tenant_id        BIGINT NOT NULL,
    agent_id         VARCHAR(36) NOT NULL,
    source_tenant_id BIGINT NOT NULL,
    created_at       TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, agent_id, source_tenant_id)
);

DO $$ BEGIN RAISE NOTICE '[Migration 000109 down] Restoring columns / roles'; END $$;

ALTER TABLE knowledge_bases ADD COLUMN IF NOT EXISTS org_id BIGINT NULL;
CREATE INDEX IF NOT EXISTS idx_knowledge_bases_org
    ON knowledge_bases(org_id) WHERE org_id IS NOT NULL;
ALTER TABLE tenant_invitations ADD COLUMN IF NOT EXISTS org_id BIGINT NOT NULL DEFAULT 0;

-- Best-effort role restore: member -> viewer (cannot recover contributor).
UPDATE tenant_members SET role = 'viewer' WHERE role = 'member';

DROP TABLE IF EXISTS kb_access_grants;

DO $$ BEGIN RAISE NOTICE '[Migration 000109 down] done'; END $$;
