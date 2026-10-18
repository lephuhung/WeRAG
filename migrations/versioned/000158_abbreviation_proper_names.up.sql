-- A dictionary row can be a proper name: a unit code or name typed as it is
-- (PV01, PA05) that the chat gate must neither ask about nor expand. The
-- user's "Giữ nguyên" answer adds such a row, active at once.
DO $$ BEGIN RAISE NOTICE '[Migration 000158] Adding abbreviations.is_proper_name...'; END $$;

ALTER TABLE abbreviations ADD COLUMN IF NOT EXISTS is_proper_name BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN abbreviations.is_proper_name IS
    'Tên riêng (mã/tên đơn vị): kept as typed, never asked about or expanded; added active by the user''s "Giữ nguyên".';

-- one active proper name per word (concurrent "Giữ nguyên" answers)
CREATE UNIQUE INDEX IF NOT EXISTS ux_abbreviations_active_proper_name
    ON abbreviations (lower(short_form))
    WHERE is_proper_name AND is_active AND deleted_at IS NULL;
