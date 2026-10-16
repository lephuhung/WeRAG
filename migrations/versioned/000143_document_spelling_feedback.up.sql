-- Verdicts on the document assistant's spelling underlines: "not a
-- mistake" from the user (the spelling list's button, which also keeps the
-- text from being underlined again in a paragraph with the same text) and
-- the findings the verifier model rejected. Each row keeps the paragraph,
-- the flagged text and the model's correction, as labelled training data
-- for the spell-check model.
CREATE TABLE IF NOT EXISTS document_spelling_feedback (
    id              BIGSERIAL PRIMARY KEY,
    tenant_id       BIGINT       NOT NULL,
    user_id         VARCHAR(36)  NOT NULL DEFAULT '',
    session_id      VARCHAR(36)  NOT NULL DEFAULT '',
    document_id     VARCHAR(36)  NOT NULL DEFAULT '',
    paragraph       TEXT         NOT NULL,
    paragraph_hash  VARCHAR(64)  NOT NULL,
    wrong           VARCHAR(255) NOT NULL,
    suggestion      VARCHAR(255) NOT NULL DEFAULT '',
    text_occurrence INTEGER      NOT NULL DEFAULT 1,
    verdict         VARCHAR(20)  NOT NULL,
    source          VARCHAR(20)  NOT NULL,
    model           VARCHAR(255) NOT NULL DEFAULT '',
    note            TEXT         NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_document_spelling_feedback_paragraph
    ON document_spelling_feedback (tenant_id, paragraph_hash);
CREATE INDEX IF NOT EXISTS idx_document_spelling_feedback_created
    ON document_spelling_feedback (created_at);
