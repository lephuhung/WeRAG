-- Migration: 000120_document_revisions
-- Snapshot timeline of a document workspace (document assistant). AI edits
-- are applied inside the editor, so each AI turn, manual snapshot, editor
-- close and restore records a revision pointing at a stored version of the
-- file (resource:// handle) the user can restore.
CREATE TABLE IF NOT EXISTS document_revisions (
    id VARCHAR(36) NOT NULL PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    tenant_id BIGINT NOT NULL,
    seq INTEGER NOT NULL,
    ref TEXT NOT NULL,
    label VARCHAR(512) NOT NULL DEFAULT '',
    source VARCHAR(16) NOT NULL,
    file_size BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_document_revisions_source CHECK (source IN ('ai', 'manual', 'close', 'restore'))
);

CREATE INDEX IF NOT EXISTS idx_document_revisions_workspace ON document_revisions(workspace_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_document_revisions_workspace_seq ON document_revisions(workspace_id, seq);
