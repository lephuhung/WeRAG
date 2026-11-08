-- Migration: 000190_usage_cache_and_cost_counters (down)
-- Rolling back the release also rolls back its counters: the two cache metrics
-- and the money. They are a history that cannot be reconstructed once dropped
-- for the periods the running meter already wrote, which is the point — a
-- counter row only makes sense against the code that produced it. Everything
-- else in usage_counters (questions, tokens) is untouched.
DELETE FROM usage_counters
WHERE metric IN ('cache_read_tokens', 'cache_miss_tokens', 'cost_micro_vnd');
