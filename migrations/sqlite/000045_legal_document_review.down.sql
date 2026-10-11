DROP TABLE IF EXISTS legal_document_links;
ALTER TABLE legal_documents DROP COLUMN source_pending;
ALTER TABLE legal_documents DROP COLUMN reviewed_at;
ALTER TABLE legal_documents DROP COLUMN reviewed_by;
