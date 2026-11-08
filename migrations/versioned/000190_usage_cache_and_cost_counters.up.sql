-- Migration: 000190_usage_cache_and_cost_counters
-- Two counter families the token total could not answer:
--
--   cache_read_tokens / cache_miss_tokens — the split of prompt_tokens the
--   provider served from its prompt cache (DeepSeek prompt_cache_hit_tokens /
--   prompt_cache_miss_tokens, OpenAI cache reads). A cached input token costs
--   up to 1/100 of an uncached one, so two months with the same token count can
--   differ tenfold in what they cost the deployment.
--
--   cost_micro_vnd — what the calls cost, in millionths of a đồng, priced at
--   the call with the model's tariff and the peak/off-peak hour (see
--   internal/models/pricing). Only the operator sees it: it is the number a
--   package price has to cover.
--
-- No DDL: usage_counters is (scope, metric, period) with a VARCHAR metric, so
-- a new metric is a new row. This migration only backfills the cache split
-- from the usage JSON already stored on messages, so the report is not empty on
-- the first day. Cost is deliberately not backfilled: it depends on the hour of
-- each call and on a price table that lives in code, and re-deriving it in SQL
-- would duplicate that table in a second place that silently drifts. Cost
-- accumulates from this release; for an older period, price the backfilled
-- tokens with the same rates (docs/llm-cache-and-cost.md).
DO $$ BEGIN RAISE NOTICE '[Migration 000190] usage_counters cache_read_tokens, cache_miss_tokens'; END $$;

WITH cached AS (
    SELECT s.tenant_id,
           COALESCE(s.user_id, '') AS user_id,
           (m.created_at AT TIME ZONE 'Asia/Ho_Chi_Minh') AS local_ts,
           COALESCE((m.usage->>'prompt_tokens')::bigint, 0) AS prompt_tokens,
           -- cache_read_tokens is the current key; cached_tokens the legacy
           -- alias the same struct still writes for compatibility.
           COALESCE(
               NULLIF((m.usage->>'cache_read_tokens')::bigint, 0),
               (m.usage->>'cached_tokens')::bigint,
               0
           ) AS cache_read_tokens,
           -- A provider that reports reads but not misses leaves the miss
           -- count to be derived, exactly as types.TokenUsage + pricing do.
           COALESCE(
               NULLIF((m.usage->>'cache_miss_tokens')::bigint, 0),
               GREATEST(
                   COALESCE((m.usage->>'prompt_tokens')::bigint, 0)
                   - COALESCE(
                       NULLIF((m.usage->>'cache_read_tokens')::bigint, 0),
                       (m.usage->>'cached_tokens')::bigint,
                       0
                   ),
                   0
               )
           ) AS cache_miss_tokens
    FROM messages m
    JOIN sessions s ON s.id = m.session_id
    WHERE m.role = 'assistant' AND m.deleted_at IS NULL
      AND m.usage IS NOT NULL AND jsonb_typeof(m.usage) = 'object'
      -- Only rows that actually carry a cache figure: a provider that reports
      -- nothing must leave both counters absent, so "no row" keeps meaning
      -- "this deployment is blind here" rather than "nothing was cached".
      AND (
          (m.usage ? 'cache_read_tokens') OR (m.usage ? 'cache_miss_tokens')
          OR (m.usage ? 'cached_tokens')
          OR (m.usage ? 'cache_reported' AND (m.usage->>'cache_reported')::boolean)
      )
), metrics AS (
    SELECT tenant_id, user_id, local_ts, 'cache_read_tokens' AS metric, cache_read_tokens AS value
    FROM cached WHERE cache_read_tokens > 0
    UNION ALL
    SELECT tenant_id, user_id, local_ts, 'cache_miss_tokens', cache_miss_tokens
    FROM cached WHERE cache_miss_tokens > 0
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
-- A counter that already exists is live traffic counted since this release:
-- adding the backfill to it would double those turns. The backfill only fills
-- the periods the running meter has not written yet.
ON CONFLICT (scope_type, scope_id, metric, period_key) DO NOTHING;
