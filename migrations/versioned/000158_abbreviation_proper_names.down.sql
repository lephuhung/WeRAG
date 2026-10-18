DROP INDEX IF EXISTS ux_abbreviations_active_proper_name;
ALTER TABLE abbreviations DROP COLUMN IF EXISTS is_proper_name;
