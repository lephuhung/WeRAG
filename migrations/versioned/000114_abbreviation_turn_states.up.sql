-- Migration: 000114_abbreviation_turn_states
-- Durable clarification-turn state for the abbreviation-resolution gate
-- (plan 2026-09-25):
--
--   - abbreviation_turn_states: one row per clarification turn, fully
--     owner-scoped (tenant/session/owner/principal). The typed payload is a
--     JSONB document (Task-1 resolution DTOs plus the frozen request
--     snapshot). Expiry is a fixed 24h deadline assigned at Begin and never
--     overwritten by partial replies.
--   - state CHECK admits exactly the turn lifecycle; version CHECK keeps the
--     CAS counter positive.
--   - Unique root (tenant, session, root message): one turn per question.
--   - Partial unique (tenant, session) WHERE awaiting_definition: at most
--     one automatically-resumed turn per session.
--   - abbreviation_turn_messages: message links (message PK, request FK to
--     the turn, FK to messages with cascade). Forks never copy links.
--   - abbreviation_suggestion_locks: Task-5 uniqueness locks over normalized
--     short/full keys. No unique index is added to the legacy abbreviations
--     dictionary itself.
--   - Touches no legacy table (abbreviations, sessions, messages unchanged).

CREATE TABLE IF NOT EXISTS abbreviation_turn_states (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    session_id VARCHAR(36) NOT NULL,
    owner_id VARCHAR(512) NOT NULL DEFAULT '',
    principal_id VARCHAR(512) NOT NULL DEFAULT '',
    root_user_message_id VARCHAR(36) NOT NULL,
    clarification_message_id VARCHAR(36) NOT NULL DEFAULT '',
    executing_message_id VARCHAR(36) NOT NULL DEFAULT '',
    state VARCHAR(32) NOT NULL,
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1,
    payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT chk_abbreviation_turn_states_state CHECK (
        state IN ('inspecting', 'awaiting_definition', 'ready', 'running',
                  'completed', 'blocked_error', 'cancelled', 'expired')
    ),
    CONSTRAINT chk_abbreviation_turn_states_version CHECK (version >= 1)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_abbreviation_turn_root
    ON abbreviation_turn_states (tenant_id, session_id, root_user_message_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_abbreviation_one_waiting
    ON abbreviation_turn_states (tenant_id, session_id)
    WHERE state = 'awaiting_definition';

CREATE TABLE IF NOT EXISTS abbreviation_turn_messages (
    message_id VARCHAR(36) PRIMARY KEY
        REFERENCES messages(id) ON DELETE CASCADE,
    request_id VARCHAR(36) NOT NULL
        REFERENCES abbreviation_turn_states(id) ON DELETE CASCADE,
    role VARCHAR(16) NOT NULL,
    CONSTRAINT chk_abbreviation_turn_messages_role CHECK (
        role IN ('root', 'definition', 'clarification', 'answer')
    )
);

CREATE INDEX IF NOT EXISTS idx_abbreviation_turn_messages_request
    ON abbreviation_turn_messages (request_id);

CREATE TABLE IF NOT EXISTS abbreviation_suggestion_locks (
    "key" VARCHAR(64) PRIMARY KEY
);
