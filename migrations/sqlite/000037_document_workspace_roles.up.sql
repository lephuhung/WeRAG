-- Mirrors versioned migration 000122_document_workspace_roles: a role per
-- document (target = editor tab, source = chat upload, looked up only) and
-- the stored text of the sources.
ALTER TABLE document_workspaces ADD COLUMN role VARCHAR(16) NOT NULL DEFAULT 'target';
ALTER TABLE document_workspaces ADD COLUMN text_status VARCHAR(16) NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS document_workspace_texts (
    workspace_id VARCHAR(36) NOT NULL PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    chunks TEXT NOT NULL DEFAULT '[]',
    token_count INTEGER NOT NULL DEFAULT 0,
    chunk_count INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_document_workspace_texts_tenant ON document_workspace_texts(tenant_id);
