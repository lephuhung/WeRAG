-- Migration: 000182_email_notifications
-- Email notifications that must go out at most once (package expiry
-- reminders 14/7/1 days before the last usable day). A backend claims the
-- row (unique dedupe_key) before sending and deletes it when the send
-- fails, so restarts and several backends never mail the same reminder
-- twice.
CREATE TABLE IF NOT EXISTS email_notifications (
    id          BIGSERIAL PRIMARY KEY,
    kind        VARCHAR(32)  NOT NULL,
    dedupe_key  VARCHAR(200) NOT NULL,
    ref_id      VARCHAR(64)  NOT NULL,
    recipients  INTEGER      NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_email_notifications_dedupe_key ON email_notifications (dedupe_key);
CREATE INDEX IF NOT EXISTS idx_email_notifications_ref ON email_notifications (kind, ref_id);
