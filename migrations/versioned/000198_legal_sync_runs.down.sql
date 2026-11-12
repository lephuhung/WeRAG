-- Migration: 000198_legal_sync_runs (down)
DO $$ BEGIN RAISE NOTICE '[Migration 000198] Dropping legal_sync_runs'; END $$;
DROP TABLE IF EXISTS legal_sync_runs;
