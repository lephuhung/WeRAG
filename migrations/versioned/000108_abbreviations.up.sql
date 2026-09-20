-- Migration: 000108_abbreviations
-- Global (non-tenant-scoped) abbreviation dictionary for Vietnamese
-- administrative/legal shorthand. Rows are suggestions until an admin
-- activates them (is_active), mirroring the AIRAG review workflow.
-- Multiple active rows per short_form are allowed — the expander treats
-- them as ambiguous and asks instead of guessing.
DO $$ BEGIN RAISE NOTICE '[Migration 000108] Creating abbreviations table'; END $$;

CREATE TABLE IF NOT EXISTS abbreviations (
    id           VARCHAR(36)  PRIMARY KEY,
    short_form   VARCHAR(50)  NOT NULL,
    full_form    VARCHAR(255) NOT NULL,
    description  TEXT         NOT NULL DEFAULT '',
    is_active    BOOLEAN      NOT NULL DEFAULT FALSE,
    suggested_by VARCHAR(36)  NOT NULL DEFAULT '',
    created_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at   TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS ix_abbreviations_short_form
    ON abbreviations (lower(short_form)) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS ix_abbreviations_active
    ON abbreviations (is_active) WHERE deleted_at IS NULL;
