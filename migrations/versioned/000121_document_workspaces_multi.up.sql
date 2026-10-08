-- Migration: 000121_document_workspaces_multi
-- A document-assistant session may hold several editable documents (one
-- editor tab each, at most 4, enforced by the service):
--
--   - attachment_id: the session upload the document was opened from; one
--     live document per upload.
--   - position: 1-based order the documents were opened in, never reused in
--     a session; the agent names them vb<position>.
--   - active_at: when the user last switched to the document's tab; the
--     most recent one is the session's active document.

DROP INDEX IF EXISTS idx_document_workspaces_session;

ALTER TABLE document_workspaces ADD COLUMN IF NOT EXISTS attachment_id VARCHAR(36) NOT NULL DEFAULT '';
ALTER TABLE document_workspaces ADD COLUMN IF NOT EXISTS position INTEGER NOT NULL DEFAULT 1;
ALTER TABLE document_workspaces ADD COLUMN IF NOT EXISTS active_at TIMESTAMP NULL;

UPDATE document_workspaces SET active_at = created_at WHERE active_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_document_workspaces_session ON document_workspaces(session_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_document_workspaces_session_attachment
    ON document_workspaces(session_id, attachment_id)
    WHERE deleted_at IS NULL AND attachment_id <> '';
