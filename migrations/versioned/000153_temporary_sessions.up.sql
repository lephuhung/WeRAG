-- Temporary sessions: nothing is kept unless someone chats.
--
-- POST /sessions creates a session with is_temporary = true (web chat, the
-- document assistant and the Word add-in all open a session before the
-- first message). The first message of the session, or of one of its
-- sub-chats, clears the flag (messageRepository.CreateMessage). A session
-- that is still temporary after a quiet period is soft-deleted, exactly like
-- the Delete button: 1 hour for a plain chat, 24 hours when a document is
-- open in it (the add-in keeps syncing the document while it is in use, so
-- a working session never times out).
--
-- Existing rows default to false: they are never touched by the purge.
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS is_temporary BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_sessions_temporary
    ON sessions (updated_at)
    WHERE is_temporary AND deleted_at IS NULL;

-- Soft-deletes the temporary sessions idle past their TTL and drops their
-- per-session usage counters (sessionRepository.dropSessionCounters does the
-- same on a manual delete). Returns how many sessions were removed.
-- document_workspaces and temporary_documents timestamps are timestamp
-- without time zone holding UTC.
CREATE OR REPLACE FUNCTION purge_expired_temporary_sessions(
    plain_ttl INTERVAL DEFAULT INTERVAL '1 hour',
    document_ttl INTERVAL DEFAULT INTERVAL '24 hours'
) RETURNS INTEGER
LANGUAGE plpgsql AS $$
DECLARE
    removed INTEGER;
BEGIN
    WITH activity AS (
        SELECT s.id,
               GREATEST(
                   s.updated_at,
                   (SELECT MAX(w.updated_at AT TIME ZONE 'UTC') FROM document_workspaces w
                     WHERE w.session_id = s.id AND w.deleted_at IS NULL),
                   (SELECT MAX(d.created_at AT TIME ZONE 'UTC') FROM temporary_documents d WHERE d.session_id = s.id)
               ) AS last_active,
               EXISTS (SELECT 1 FROM document_workspaces w
                        WHERE w.session_id = s.id AND w.deleted_at IS NULL) AS has_document
          FROM sessions s
         WHERE s.is_temporary AND s.deleted_at IS NULL
    ), expired AS (
        UPDATE sessions s
           SET deleted_at = NOW()
          FROM activity a
         WHERE s.id = a.id
           AND s.is_temporary AND s.deleted_at IS NULL
           -- a sub-chat with messages keeps its root (the flag is cleared
           -- then, this is only a guard against a missed clear)
           AND NOT EXISTS (SELECT 1 FROM sessions c
                            WHERE c.chat_root_id = s.id AND c.deleted_at IS NULL)
           AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.session_id = s.id)
           AND a.last_active < NOW() - CASE WHEN a.has_document THEN document_ttl ELSE plain_ttl END
        RETURNING s.id
    ), counters AS (
        DELETE FROM usage_counters u
         USING expired e
         WHERE u.scope_type = 'session' AND u.scope_id = e.id
    )
    SELECT COUNT(*) INTO removed FROM expired;
    RETURN removed;
END;
$$;

-- Every new session sweeps the expired ones, so the purge needs no scheduler;
-- the backend also calls the function every 10 minutes for quiet hours.
CREATE OR REPLACE FUNCTION trg_purge_expired_temporary_sessions() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM purge_expired_temporary_sessions();
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS sessions_purge_expired_temporary ON sessions;
CREATE TRIGGER sessions_purge_expired_temporary
    AFTER INSERT ON sessions
    FOR EACH STATEMENT
    EXECUTE FUNCTION trg_purge_expired_temporary_sessions();
