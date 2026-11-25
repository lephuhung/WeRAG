DROP INDEX IF EXISTS idx_document_spelling_feedback_review;
DROP INDEX IF EXISTS idx_document_format_feedback_review;

ALTER TABLE document_spelling_feedback
    DROP COLUMN IF EXISTS review_verdict,
    DROP COLUMN IF EXISTS review_correction,
    DROP COLUMN IF EXISTS review_note,
    DROP COLUMN IF EXISTS reviewed_by,
    DROP COLUMN IF EXISTS reviewed_at;

ALTER TABLE document_format_feedback
    DROP COLUMN IF EXISTS review_verdict,
    DROP COLUMN IF EXISTS review_correction,
    DROP COLUMN IF EXISTS review_note,
    DROP COLUMN IF EXISTS reviewed_by,
    DROP COLUMN IF EXISTS reviewed_at;
