-- Mirrors versioned migration 000116_document_workspaces. Portable
-- differences: INTEGER instead of BIGINT, DATETIME instead of TIMESTAMP.
CREATE TABLE IF NOT EXISTS document_workspaces (
    id VARCHAR(36) NOT NULL PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    session_id VARCHAR(36) NOT NULL,
    user_id VARCHAR(36) NOT NULL DEFAULT '',
    original_ref TEXT NOT NULL,
    current_ref TEXT NOT NULL,
    file_name VARCHAR(1024) NOT NULL,
    file_type VARCHAR(16) NOT NULL DEFAULT 'docx',
    file_size INTEGER NOT NULL DEFAULT 0,
    revision INTEGER NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL DEFAULT 'open',
    save_count INTEGER NOT NULL DEFAULT 0,
    last_saved_at DATETIME NULL,
    closed_at DATETIME NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_document_workspaces_session
    ON document_workspaces(session_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_document_workspaces_tenant ON document_workspaces(tenant_id);
CREATE INDEX IF NOT EXISTS idx_document_workspaces_status ON document_workspaces(status);
CREATE INDEX IF NOT EXISTS idx_document_workspaces_deleted_at ON document_workspaces(deleted_at);
