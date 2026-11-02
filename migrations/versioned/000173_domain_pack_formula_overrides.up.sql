-- Migration: 000173_domain_pack_formula_overrides
-- A SuperAdmin's edit of a domain pack formula (name, expression, variable
-- descriptions/units/sources, basis, examples). The shipped formulas.yaml
-- stays the default: a row is applied over it at startup and on save, and
-- deleting the row restores it. The id, tool and variable names cannot be
-- edited (the Go implementation is bound to them); the backend checks every
-- row again at startup and skips one that no longer fits the build.

CREATE TABLE IF NOT EXISTS domain_pack_formula_overrides (
    pack_name   VARCHAR(128) NOT NULL,
    formula_id  VARCHAR(128) NOT NULL,
    formula     JSONB        NOT NULL,
    updated_by  VARCHAR(36)  NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (pack_name, formula_id)
);

DO $$ BEGIN RAISE NOTICE '[Migration 000173] domain_pack_formula_overrides present'; END $$;
