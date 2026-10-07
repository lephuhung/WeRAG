-- Migration: 000118_knowledge_created_by
-- Records who uploaded each document so a Member may edit or delete the
-- documents they uploaded (Tenant Admins edit every document of the
-- tenant). Rows created before this migration keep '' and stay
-- admin-only: there is no trustworthy source to backfill the uploader.

ALTER TABLE knowledges ADD COLUMN IF NOT EXISTS created_by VARCHAR(36) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_knowledges_created_by ON knowledges (created_by);
