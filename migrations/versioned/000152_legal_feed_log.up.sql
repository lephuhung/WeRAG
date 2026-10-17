-- Legal feed log: every write the MCP "legal_write" tool group makes to the
-- public legal knowledge bases (ingest, replace, remove, audit removal,
-- review scan, wiki rebuild), so an operator can see what an external
-- feeding agent did and why. Append-only; rows are never updated.
-- See docs/legal-document-feed-spec.md §6.
DO $$ BEGIN RAISE NOTICE '[Migration 000152] Creating legal_feed_log'; END $$;

CREATE TABLE IF NOT EXISTS legal_feed_log (
    id                BIGSERIAL    PRIMARY KEY,
    action            VARCHAR(32)  NOT NULL,
    vbpl_id           VARCHAR(64)  NOT NULL DEFAULT '',
    knowledge_id      VARCHAR(36)  NOT NULL DEFAULT '',
    knowledge_base_id VARCHAR(36)  NOT NULL DEFAULT '',
    document_number   VARCHAR(128) NOT NULL DEFAULT '',
    reason            TEXT         NOT NULL DEFAULT '',
    actor_endpoint_id VARCHAR(36)  NOT NULL DEFAULT '',
    actor_tenant_id   BIGINT       NOT NULL DEFAULT 0,
    detail            JSONB,
    created_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS ix_legal_feed_log_vbpl ON legal_feed_log (vbpl_id);
CREATE INDEX IF NOT EXISTS ix_legal_feed_log_created ON legal_feed_log (created_at DESC);
