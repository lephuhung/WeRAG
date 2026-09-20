-- Mirrors versioned migration 000108_abbreviations: global abbreviation
-- dictionary (suggestions pending admin activation via is_active).
CREATE TABLE IF NOT EXISTS abbreviations (
    id           VARCHAR(36) PRIMARY KEY,
    short_form   VARCHAR(50)  NOT NULL,
    full_form    VARCHAR(255) NOT NULL,
    description  TEXT         NOT NULL DEFAULT '',
    is_active    BOOLEAN      NOT NULL DEFAULT FALSE,
    suggested_by VARCHAR(36)  NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at   DATETIME
);
CREATE INDEX IF NOT EXISTS ix_abbreviations_short_form
    ON abbreviations (short_form) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS ix_abbreviations_active
    ON abbreviations (is_active) WHERE deleted_at IS NULL;
