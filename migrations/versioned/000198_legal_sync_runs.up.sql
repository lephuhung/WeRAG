-- Migration: 000198_legal_sync_runs
-- One row per run of a legal data worker, for the SuperAdmin monitor
-- (Pháp lý → Thu thập dữ liệu) and its in-app alert (legal_sync_alert):
-- the nightly vbpl.vn sync (deploy/vbpl-sync: prepare → crawl → import →
-- MinIO push, recorded through legal-catalog-import -run-* flags) and the
-- backend's auto-ingest and expired-retention workers (a row only for a
-- pass that did something; kept 30 days). summary holds the run's counts
-- and the start/end of each step; error the failed step's last output.
DO $$ BEGIN RAISE NOTICE '[Migration 000198] Creating legal_sync_runs'; END $$;

CREATE TABLE IF NOT EXISTS legal_sync_runs (
    id          BIGSERIAL PRIMARY KEY,
    worker      VARCHAR(64)  NOT NULL,
    status      VARCHAR(16)  NOT NULL DEFAULT 'running',
    step        VARCHAR(32)  NOT NULL DEFAULT '',
    started_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    summary     JSONB        NOT NULL DEFAULT '{}'::jsonb,
    error       TEXT         NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_legal_sync_runs_worker_started ON legal_sync_runs (worker, started_at DESC);
