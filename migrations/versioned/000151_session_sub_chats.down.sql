DROP INDEX IF EXISTS idx_sessions_chat_root_id;
ALTER TABLE sessions DROP COLUMN IF EXISTS chat_root_id;
