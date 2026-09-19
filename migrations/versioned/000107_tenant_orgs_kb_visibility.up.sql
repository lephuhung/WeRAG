-- Migration: 000107_tenant_orgs_kb_visibility
-- Introduces tenant-scoped user groups ("orgs") and a visibility column
-- on knowledge_bases implementing the three-scope model:
--   - 'tenant' (default): every tenant member can read (unchanged).
--   - 'org': only members of the bound tenant_orgs row plus tenant
--     Admin/Owner and system admins can read/search. Org KBs never
--     cross the tenant boundary (no kb_shares / agent-share grants).
--   - 'public': readable/searchable by any authenticated user of any
--     tenant; writes stay with the owning tenant.
--
-- tenant_invitations.org_id binds a share-link invitation to an org so
-- org managers can onboard users by link: accepting registers the
-- account, joins the tenant with the link's role, and enrols the user
-- into the org in one step.
DO $$ BEGIN RAISE NOTICE '[Migration 000107] Creating tenant_orgs'; END $$;

CREATE TABLE IF NOT EXISTS tenant_orgs (
    id          BIGSERIAL   PRIMARY KEY,
    tenant_id   INTEGER     NOT NULL,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    created_by  VARCHAR(36) NOT NULL DEFAULT '',
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at  TIMESTAMP WITH TIME ZONE
);

-- Org names are unique within a tenant (soft-deleted names reusable).
CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_orgs_tenant_name
    ON tenant_orgs(tenant_id, name) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_tenant_orgs_tenant
    ON tenant_orgs(tenant_id) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS tenant_org_members (
    id          BIGSERIAL   PRIMARY KEY,
    org_id      BIGINT      NOT NULL,
    user_id     VARCHAR(36) NOT NULL,
    role        VARCHAR(20) NOT NULL DEFAULT 'member',
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_org_members_unique
    ON tenant_org_members(org_id, user_id);
CREATE INDEX IF NOT EXISTS idx_tenant_org_members_user
    ON tenant_org_members(user_id);

DO $$ BEGIN RAISE NOTICE '[Migration 000107] knowledge_bases visibility'; END $$;

ALTER TABLE knowledge_bases
    ADD COLUMN IF NOT EXISTS visibility VARCHAR(16) NOT NULL DEFAULT 'tenant';
ALTER TABLE knowledge_bases
    ADD COLUMN IF NOT EXISTS org_id BIGINT NULL;

CREATE INDEX IF NOT EXISTS idx_knowledge_bases_visibility
    ON knowledge_bases(visibility);
CREATE INDEX IF NOT EXISTS idx_knowledge_bases_org
    ON knowledge_bases(org_id) WHERE org_id IS NOT NULL;

DO $$ BEGIN RAISE NOTICE '[Migration 000107] tenant_invitations org binding'; END $$;

ALTER TABLE tenant_invitations
    ADD COLUMN IF NOT EXISTS org_id BIGINT NOT NULL DEFAULT 0;

DO $$ BEGIN RAISE NOTICE '[Migration 000107] done'; END $$;
