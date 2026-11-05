-- Migration: 000188_users_can_upload_legal_documents
-- The external-source legal-document feed (docs/legal-document-feed-spec.md §9)
-- is filled by an operator account that may only upload documents. This flag is
-- that permission: push a document, read the legal-document request queue, and
-- close a request as "added". It grants nothing else (no KB access, no review,
-- no configuration), and API keys never carry it — they keep the ingest policy
-- on the push route.
DO $$ BEGIN RAISE NOTICE '[Migration 000188] users.can_upload_legal_documents'; END $$;

ALTER TABLE users ADD COLUMN IF NOT EXISTS can_upload_legal_documents BOOLEAN NOT NULL DEFAULT FALSE;
-- No index: the flag is only read from a user row already loaded by id
-- (applyAuthSession); no query filters on it, and a plain CREATE INDEX on
-- users would block writers on a large table.
