-- Migration: 000132_memory_items_announced
-- Memories the system distills on its own are used straight away and told
-- to the user on their next turn ("I just saved ...; tell me if it's wrong"),
-- instead of waiting in a review queue nobody can open. announced_at records
-- when that notice went out; NULL means not yet told.
--
-- Everything stored before this change counts as already announced.

ALTER TABLE memory_items ADD COLUMN IF NOT EXISTS announced_at TIMESTAMP NULL;

UPDATE memory_items SET announced_at = created_at WHERE announced_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_memory_items_unannounced
    ON memory_items(tenant_id, subject_id)
    WHERE announced_at IS NULL;
