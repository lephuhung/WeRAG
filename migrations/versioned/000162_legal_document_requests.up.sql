-- Migration: 000162_legal_document_requests
-- Requests to add the full text of a legal document the system lacks. A
-- chat question naming a full số hiệu that no knowledge base holds records
-- one; the SuperAdmin queue (/system/legal-requests) ingests it from
-- vbpl.vn, marks it added by hand or rejects it. One row per (number_key,
-- legal_document_id): legal_document_id is the catalogue (vbpl.vn) id, ''
-- when the catalogue does not know the number. source names where the
-- identity came from ('catalogue', '' unknown); detail keeps a source's
-- evidence so later sources need no new columns.
DO $$ BEGIN RAISE NOTICE '[Migration 000162] Creating legal_document_requests'; END $$;

CREATE TABLE IF NOT EXISTS legal_document_requests (
    id                 VARCHAR(36)  PRIMARY KEY,
    number_key         VARCHAR(128) NOT NULL,
    document_number    VARCHAR(128) NOT NULL DEFAULT '',
    doc_type           VARCHAR(64)  NOT NULL DEFAULT '',
    title              TEXT         NOT NULL DEFAULT '',
    legal_document_id  VARCHAR(64)  NOT NULL DEFAULT '',
    source             VARCHAR(32)  NOT NULL DEFAULT '',
    status             VARCHAR(16)  NOT NULL DEFAULT 'requested',
    request_count      INT          NOT NULL DEFAULT 1,
    first_requested_at TIMESTAMP WITH TIME ZONE NOT NULL,
    last_requested_at  TIMESTAMP WITH TIME ZONE NOT NULL,
    last_tenant_id     BIGINT       NOT NULL DEFAULT 0,
    last_user_id       VARCHAR(36)  NOT NULL DEFAULT '',
    last_session_id    VARCHAR(36)  NOT NULL DEFAULT '',
    last_query         TEXT         NOT NULL DEFAULT '',
    knowledge_id       VARCHAR(36)  NOT NULL DEFAULT '',
    knowledge_base_id  VARCHAR(36)  NOT NULL DEFAULT '',
    decided_by         VARCHAR(36)  NOT NULL DEFAULT '',
    decided_at         TIMESTAMP WITH TIME ZONE NULL,
    decision_note      TEXT         NOT NULL DEFAULT '',
    detail             JSONB        NULL,
    created_at         TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_legal_document_requests_doc
    ON legal_document_requests (number_key, legal_document_id);
CREATE INDEX IF NOT EXISTS idx_legal_document_requests_status
    ON legal_document_requests (status);
CREATE INDEX IF NOT EXISTS idx_legal_document_requests_last_requested_at
    ON legal_document_requests (last_requested_at DESC);
