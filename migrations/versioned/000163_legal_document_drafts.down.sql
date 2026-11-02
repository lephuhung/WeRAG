-- Requests left in the draft states go back to the queue.
UPDATE legal_document_requests SET status = 'requested' WHERE status IN ('fetched', 'reviewing');
DROP TABLE IF EXISTS legal_document_drafts;
