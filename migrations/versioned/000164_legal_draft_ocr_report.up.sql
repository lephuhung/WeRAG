-- Migration: 000164_legal_draft_ocr_report
-- What the OCR repair of a legal draft did: the pages read a second time
-- because the first OCR pass looped on them, and the pages still broken
-- after both passes (flagged in the draft for typing in from the PDF):
-- {"ocr_pages_reocr": [6], "ocr_degenerate_pages": []}.
DO $$ BEGIN RAISE NOTICE '[Migration 000164] Adding legal_document_drafts.ocr_report'; END $$;

ALTER TABLE legal_document_drafts ADD COLUMN IF NOT EXISTS ocr_report JSONB NULL;
