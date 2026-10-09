-- Mirrors versioned migration 000125_document_workspace_editor_kind.
ALTER TABLE document_workspaces ADD COLUMN editor_kind VARCHAR(16) NOT NULL DEFAULT 'onlyoffice';
