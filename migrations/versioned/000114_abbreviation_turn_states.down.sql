-- Rollback for 000114_abbreviation_turn_states.
--
-- Drops only the three new tables in foreign-key order (links and locks
-- before states; indexes drop with their tables). Legacy tables
-- (abbreviations, sessions, messages) are untouched. Turn history is lost
-- by design; re-apply 000114 to recreate the empty schema.

DROP TABLE IF EXISTS abbreviation_turn_messages;
DROP TABLE IF EXISTS abbreviation_suggestion_locks;
DROP TABLE IF EXISTS abbreviation_turn_states;
