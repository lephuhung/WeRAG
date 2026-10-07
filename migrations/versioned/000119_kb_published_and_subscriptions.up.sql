-- Migration: 000119_kb_published_and_subscriptions
-- Tenant-published knowledge bases and their subscriptions.
--
--   1. visibility = 'published': a tenant-owned KB (owner_tenant_id > 0)
--      its Tenant Admin opened to every authenticated human. The owner
--      keeps all writes. 'public' stays reserved for platform-owned KBs.
--   2. kb_subscriptions: which published KBs enter a reader's default
--      retrieval scope. user_id = '' subscribes the whole tenant (set by
--      its Tenant Admin); a non-empty user_id subscribes one member.

DO $$ BEGIN RAISE NOTICE '[Migration 000119] Allowing published visibility'; END $$;

ALTER TABLE knowledge_bases
    DROP CONSTRAINT IF EXISTS chk_knowledge_bases_owner_visibility;

ALTER TABLE knowledge_bases
    ADD CONSTRAINT chk_knowledge_bases_owner_visibility CHECK (
        (visibility = 'public' AND owner_tenant_id = 0)
        OR (visibility IN ('tenant', 'published') AND owner_tenant_id > 0)
    );

DO $$ BEGIN RAISE NOTICE '[Migration 000119] Creating kb_subscriptions'; END $$;

CREATE TABLE IF NOT EXISTS kb_subscriptions (
    id          VARCHAR(36) PRIMARY KEY,
    kb_id       VARCHAR(36) NOT NULL,
    tenant_id   BIGINT      NOT NULL,
    user_id     VARCHAR(36) NOT NULL DEFAULT '',
    created_by  VARCHAR(36) NOT NULL DEFAULT '',
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uniq_kb_subscriptions_subject
    ON kb_subscriptions(kb_id, tenant_id, user_id);
CREATE INDEX IF NOT EXISTS idx_kb_subscriptions_reader
    ON kb_subscriptions(tenant_id, user_id);
