-- Migration: 000189_legal_request_claim
-- The external-source legal-document feed has several operators working the
-- same queue of documents to fetch. A soft lease (claimed_by/claimed_at, 30
-- minutes, see types.LegalRequestClaimLease) makes "someone is on it" visible
-- without a long transaction; any decision clears it. No index: the queue is
-- read by status, and the lease is evaluated on the row already fetched.
DO $$ BEGIN RAISE NOTICE '[Migration 000189] legal_document_requests.claimed_by, claimed_at'; END $$;

ALTER TABLE legal_document_requests ADD COLUMN IF NOT EXISTS claimed_by VARCHAR(36) NOT NULL DEFAULT '';
ALTER TABLE legal_document_requests ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;
