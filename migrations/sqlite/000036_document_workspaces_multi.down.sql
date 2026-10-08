-- Rollback for 000036_document_workspaces_multi (see the versioned 000121
-- down migration): keep the earliest live document of each session.
UPDATE document_workspaces SET deleted_at = CURRENT_TIMESTAMP
WHERE deleted_at IS NULL AND EXISTS (
    SELECT 1 FROM document_workspaces o
    WHERE o.session_id = document_workspaces.session_id AND o.deleted_at IS NULL
      AND (o.created_at < document_workspaces.created_at
           OR (o.created_at = document_workspaces.created_at AND o.id < document_workspaces.id))
);

DROP INDEX IF EXISTS idx_document_workspaces_session_attachment;
DROP INDEX IF EXISTS idx_document_workspaces_session;

ALTER TABLE document_workspaces DROP COLUMN active_at;
ALTER TABLE document_workspaces DROP COLUMN position;
ALTER TABLE document_workspaces DROP COLUMN attachment_id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_document_workspaces_session
    ON document_workspaces(session_id) WHERE deleted_at IS NULL;
