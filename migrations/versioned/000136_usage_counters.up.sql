-- Migration: 000136_usage_counters
-- Usage metering (phase 1 of usage packages): one counter row per
-- (scope, metric, period). Scopes are the workspace ("tenant", scope_id =
-- tenant id) and the asking person ("user", scope_id = user id). Metrics are
-- questions (role='user' messages) and LLM tokens (tokens / prompt_tokens /
-- completion_tokens from messages.usage). Periods are Vietnam calendar days
-- (YYYY-MM-DD) and months (YYYY-MM). Rows are only ever incremented, so the
-- table doubles as the usage history for reports.
--
-- messages.usage_metered_at marks assistant rows whose token usage has been
-- folded into the counters, so a message updated several times is counted
-- once. Kept out of the Go struct on purpose: it is claimed with a raw
-- UPDATE ... WHERE usage_metered_at IS NULL.

CREATE TABLE IF NOT EXISTS usage_counters (
    scope_type VARCHAR(16)  NOT NULL,
    scope_id   VARCHAR(64)  NOT NULL,
    metric     VARCHAR(32)  NOT NULL,
    period_key VARCHAR(16)  NOT NULL,
    value      BIGINT       NOT NULL DEFAULT 0,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (scope_type, scope_id, metric, period_key)
);

CREATE INDEX IF NOT EXISTS idx_usage_counters_period
    ON usage_counters (period_key, metric);

ALTER TABLE messages ADD COLUMN IF NOT EXISTS usage_metered_at TIMESTAMP WITH TIME ZONE NULL;

-- Backfill from existing chat history so the first dashboard is not empty.
-- Questions: every user message, attributed through its session.
WITH q AS (
    SELECT s.tenant_id,
           COALESCE(s.user_id, '') AS user_id,
           (m.created_at AT TIME ZONE 'Asia/Ho_Chi_Minh') AS local_ts
    FROM messages m
    JOIN sessions s ON s.id = m.session_id
    WHERE m.role = 'user' AND m.deleted_at IS NULL
), agg AS (
    SELECT 'tenant' AS scope_type, tenant_id::text AS scope_id, 'questions' AS metric,
           to_char(local_ts, 'YYYY-MM-DD') AS period_key, COUNT(*) AS value
    FROM q GROUP BY 2, 4
    UNION ALL
    SELECT 'tenant', tenant_id::text, 'questions', to_char(local_ts, 'YYYY-MM'), COUNT(*)
    FROM q GROUP BY 2, 4
    UNION ALL
    SELECT 'user', user_id, 'questions', to_char(local_ts, 'YYYY-MM-DD'), COUNT(*)
    FROM q WHERE user_id <> '' AND user_id NOT LIKE 'system-%' GROUP BY 2, 4
    UNION ALL
    SELECT 'user', user_id, 'questions', to_char(local_ts, 'YYYY-MM'), COUNT(*)
    FROM q WHERE user_id <> '' AND user_id NOT LIKE 'system-%' GROUP BY 2, 4
)
INSERT INTO usage_counters (scope_type, scope_id, metric, period_key, value)
SELECT scope_type, scope_id, metric, period_key, value FROM agg
ON CONFLICT (scope_type, scope_id, metric, period_key)
DO UPDATE SET value = EXCLUDED.value, updated_at = CURRENT_TIMESTAMP;

-- Tokens: assistant messages that recorded usage (agent turns so far;
-- quick-answer turns start recording with this release).
WITH t AS (
    SELECT s.tenant_id,
           COALESCE(s.user_id, '') AS user_id,
           (m.created_at AT TIME ZONE 'Asia/Ho_Chi_Minh') AS local_ts,
           COALESCE((m.usage->>'total_tokens')::bigint, 0)      AS tokens,
           COALESCE((m.usage->>'prompt_tokens')::bigint, 0)     AS prompt_tokens,
           COALESCE((m.usage->>'completion_tokens')::bigint, 0) AS completion_tokens
    FROM messages m
    JOIN sessions s ON s.id = m.session_id
    WHERE m.role = 'assistant' AND m.deleted_at IS NULL
      AND m.usage IS NOT NULL AND jsonb_typeof(m.usage) = 'object'
), metrics AS (
    SELECT tenant_id, user_id, local_ts, 'tokens' AS metric, tokens AS value FROM t
    UNION ALL SELECT tenant_id, user_id, local_ts, 'prompt_tokens', prompt_tokens FROM t
    UNION ALL SELECT tenant_id, user_id, local_ts, 'completion_tokens', completion_tokens FROM t
), agg AS (
    SELECT 'tenant' AS scope_type, tenant_id::text AS scope_id, metric,
           to_char(local_ts, 'YYYY-MM-DD') AS period_key, SUM(value) AS value
    FROM metrics GROUP BY 2, 3, 4
    UNION ALL
    SELECT 'tenant', tenant_id::text, metric, to_char(local_ts, 'YYYY-MM'), SUM(value)
    FROM metrics GROUP BY 2, 3, 4
    UNION ALL
    SELECT 'user', user_id, metric, to_char(local_ts, 'YYYY-MM-DD'), SUM(value)
    FROM metrics WHERE user_id <> '' AND user_id NOT LIKE 'system-%' GROUP BY 2, 3, 4
    UNION ALL
    SELECT 'user', user_id, metric, to_char(local_ts, 'YYYY-MM'), SUM(value)
    FROM metrics WHERE user_id <> '' AND user_id NOT LIKE 'system-%' GROUP BY 2, 3, 4
)
INSERT INTO usage_counters (scope_type, scope_id, metric, period_key, value)
SELECT scope_type, scope_id, metric, period_key, value FROM agg
ON CONFLICT (scope_type, scope_id, metric, period_key)
DO UPDATE SET value = EXCLUDED.value, updated_at = CURRENT_TIMESTAMP;

UPDATE messages SET usage_metered_at = created_at
WHERE role = 'assistant' AND usage_metered_at IS NULL
  AND usage IS NOT NULL AND jsonb_typeof(usage) = 'object';
