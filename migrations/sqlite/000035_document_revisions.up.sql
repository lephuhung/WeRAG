-- Mirrors versioned migration 000120_document_revisions. Portable
-- differences: INTEGER instead of BIGINT, DATETIME instead of TIMESTAMP.
CREATE TABLE IF NOT EXISTS document_revisions (
    id VARCHAR(36) NOT NULL PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    tenant_id INTEGER NOT NULL,
    seq INTEGER NOT NULL,
    ref TEXT NOT NULL,
    label VARCHAR(512) NOT NULL DEFAULT '',
    source VARCHAR(16) NOT NULL,
    file_size INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_document_revisions_source CHECK (source IN ('ai', 'manual', 'close', 'restore'))
);

CREATE INDEX IF NOT EXISTS idx_document_revisions_workspace ON document_revisions(workspace_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_document_revisions_workspace_seq ON document_revisions(workspace_id, seq);
