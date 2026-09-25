-- Migration: 000113_platform_public_knowledge_bases
-- Platform-owned public knowledge bases (plan 2026-09-24): split KB
-- ownership from the data-scope partition.
--
--   - Adds knowledge_bases.owner_tenant_id: the authorization ownership
--     field. owner_tenant_id > 0 means tenant-owned; 0 means
--     platform-owned (no tenant owner).
--   - Keeps knowledge_bases.tenant_id untouched as the immutable
--     document/index execution partition. No data-scope ID is rewritten.
--   - Backfills existing rows with owner_tenant_id = tenant_id (migration
--     000111 already narrowed every historical public row to tenant
--     visibility, so no row is republished by this backfill).
--   - Enforces the owner/visibility invariant:
--       public requires owner 0; tenant requires a nonzero owner.
--   - Adds a composite index serving owner + visibility catalog queries.
--
-- SQLite note: the repository's SQLite migration-test harness only replays
-- portable UPDATE / CREATE INDEX statements. ADD COLUMN IF NOT EXISTS and
-- ADD CONSTRAINT are Postgres-only and are covered on SQLite by Go model
-- validation (KnowledgeBase.ValidateOwnership) instead.

DO $$ BEGIN RAISE NOTICE '[Migration 000113] Adding knowledge_bases.owner_tenant_id'; END $$;

ALTER TABLE knowledge_bases
    ADD COLUMN IF NOT EXISTS owner_tenant_id BIGINT NOT NULL DEFAULT 0;

DO $$ BEGIN RAISE NOTICE '[Migration 000113] Backfilling owner from data scope'; END $$;

UPDATE knowledge_bases
SET owner_tenant_id = tenant_id
WHERE owner_tenant_id = 0 AND tenant_id <> 0;

DO $$ BEGIN RAISE NOTICE '[Migration 000113] Enforcing owner/visibility invariant'; END $$;

ALTER TABLE knowledge_bases
    DROP CONSTRAINT IF EXISTS chk_knowledge_bases_owner_visibility;

ALTER TABLE knowledge_bases
    ADD CONSTRAINT chk_knowledge_bases_owner_visibility CHECK (
        (visibility = 'public' AND owner_tenant_id = 0)
        OR (visibility = 'tenant' AND owner_tenant_id > 0)
    );

CREATE INDEX IF NOT EXISTS idx_knowledge_bases_owner_visibility
    ON knowledge_bases(owner_tenant_id, visibility);

DO $$ BEGIN RAISE NOTICE '[Migration 000113] Platform public KB ownership ready'; END $$;
