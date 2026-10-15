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
