-- A workspace's font sizes for the NĐ30 templates drafted from chat
-- (2026-10-17). The platform default is the system setting
-- document_template.font_sizes; a key stored here overrides it for this
-- workspace, a key left out follows it. Edited by the workspace's admin.
--
-- Idempotent: every statement can run twice.

DO $$ BEGIN RAISE NOTICE '[Migration 000156] tenant_document_template_styles'; END $$;

CREATE TABLE IF NOT EXISTS tenant_document_template_styles (
    tenant_id  BIGINT      PRIMARY KEY,
    -- {"tieu_ngu": 14, "quoc_hieu": 13, ...}, sizes in points
    font_sizes JSONB       NOT NULL DEFAULT '{}',
    updated_by VARCHAR(36) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
