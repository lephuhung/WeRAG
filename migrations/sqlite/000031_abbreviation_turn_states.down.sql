-- Rollback for 000031_abbreviation_turn_states (mirrors versioned 000114).
-- Drops only the three new tables in foreign-key order. Legacy tables
-- (abbreviations, sessions, messages) are untouched.
DROP TABLE IF EXISTS abbreviation_turn_messages;
DROP TABLE IF EXISTS abbreviation_suggestion_locks;
DROP TABLE IF EXISTS abbreviation_turn_states;
