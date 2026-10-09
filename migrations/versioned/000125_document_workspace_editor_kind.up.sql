-- Migration: 000125_document_workspace_editor_kind
-- A target document is edited either in the embedded ONLYOFFICE editor or in
-- Microsoft Word through the WeRAG add-in, whose taskpane uploads the file.
ALTER TABLE document_workspaces ADD COLUMN IF NOT EXISTS editor_kind VARCHAR(16) NOT NULL DEFAULT 'onlyoffice';
