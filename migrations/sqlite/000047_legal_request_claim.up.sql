-- Mirrors versioned migration 000189.
-- Operator lease so several people do not fetch the same missing document.
ALTER TABLE legal_document_requests ADD COLUMN claimed_by VARCHAR(36) NOT NULL DEFAULT '';
ALTER TABLE legal_document_requests ADD COLUMN claimed_at DATETIME;
