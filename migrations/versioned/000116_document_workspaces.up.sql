-- Migration: 000116_document_workspaces
-- The editable .docx of a chat session for the document-assistant agent
-- (embedded ONLYOFFICE editor + AI tracked-change edits).
--
--   - original_ref / current_ref: resource:// handles of the uploaded copy and
--     of the latest version. Only these two files are ever referenced.
--   - revision: bumped on every write outside the editor (AI edit); the
--     ONLYOFFICE document key is "<id>-<revision>".
--   - save_count: editor saves (callback status 2/6); never changes the key.
--
-- One workspace per live session: the unique index ignores soft-deleted rows.

CREATE TABLE IF NOT EXISTS document_workspaces (
    id VARCHAR(36) NOT NULL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    session_id VARCHAR(36) NOT NULL,
    user_id VARCHAR(36) NOT NULL DEFAULT '',
    original_ref TEXT NOT NULL,
    current_ref TEXT NOT NULL,
    file_name VARCHAR(1024) NOT NULL,
    file_type VARCHAR(16) NOT NULL DEFAULT 'docx',
    file_size BIGINT NOT NULL DEFAULT 0,
    revision INTEGER NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL DEFAULT 'open',
    save_count INTEGER NOT NULL DEFAULT 0,
    last_saved_at TIMESTAMP NULL,
    closed_at TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_document_workspaces_session
    ON document_workspaces(session_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_document_workspaces_tenant ON document_workspaces(tenant_id);
CREATE INDEX IF NOT EXISTS idx_document_workspaces_status ON document_workspaces(status);
CREATE INDEX IF NOT EXISTS idx_document_workspaces_deleted_at ON document_workspaces(deleted_at);
