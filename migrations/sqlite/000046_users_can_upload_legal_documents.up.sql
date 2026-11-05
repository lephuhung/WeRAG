-- Mirrors versioned migration 000188.
-- users.can_upload_legal_documents: the narrow permission for the
-- external-source legal-document feed (push, request queue, close as added).
ALTER TABLE users ADD COLUMN can_upload_legal_documents BOOLEAN NOT NULL DEFAULT 0;
-- No index: chỉ đọc từ user row đã load theo id.
