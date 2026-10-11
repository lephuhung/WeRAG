-- Mirrors versioned migration 000129_legal_catalog.
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
    source_updated_at DATETIME,
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS ix_legal_documents_number_key ON legal_documents (number_key);

CREATE TABLE IF NOT EXISTS legal_document_relations (
    from_id      VARCHAR(64) NOT NULL,
    to_id        VARCHAR(64) NOT NULL,
    relation     VARCHAR(32) NOT NULL,
    source_label VARCHAR(128) NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (from_id, to_id, relation)
);
CREATE INDEX IF NOT EXISTS ix_legal_document_relations_to ON legal_document_relations (to_id);
