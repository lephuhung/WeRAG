-- Rollback: 000141_session_context_limits

UPDATE usage_plans
SET features = features - 'agent.turn_token_cap' - 'context.history_tokens' - 'session.max_turns',
    updated_at = CURRENT_TIMESTAMP
WHERE code IN ('co-ban', 'Go-1') AND features IS NOT NULL;

DELETE FROM usage_counters WHERE scope_type = 'session';

ALTER TABLE sessions DROP COLUMN IF EXISTS context_seed;
