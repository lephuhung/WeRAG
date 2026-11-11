-- Validity lookups (GET /legal-lookup) that found nothing, or found only
-- documents mentioning a số hiệu the catalogue lacks, counted per
-- normalized query so a SuperAdmin sees the catalogue's gaps. One row per
-- query (upserted); the lookup never fails because of this table.
DO $$ BEGIN RAISE NOTICE '[Migration 000194] Creating legal_lookup_misses'; END $$;

CREATE TABLE IF NOT EXISTS legal_lookup_misses (
    id             BIGSERIAL    PRIMARY KEY,
    query          VARCHAR(200) NOT NULL,
    sample         VARCHAR(200) NOT NULL DEFAULT '',
    number         VARCHAR(128) NOT NULL DEFAULT '',
    reason         VARCHAR(16)  NOT NULL DEFAULT '',
    count          BIGINT       NOT NULL DEFAULT 1,
    first_at       TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_tenant_id BIGINT       NOT NULL DEFAULT 0,
    last_user_id   VARCHAR(36)  NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_legal_lookup_misses_query ON legal_lookup_misses (query);
CREATE INDEX IF NOT EXISTS ix_legal_lookup_misses_last ON legal_lookup_misses (last_at DESC);
CREATE INDEX IF NOT EXISTS ix_legal_lookup_misses_count ON legal_lookup_misses (count DESC);
