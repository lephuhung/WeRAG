-- Review of the spelling and format labels on the "Nhãn chính tả và thể
-- thức" page (Tenant Admin / SuperAdmin), to build a training set: the
-- reviewer confirms a real mistake (review_verdict 'error', with the right
-- form in review_correction: the model's suggestion accepted, or typed by
-- hand), says it is no mistake ('not_error') or drops an unusable sample
-- ('skip'). '' = not reviewed yet. reviewed_by is kept for audit only; the
-- JSONL export carries no user identity.
ALTER TABLE document_spelling_feedback
    ADD COLUMN IF NOT EXISTS review_verdict    VARCHAR(20)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS review_correction VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS review_note       TEXT         NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reviewed_by       VARCHAR(36)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reviewed_at       TIMESTAMPTZ  NULL;

ALTER TABLE document_format_feedback
    ADD COLUMN IF NOT EXISTS review_verdict    VARCHAR(20)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS review_correction VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS review_note       TEXT         NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reviewed_by       VARCHAR(36)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reviewed_at       TIMESTAMPTZ  NULL;

CREATE INDEX IF NOT EXISTS idx_document_spelling_feedback_review
    ON document_spelling_feedback (review_verdict);
CREATE INDEX IF NOT EXISTS idx_document_format_feedback_review
    ON document_format_feedback (review_verdict);
