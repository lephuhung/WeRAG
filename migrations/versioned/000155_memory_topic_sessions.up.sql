-- Memory interests recur per conversation, not per extraction segment (2026-10-17).
--
-- memory_topic_stats.hits counted every extraction segment that named a
-- subject, so one long conversation (or one evaluation run) promoted a
-- passing question into a "long-term focus". The promotion now requires the
-- subject to come up in distinct sessions on distinct days; the raw hit
-- counter stays for display and ordering.
--
-- Idempotent: every statement can run twice.

DO $$ BEGIN RAISE NOTICE '[Migration 000155] memory_topic_stats per-session recurrence'; END $$;

ALTER TABLE memory_topic_stats
    ADD COLUMN IF NOT EXISTS sessions jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS days jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS session_hits integer NOT NULL DEFAULT 0;
