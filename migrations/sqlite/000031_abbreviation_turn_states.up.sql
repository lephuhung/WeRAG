-- Mirrors versioned migration 000114_abbreviation_turn_states: durable
-- clarification-turn state for the abbreviation-resolution gate.
--
-- Portable differences from the Postgres original: payload is TEXT JSON
-- instead of JSONB, DATETIME instead of TIMESTAMPTZ. The state/version/role
-- CHECKs, the unique root, and the partial unique one-waiting index are
-- identical (SQLite supports partial indexes). Touches no legacy table.
CREATE TABLE IF NOT EXISTS abbreviation_turn_states (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    session_id VARCHAR(36) NOT NULL,
    owner_id VARCHAR(512) NOT NULL DEFAULT '',
    principal_id VARCHAR(512) NOT NULL DEFAULT '',
    root_user_message_id VARCHAR(36) NOT NULL,
    clarification_message_id VARCHAR(36) NOT NULL DEFAULT '',
    executing_message_id VARCHAR(36) NOT NULL DEFAULT '',
    state VARCHAR(32) NOT NULL,
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1,
    payload TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
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
