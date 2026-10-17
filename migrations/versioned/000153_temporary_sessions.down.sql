DROP TRIGGER IF EXISTS sessions_purge_expired_temporary ON sessions;
DROP FUNCTION IF EXISTS trg_purge_expired_temporary_sessions();
DROP FUNCTION IF EXISTS purge_expired_temporary_sessions(INTERVAL, INTERVAL);
DROP INDEX IF EXISTS idx_sessions_temporary;
ALTER TABLE sessions DROP COLUMN IF EXISTS is_temporary;
