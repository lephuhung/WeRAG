-- Every timestamp column becomes timestamp with time zone.
--
-- These columns were "timestamp without time zone". The driver stores the
-- wall clock of the Go value, so a time.Now() written by a process whose zone
-- is not UTC (app-dev ran TZ=Asia/Shanghai) landed hours off, while GORM's
-- own created_at/updated_at (NowFunc = UTC) were right. With time zone the
-- column holds the instant whatever zone the writer runs in; the server
-- displays it in Vietnam time (GMT+7).
--
-- Existing values are UTC except the ones written as local wall clock by a
-- process running at +07 (TZ=Asia/Ho_Chi_Minh) or +08 (Asia/Shanghai). Each
-- row is shifted back by the offset it shows, in whole hours:
--   temporary_documents.expires_at   created_at + 24 h (the default TTL)
--                                    + offset; rows with another TTL are left
--   temporary_documents.started_at / ready_at   on those rows, when at least
--                                    offset - 1 h after created_at
--   document_workspace_texts         6.5–8.5 h after the workspace was
--                                    created (texts are saved right away)
--   storage_backends.updated_at      in the future (8 h)
-- Down does not undo these corrections.
DO $$ BEGIN RAISE NOTICE '[Migration 000157] Converting timestamps to timestamptz...'; END $$;

UPDATE temporary_documents d
   SET expires_at = d.expires_at - o.shift,
       started_at = CASE WHEN d.started_at - d.created_at >= o.shift - INTERVAL '1 hour'
                         THEN d.started_at - o.shift ELSE d.started_at END,
       ready_at   = CASE WHEN d.ready_at - d.created_at >= o.shift - INTERVAL '1 hour'
                         THEN d.ready_at - o.shift ELSE d.ready_at END
  FROM (SELECT id,
               make_interval(hours => (round(extract(epoch FROM expires_at - created_at) / 3600) - 24)::int) AS shift
          FROM temporary_documents) o
 WHERE o.id = d.id
   AND o.shift IN (INTERVAL '7 hours', INTERVAL '8 hours');

UPDATE document_workspace_texts t
   SET created_at = t.created_at - o.shift,
       updated_at = t.updated_at - o.shift
  FROM (SELECT t2.workspace_id,
               make_interval(hours => round(extract(epoch FROM t2.created_at - w.created_at) / 3600)::int) AS shift
          FROM document_workspace_texts t2
          JOIN document_workspaces w ON w.id = t2.workspace_id
         WHERE t2.created_at - w.created_at BETWEEN INTERVAL '6 hours 30 minutes' AND INTERVAL '8 hours 30 minutes') o
 WHERE o.workspace_id = t.workspace_id;

UPDATE storage_backends
   SET updated_at = updated_at - INTERVAL '8 hours'
 WHERE updated_at > (NOW() AT TIME ZONE 'UTC') + INTERVAL '5 minutes';

DO $$
DECLARE
    col RECORD;
BEGIN
    FOR col IN
        SELECT c.table_name, c.column_name
          FROM information_schema.columns c
         WHERE c.table_schema = current_schema()
           AND c.data_type = 'timestamp without time zone'
           AND c.table_name IN (
               'data_sources', 'document_revisions', 'document_workspace_texts',
               'document_workspaces', 'mcp_oauth_clients', 'mcp_oauth_tokens',
               'mcp_services', 'mcp_tool_approvals', 'memory_items',
               'resource_bindings', 'resources', 'storage_backends', 'sync_logs',
               'temporary_documents', 'vector_stores', 'web_search_providers')
    LOOP
        EXECUTE format(
            'ALTER TABLE %I ALTER COLUMN %I TYPE TIMESTAMP WITH TIME ZONE USING %I AT TIME ZONE ''UTC''',
            col.table_name, col.column_name, col.column_name);
    END LOOP;
END $$;

-- The purge compared these columns through AT TIME ZONE 'UTC'; on
-- timestamptz that would turn them back into wall clocks.
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
                   (SELECT MAX(w.updated_at) FROM document_workspaces w
                     WHERE w.session_id = s.id AND w.deleted_at IS NULL),
                   (SELECT MAX(d.created_at) FROM temporary_documents d WHERE d.session_id = s.id)
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
