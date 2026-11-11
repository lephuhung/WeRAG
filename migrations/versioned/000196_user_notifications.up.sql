-- Migration: 000196_user_notifications
-- In-app notifications for one person, shown by the web app and the Word
-- add-in while the person is online: a toast once (seen_at), then in the
-- bell list until read (read_at). They belong to the account, not to an
-- organization (tenant_id records where the event happened). Producers
-- insert with a unique dedupe_key (ON CONFLICT DO NOTHING) so a repeated
-- run never notifies twice; read ones are purged after 90 days. First
-- producer: a legal document the person asked for was added (kind
-- 'legal_request_added').
DO $$ BEGIN RAISE NOTICE '[Migration 000196] Creating user_notifications'; END $$;

CREATE TABLE IF NOT EXISTS user_notifications (
    id          VARCHAR(36)   PRIMARY KEY,
    tenant_id   BIGINT        NOT NULL DEFAULT 0,
    user_id     VARCHAR(36)   NOT NULL,
    kind        VARCHAR(64)   NOT NULL,
    title       TEXT          NOT NULL DEFAULT '',
    body        TEXT          NOT NULL DEFAULT '',
    link        VARCHAR(1024) NOT NULL DEFAULT '',
    payload     JSONB         NULL,
    dedupe_key  VARCHAR(200)  NOT NULL,
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    seen_at     TIMESTAMP WITH TIME ZONE NULL,
    read_at     TIMESTAMP WITH TIME ZONE NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_notifications_dedupe_key
    ON user_notifications (dedupe_key);
CREATE INDEX IF NOT EXISTS idx_user_notifications_user_read
    ON user_notifications (user_id, read_at);
CREATE INDEX IF NOT EXISTS idx_user_notifications_user_created
    ON user_notifications (user_id, created_at DESC);
-- Read notifications older than 90 days are purged daily, in batches.
CREATE INDEX IF NOT EXISTS idx_user_notifications_read_at
    ON user_notifications (read_at) WHERE read_at IS NOT NULL;
