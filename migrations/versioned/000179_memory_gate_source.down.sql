-- Rollback for 000179_memory_gate_source: the daily re-check judges every
-- distilled row by the rules again.
ALTER TABLE memory_items DROP COLUMN IF EXISTS gate_source;
