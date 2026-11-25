-- What the user did with a live-spelling underline, seen by the editor
-- after an edit (the assistant never applies a correction itself): verdict
-- 'accepted' = the user typed the model's suggestion over the flagged text,
-- 'fixed' = the user typed something else, kept here as correction (empty
-- for every other verdict). Positive samples for the spell-check training
-- set next to the 'not_error' ones; only 'not_error' rows suppress an
-- underline.
ALTER TABLE document_spelling_feedback
    ADD COLUMN IF NOT EXISTS correction VARCHAR(255) NOT NULL DEFAULT '';
