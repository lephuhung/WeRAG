-- Migration: 000122_document_workspace_roles
-- Every document of a document-assistant session has a role:
--
--   - target: the Word file open in an editor tab (every row so far).
--   - source: a file uploaded at chat (docx, pdf, xlsx, scans, …): no editor
--     tab, never format-checked or edited, only looked up.
--
-- text_status tracks the parsed text of a source ('' for a target):
-- processing until the upload is parsed, then ready or failed. The text
-- itself lives in document_workspace_texts so listing a session's
-- documents (every chat turn) never loads it; it is copied from the
-- temporary attachment, so it outlives that attachment's 24h TTL.

ALTER TABLE document_workspaces ADD COLUMN IF NOT EXISTS role VARCHAR(16) NOT NULL DEFAULT 'target';
ALTER TABLE document_workspaces ADD COLUMN IF NOT EXISTS text_status VARCHAR(16) NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS document_workspace_texts (
    workspace_id VARCHAR(36) NOT NULL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    chunks JSONB NOT NULL DEFAULT '[]'::jsonb,
    token_count INTEGER NOT NULL DEFAULT 0,
    chunk_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_document_workspace_texts_tenant ON document_workspace_texts(tenant_id);
