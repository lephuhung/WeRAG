-- Migration: 000196_legal_draft_source_meta
-- OCR drafts of documents a legal feed pushed as a scan (dangcongsan.vn…):
-- the feed's name, page address and id at the source, written into the
-- knowledge's metadata when the draft is approved. NULL for vbpl.vn drafts.
DO $$ BEGIN RAISE NOTICE '[Migration 000196] legal_document_drafts.source_meta'; END $$;

ALTER TABLE legal_document_drafts ADD COLUMN IF NOT EXISTS source_meta JSONB;
