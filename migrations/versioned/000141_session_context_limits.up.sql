-- Migration: 000141_session_context_limits
-- Per-turn and per-session caps of usage plans, stored as flat dotted keys of
-- usage_plans.features (see types.UsagePlanFeatures). A key that is absent or
-- 0 leaves the old behaviour; the keys are merged into whatever features a
-- plan already has, so SuperAdmin edits to other keys survive.

UPDATE usage_plans
SET features = COALESCE(features, '{}'::jsonb) || '{
        "agent.turn_token_cap": 300000,
        "context.history_tokens": 32768,
        "session.max_turns": 15
    }'::jsonb,
    updated_at = CURRENT_TIMESTAMP
WHERE code = 'co-ban';

UPDATE usage_plans
SET features = COALESCE(features, '{}'::jsonb) || '{
        "agent.turn_token_cap": 600000,
        "context.history_tokens": 65536,
        "session.max_turns": 30
    }'::jsonb,
    updated_at = CURRENT_TIMESTAMP
WHERE code = 'Go-1';

-- A session continued from another starts from that one's summary
-- (types.Session.ContextSeed); empty for ordinary sessions.
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS context_seed TEXT;

-- Per-session lifetime counters (scope_type 'session', period_key 'all'),
-- read only by the session.max_turns / session.max_tokens caps. Every
-- aggregate over usage_counters filters by scope_type, so these rows never
-- add into workspace or person totals. Backfilled from the stored history so
-- a session that is already long counts as long.
INSERT INTO usage_counters (scope_type, scope_id, metric, period_key, value)
SELECT 'session', m.session_id, 'questions', 'all', COUNT(*)
FROM messages m
WHERE m.role = 'user' AND m.deleted_at IS NULL
GROUP BY m.session_id
ON CONFLICT (scope_type, scope_id, metric, period_key)
DO UPDATE SET value = EXCLUDED.value, updated_at = CURRENT_TIMESTAMP;

WITH t AS (
    SELECT m.session_id,
           COALESCE((m.usage->>'total_tokens')::bigint, 0)      AS tokens,
           COALESCE((m.usage->>'prompt_tokens')::bigint, 0)     AS prompt_tokens,
           COALESCE((m.usage->>'completion_tokens')::bigint, 0) AS completion_tokens
    FROM messages m
    WHERE m.role = 'assistant' AND m.deleted_at IS NULL
      AND m.usage IS NOT NULL AND jsonb_typeof(m.usage) = 'object'
), agg AS (
    SELECT session_id, 'tokens' AS metric, SUM(tokens) AS value FROM t GROUP BY session_id
    UNION ALL SELECT session_id, 'prompt_tokens', SUM(prompt_tokens) FROM t GROUP BY session_id
    UNION ALL SELECT session_id, 'completion_tokens', SUM(completion_tokens) FROM t GROUP BY session_id
)
INSERT INTO usage_counters (scope_type, scope_id, metric, period_key, value)
SELECT 'session', session_id, metric, 'all', value FROM agg WHERE value > 0
ON CONFLICT (scope_type, scope_id, metric, period_key)
DO UPDATE SET value = EXCLUDED.value, updated_at = CURRENT_TIMESTAMP;
