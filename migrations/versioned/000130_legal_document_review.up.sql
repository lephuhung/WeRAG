-- Migration: 000130_legal_document_review
-- SuperAdmin review of the legal identity of documents in platform public
-- KBs. legal_documents gains review fields: a reviewed row is the curated
-- truth and a vbpl re-import no longer overwrites it — differing portal
-- values are parked in source_pending for the reviewer instead. Rows the
-- reviewer adds by hand (documents vbpl does not list) use source 'manual'.
-- legal_document_links ties one knowledge to its catalogue row together
-- with the parser's suggestion and the review state.
DO $$ BEGIN RAISE NOTICE '[Migration 000130] Adding legal document review'; END $$;

ALTER TABLE legal_documents ADD COLUMN IF NOT EXISTS reviewed_by    VARCHAR(36) NOT NULL DEFAULT '';
ALTER TABLE legal_documents ADD COLUMN IF NOT EXISTS reviewed_at    TIMESTAMP WITH TIME ZONE;
ALTER TABLE legal_documents ADD COLUMN IF NOT EXISTS source_pending JSONB;

CREATE TABLE IF NOT EXISTS legal_document_links (
    knowledge_id      VARCHAR(36) PRIMARY KEY,
    knowledge_base_id VARCHAR(36) NOT NULL,
    legal_document_id VARCHAR(64) NOT NULL DEFAULT '',
    review_status     VARCHAR(16) NOT NULL DEFAULT 'pending',
    suggestion        JSONB,
    reviewed_by       VARCHAR(36) NOT NULL DEFAULT '',
    reviewed_at       TIMESTAMP WITH TIME ZONE,
    created_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS ix_legal_document_links_doc ON legal_document_links (legal_document_id);
CREATE INDEX IF NOT EXISTS ix_legal_document_links_status ON legal_document_links (review_status);
