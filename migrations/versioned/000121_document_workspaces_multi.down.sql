-- Rollback for 000121_document_workspaces_multi. Keeps the earliest live
-- document of each session and soft-deletes the others, so the
-- one-document-per-session index can be restored.
UPDATE document_workspaces w SET deleted_at = CURRENT_TIMESTAMP
WHERE w.deleted_at IS NULL AND EXISTS (
    SELECT 1 FROM document_workspaces o
    WHERE o.session_id = w.session_id AND o.deleted_at IS NULL
      AND (o.created_at < w.created_at OR (o.created_at = w.created_at AND o.id < w.id))
);

DROP INDEX IF EXISTS idx_document_workspaces_session_attachment;
DROP INDEX IF EXISTS idx_document_workspaces_session;

ALTER TABLE document_workspaces DROP COLUMN IF EXISTS active_at;
ALTER TABLE document_workspaces DROP COLUMN IF EXISTS position;
ALTER TABLE document_workspaces DROP COLUMN IF EXISTS attachment_id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_document_workspaces_session
    ON document_workspaces(session_id) WHERE deleted_at IS NULL;
