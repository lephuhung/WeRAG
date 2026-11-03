-- Which write gate let a distilled memory through: 'model' when the
-- classifier model (memory.config gate_model_id) judged the user's sentence,
-- '' for the word-list rules. The daily rules-only re-check skips 'model'
-- rows so it does not undo the model's call a day later.
ALTER TABLE memory_items ADD COLUMN IF NOT EXISTS gate_source VARCHAR(16) NOT NULL DEFAULT '';
