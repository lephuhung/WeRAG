-- Rollback for 000116_document_workspaces. Drops the workspace rows only;
-- the stored .docx objects stay in the resource catalog.
DROP TABLE IF EXISTS document_workspaces;
