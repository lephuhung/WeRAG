-- Users' feedback on the NĐ30 format check report (the report's "Phản hồi"
-- buttons): a finding reported as wrong, a document that is no văn bản
-- hành chính under NĐ30, a wrong document type, or a free note. Each row
-- keeps the report it was given on (scope, type, findings) and the checked
-- file's resource ref, as labelled data for tuning the checker later.
CREATE TABLE IF NOT EXISTS document_format_feedback (
    id                BIGSERIAL PRIMARY KEY,
    tenant_id         BIGINT        NOT NULL,
    user_id           VARCHAR(36)   NOT NULL DEFAULT '',
    session_id        VARCHAR(36)   NOT NULL DEFAULT '',
    document_id       VARCHAR(36)   NOT NULL DEFAULT '',
    file_name         VARCHAR(1024) NOT NULL DEFAULT '',
    document_ref      TEXT          NOT NULL DEFAULT '',
    document_revision INTEGER,
    report_id         VARCHAR(64)   NOT NULL,
    document_type     VARCHAR(64)   NOT NULL DEFAULT '',
    scope             VARCHAR(16)   NOT NULL DEFAULT '',
    finding_id        VARCHAR(255)  NOT NULL DEFAULT '',
    check_id          VARCHAR(255)  NOT NULL DEFAULT '',
    finding_title     TEXT          NOT NULL DEFAULT '',
    verdict           VARCHAR(20)   NOT NULL,
    note              TEXT          NOT NULL DEFAULT '',
    model             VARCHAR(255)  NOT NULL DEFAULT '',
    report            JSONB         NOT NULL DEFAULT '{}'::jsonb,
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_document_format_feedback_created
    ON document_format_feedback (created_at);
CREATE INDEX IF NOT EXISTS idx_document_format_feedback_verdict
    ON document_format_feedback (verdict, check_id);
