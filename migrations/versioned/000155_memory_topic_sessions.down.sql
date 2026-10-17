ALTER TABLE memory_topic_stats
    DROP COLUMN IF EXISTS sessions,
    DROP COLUMN IF EXISTS days,
    DROP COLUMN IF EXISTS session_hits;
