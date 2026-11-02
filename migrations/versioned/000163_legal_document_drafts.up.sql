-- Migration: 000163_legal_document_drafts
-- OCR drafts of legal documents vbpl.vn publishes only as a scanned PDF.
-- A scan is never embedded as raw OCR: the PDF is fetched and OCRed into a
-- draft, the SuperAdmin edits it in the embedded editor until it reads like
-- an HTML original and approves it; only then is it converted to vbpl-style
-- HTML and ingested. One draft per catalogue document (legal_document_id);
-- a failed draft is deleted when the document is fetched again. Requests get
-- two more states: 'fetched' (a draft exists) and 'reviewing' (it is open).
DO $$ BEGIN RAISE NOTICE '[Migration 000163] Creating legal_document_drafts'; END $$;

CREATE TABLE IF NOT EXISTS legal_document_drafts (
    id                    VARCHAR(36)  PRIMARY KEY,
    request_id            VARCHAR(36)  NOT NULL DEFAULT '',
    legal_document_id     VARCHAR(64)  NOT NULL,
    document_number       VARCHAR(128) NOT NULL DEFAULT '',
    doc_type              VARCHAR(64)  NOT NULL DEFAULT '',
    title                 TEXT         NOT NULL DEFAULT '',
    pdf_file_name         TEXT         NOT NULL DEFAULT '',
    pdf_ref               TEXT         NOT NULL DEFAULT '',
    pdf_bytes             BIGINT       NOT NULL DEFAULT 0,
    ocr_markdown          TEXT         NOT NULL DEFAULT '',
    ocr_pages             INT          NOT NULL DEFAULT 0,
    ocr_engine            VARCHAR(64)  NOT NULL DEFAULT '',
    status                VARCHAR(16)  NOT NULL DEFAULT 'fetching',
    error                 TEXT         NOT NULL DEFAULT '',
    session_id            VARCHAR(36)  NOT NULL DEFAULT '',
    attachment_id         VARCHAR(36)  NOT NULL DEFAULT '',
    workspace_id          VARCHAR(36)  NOT NULL DEFAULT '',
    structure             JSONB        NULL,
    approved_knowledge_id VARCHAR(36)  NOT NULL DEFAULT '',
    approved_kb_id        VARCHAR(36)  NOT NULL DEFAULT '',
    created_by            VARCHAR(64)  NOT NULL DEFAULT '',
    reviewed_by           VARCHAR(64)  NOT NULL DEFAULT '',
    approved_by           VARCHAR(64)  NOT NULL DEFAULT '',
    approved_at           TIMESTAMP WITH TIME ZONE NULL,
    created_at            TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at            TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_legal_document_drafts_doc
    ON legal_document_drafts (legal_document_id);
CREATE INDEX IF NOT EXISTS idx_legal_document_drafts_status
    ON legal_document_drafts (status);
