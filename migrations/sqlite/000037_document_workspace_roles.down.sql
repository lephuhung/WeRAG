-- Rollback for 000037_document_workspace_roles (see the versioned 000122
-- down migration): sources are soft-deleted first.
UPDATE document_workspaces SET deleted_at = CURRENT_TIMESTAMP
WHERE deleted_at IS NULL AND role = 'source';

DROP TABLE IF EXISTS document_workspace_texts;

ALTER TABLE document_workspaces DROP COLUMN text_status;
ALTER TABLE document_workspaces DROP COLUMN role;
