-- Identity links written by a legal feed push (reviewed_by "feed:<name>")
-- were stored as approved although no reviewer confirmed them. They now
-- wait in the review queue like every unconfirmed link.
UPDATE legal_document_links
   SET review_status = 'pending', reviewed_by = '', reviewed_at = NULL
 WHERE reviewed_by LIKE 'feed:%' AND review_status = 'approved';
