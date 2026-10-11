-- Mirrors versioned migration 000130_legal_document_review.
ALTER TABLE legal_documents ADD COLUMN reviewed_by VARCHAR(36) NOT NULL DEFAULT '';
ALTER TABLE legal_documents ADD COLUMN reviewed_at DATETIME;
ALTER TABLE legal_documents ADD COLUMN source_pending TEXT;

CREATE TABLE IF NOT EXISTS legal_document_links (
    knowledge_id      VARCHAR(36) PRIMARY KEY,
    knowledge_base_id VARCHAR(36) NOT NULL,
    legal_document_id VARCHAR(64) NOT NULL DEFAULT '',
    review_status     VARCHAR(16) NOT NULL DEFAULT 'pending',
    suggestion        TEXT,
    reviewed_by       VARCHAR(36) NOT NULL DEFAULT '',
    reviewed_at       DATETIME,
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS ix_legal_document_links_doc ON legal_document_links (legal_document_id);
CREATE INDEX IF NOT EXISTS ix_legal_document_links_status ON legal_document_links (review_status);
