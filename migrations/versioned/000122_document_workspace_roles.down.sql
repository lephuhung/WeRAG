-- Rollback for 000122_document_workspace_roles. Sources have no editor
-- file a target could use, so they are soft-deleted before the role
-- column goes.
UPDATE document_workspaces SET deleted_at = CURRENT_TIMESTAMP
WHERE deleted_at IS NULL AND role = 'source';

DROP TABLE IF EXISTS document_workspace_texts;

ALTER TABLE document_workspaces DROP COLUMN IF EXISTS text_status;
ALTER TABLE document_workspaces DROP COLUMN IF EXISTS role;
