-- Mirrors versioned migration 000128_message_feedback.
ALTER TABLE messages ADD COLUMN feedback INTEGER NOT NULL DEFAULT 0;
ALTER TABLE messages ADD COLUMN feedback_comment TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN feedback_at DATETIME;
