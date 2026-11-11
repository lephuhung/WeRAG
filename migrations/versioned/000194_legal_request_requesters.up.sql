-- Migration: 000194_legal_request_requesters
-- Everyone who asked for a legal document request's document, so they can
-- be told when it is added (legal_document_requests keeps only the last
-- asker in last_*). One row per (request_id, user_id): the same person
-- asking again moves the row to the latest conversation and makes it
-- pending again. notified_at is set once the person was told (by mail when
-- the account has an address), notify_status says how ('sent', 'no_email',
-- 'inactive', 'no_user').
DO $$ BEGIN RAISE NOTICE '[Migration 000194] Creating legal_request_requesters'; END $$;

CREATE TABLE IF NOT EXISTS legal_request_requesters (
    id            VARCHAR(36)  PRIMARY KEY,
    request_id    VARCHAR(36)  NOT NULL REFERENCES legal_document_requests(id) ON DELETE CASCADE,
    tenant_id     BIGINT       NOT NULL DEFAULT 0,
    user_id       VARCHAR(36)  NOT NULL,
    session_id    VARCHAR(36)  NOT NULL DEFAULT '',
    message_id    VARCHAR(36)  NULL,
    channel       VARCHAR(32)  NOT NULL DEFAULT '',
    notified_at   TIMESTAMP WITH TIME ZONE NULL,
    notify_status VARCHAR(16)  NOT NULL DEFAULT '',
    created_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_legal_request_requesters_user
    ON legal_request_requesters (request_id, user_id);
CREATE INDEX IF NOT EXISTS idx_legal_request_requesters_pending
    ON legal_request_requesters (request_id) WHERE notified_at IS NULL;

-- The last asker of each open request people asked for is the only one
-- known so far: keep them, so they are told too.
INSERT INTO legal_request_requesters (id, request_id, tenant_id, user_id, session_id, channel, created_at, updated_at)
SELECT gen_random_uuid()::text, r.id, r.last_tenant_id, r.last_user_id, r.last_session_id, '',
       r.last_requested_at, r.last_requested_at
  FROM legal_document_requests r
 WHERE r.status IN ('requested', 'fetched', 'reviewing', 'auto_ingest')
   AND r.request_count > 0
   AND r.last_user_id <> ''
   AND r.last_session_id <> ''
ON CONFLICT (request_id, user_id) DO NOTHING;
