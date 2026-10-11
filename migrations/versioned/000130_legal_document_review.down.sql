DROP TABLE IF EXISTS legal_document_links;
ALTER TABLE legal_documents DROP COLUMN IF EXISTS source_pending;
ALTER TABLE legal_documents DROP COLUMN IF EXISTS reviewed_at;
ALTER TABLE legal_documents DROP COLUMN IF EXISTS reviewed_by;
