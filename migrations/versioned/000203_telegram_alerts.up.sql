-- Migration: 000203_telegram_alerts
-- The system alert bot on Telegram (SuperAdmins only): the chats it writes
-- to and the messages it sends.
--
-- telegram_alert_chats: a private chat or a group linked by a SuperAdmin
-- with a one-time code (/start <code>). A chat receives alerts while the
-- SuperAdmin who linked it is still an active SuperAdmin; disabled_at is
-- set when Telegram refuses the chat (bot blocked or removed) or /stop.
--
-- telegram_alert_messages: the outbox, one row per chat and alert, sent in
-- the background with retries. event_key names the alert ("order_paid:<id>",
-- a digest's content hash and episode, …): unique per chat, so the same
-- alert is never queued twice for a chat, whichever instance queues it.
-- Sent rows are purged after 30 days.
DO $$ BEGIN RAISE NOTICE '[Migration 000203] Creating telegram_alert_chats, telegram_alert_messages'; END $$;

CREATE TABLE IF NOT EXISTS telegram_alert_chats (
    id           VARCHAR(36)  PRIMARY KEY,
    chat_id      BIGINT       NOT NULL,
    chat_type    VARCHAR(16)  NOT NULL DEFAULT '',
    title        TEXT         NOT NULL DEFAULT '',
    linked_by    VARCHAR(36)  NOT NULL,
    linked_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    disabled_at  TIMESTAMP WITH TIME ZONE NULL,
    last_error   TEXT         NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_telegram_alert_chats_chat_id
    ON telegram_alert_chats (chat_id);

CREATE TABLE IF NOT EXISTS telegram_alert_messages (
    id               VARCHAR(36)  PRIMARY KEY,
    chat_id          BIGINT       NOT NULL,
    kind             VARCHAR(64)  NOT NULL,
    event_key        VARCHAR(200) NOT NULL,
    text             TEXT         NOT NULL,
    silent           BOOLEAN      NOT NULL DEFAULT FALSE,
    status           VARCHAR(16)  NOT NULL DEFAULT 'pending',
    attempts         INT          NOT NULL DEFAULT 0,
    next_attempt_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    message_id       BIGINT       NULL,
    last_error       TEXT         NOT NULL DEFAULT '',
    created_at       TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sent_at          TIMESTAMP WITH TIME ZONE NULL
);

-- the sender's queue
CREATE INDEX IF NOT EXISTS idx_telegram_alert_messages_due
    ON telegram_alert_messages (next_attempt_at) WHERE status = 'pending';
-- one message per chat and alert (inserted ON CONFLICT DO NOTHING)
CREATE UNIQUE INDEX IF NOT EXISTS idx_telegram_alert_messages_chat_event
    ON telegram_alert_messages (chat_id, event_key);
-- "already told?" across chats (the order watch)
CREATE INDEX IF NOT EXISTS idx_telegram_alert_messages_event
    ON telegram_alert_messages (event_key);
-- the latest alert of a kind in a chat (resolutions)
CREATE INDEX IF NOT EXISTS idx_telegram_alert_messages_kind
    ON telegram_alert_messages (kind, chat_id, created_at DESC);
