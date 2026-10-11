-- Migration: 000129_legal_catalog
-- Platform-wide catalogue of Vietnamese legal documents (metadata only, no
-- full text) and the amend / replace / repeal / guide relations between
-- them, imported from vbpl.vn snapshots. It answers "is this document still
-- in force, and what changed it?" for the check_legal_status agent tool.
-- Not tenant-scoped: the catalogue is public law, the same for everyone.
DO $$ BEGIN RAISE NOTICE '[Migration 000129] Creating legal catalog tables'; END $$;

CREATE TABLE IF NOT EXISTS legal_documents (
    id                VARCHAR(64)  PRIMARY KEY,
    source            VARCHAR(32)  NOT NULL DEFAULT 'vbpl',
    document_number   VARCHAR(128) NOT NULL DEFAULT '',
    number_key        VARCHAR(128) NOT NULL DEFAULT '',
    title             TEXT         NOT NULL DEFAULT '',
    doc_type          VARCHAR(64)  NOT NULL DEFAULT '',
    issuer            VARCHAR(255) NOT NULL DEFAULT '',
    signer            VARCHAR(255) NOT NULL DEFAULT '',
    signer_title      VARCHAR(255) NOT NULL DEFAULT '',
    field             TEXT         NOT NULL DEFAULT '',
    issued_on         DATE,
    effective_on      DATE,
    expired_on        DATE,
    status            VARCHAR(64)  NOT NULL DEFAULT '',
    source_updated_at TIMESTAMP WITH TIME ZONE,
    created_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS ix_legal_documents_number_key ON legal_documents (number_key);

-- One directed edge per (from, to, relation): "from_id <relation> to_id",
-- e.g. from_id replaces to_id. source_label keeps the portal's wording.
CREATE TABLE IF NOT EXISTS legal_document_relations (
    from_id      VARCHAR(64) NOT NULL,
    to_id        VARCHAR(64) NOT NULL,
    relation     VARCHAR(32) NOT NULL,
    source_label VARCHAR(128) NOT NULL DEFAULT '',
    created_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (from_id, to_id, relation)
);

CREATE INDEX IF NOT EXISTS ix_legal_document_relations_to ON legal_document_relations (to_id);
