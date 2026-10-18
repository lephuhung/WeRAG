-- Back to timestamp without time zone holding UTC, for the columns 000157
-- converted (the +08 corrections of the up migration are kept).
DO $$
DECLARE
    col TEXT[];
BEGIN
    FOREACH col SLICE 1 IN ARRAY ARRAY[
        ['data_sources', 'created_at'], ['data_sources', 'deleted_at'],
        ['data_sources', 'last_sync_at'], ['data_sources', 'updated_at'],
        ['document_revisions', 'created_at'],
        ['document_workspace_texts', 'created_at'], ['document_workspace_texts', 'updated_at'],
        ['document_workspaces', 'active_at'], ['document_workspaces', 'closed_at'],
        ['document_workspaces', 'created_at'], ['document_workspaces', 'deleted_at'],
        ['document_workspaces', 'last_saved_at'], ['document_workspaces', 'updated_at'],
        ['mcp_oauth_clients', 'created_at'], ['mcp_oauth_clients', 'updated_at'],
        ['mcp_oauth_tokens', 'created_at'], ['mcp_oauth_tokens', 'expires_at'],
        ['mcp_oauth_tokens', 'updated_at'],
        ['mcp_services', 'created_at'], ['mcp_services', 'deleted_at'], ['mcp_services', 'updated_at'],
        ['mcp_tool_approvals', 'created_at'], ['mcp_tool_approvals', 'updated_at'],
        ['memory_items', 'announced_at'],
        ['resource_bindings', 'created_at'],
        ['resources', 'created_at'], ['resources', 'deleted_at'],
        ['resources', 'expires_at'], ['resources', 'updated_at'],
        ['storage_backends', 'created_at'], ['storage_backends', 'deleted_at'],
        ['storage_backends', 'updated_at'],
        ['sync_logs', 'created_at'], ['sync_logs', 'finished_at'],
        ['sync_logs', 'started_at'], ['sync_logs', 'updated_at'],
        ['temporary_documents', 'created_at'], ['temporary_documents', 'deleted_at'],
        ['temporary_documents', 'expires_at'], ['temporary_documents', 'ready_at'],
        ['temporary_documents', 'started_at'], ['temporary_documents', 'updated_at'],
        ['vector_stores', 'created_at'], ['vector_stores', 'deleted_at'], ['vector_stores', 'updated_at'],
        ['web_search_providers', 'created_at'], ['web_search_providers', 'deleted_at'],
        ['web_search_providers', 'updated_at']
    ]
    LOOP
        IF EXISTS (SELECT 1 FROM information_schema.columns
                    WHERE table_schema = current_schema()
                      AND table_name = col[1] AND column_name = col[2]) THEN
            EXECUTE format(
                'ALTER TABLE %I ALTER COLUMN %I TYPE TIMESTAMP WITHOUT TIME ZONE USING %I AT TIME ZONE ''UTC''',
                col[1], col[2], col[2]);
        END IF;
    END LOOP;
END $$;

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
