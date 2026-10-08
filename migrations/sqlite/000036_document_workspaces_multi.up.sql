-- Mirrors versioned migration 000121_document_workspaces_multi: several
-- editable documents per session (attachment_id, position, active_at).
DROP INDEX IF EXISTS idx_document_workspaces_session;

ALTER TABLE document_workspaces ADD COLUMN attachment_id VARCHAR(36) NOT NULL DEFAULT '';
ALTER TABLE document_workspaces ADD COLUMN position INTEGER NOT NULL DEFAULT 1;
ALTER TABLE document_workspaces ADD COLUMN active_at DATETIME NULL;

UPDATE document_workspaces SET active_at = created_at WHERE active_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_document_workspaces_session ON document_workspaces(session_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_document_workspaces_session_attachment
    ON document_workspaces(session_id, attachment_id)
    WHERE deleted_at IS NULL AND attachment_id <> '';
