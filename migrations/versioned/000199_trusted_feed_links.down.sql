-- The approvals of 000199 are not undone: a link approved since by a person
-- cannot be told apart reliably. Re-open them by hand if needed:
--   UPDATE legal_document_links SET review_status = 'pending', reviewed_by = '', reviewed_at = NULL
--    WHERE reviewed_by = 'feed:dangcongsan';
SELECT 1;
