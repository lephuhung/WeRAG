-- Migration: 000128_message_feedback
-- Thumbs up/down (and an optional note) on an assistant answer. The row is
-- the source of truth; the same judgement is mirrored to Langfuse as the
-- "user-feedback" score on the turn's trace so training exports can filter
-- on it. 1 = helpful, -1 = not helpful, 0 = no feedback.
DO $$ BEGIN RAISE NOTICE '[Migration 000128] Adding message feedback'; END $$;

ALTER TABLE messages ADD COLUMN IF NOT EXISTS feedback         SMALLINT NOT NULL DEFAULT 0;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS feedback_comment TEXT     NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN IF NOT EXISTS feedback_at      TIMESTAMP WITH TIME ZONE;
