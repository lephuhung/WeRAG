-- Migration: 000199_trusted_feed_links
-- The Party's portals (feed "dangcongsan") are an official source, taken
-- like vbpl.vn without a SuperAdmin review (types.LegalTrustedFeeds): the
-- identity links of their full texts waiting in the review queue are
-- approved under the feed's name, as new ingests now are.
DO $$ BEGIN RAISE NOTICE '[Migration 000199] approve identity links of trusted legal feeds'; END $$;

UPDATE legal_document_links l
   SET review_status = 'approved', reviewed_by = 'feed:dangcongsan', reviewed_at = NOW(), updated_at = NOW()
  FROM knowledges k
 WHERE k.id = l.knowledge_id
   AND k.metadata->>'source' = 'dangcongsan'
   AND l.legal_document_id LIKE 'dangcongsan-%'
   AND l.review_status = 'pending'
   AND COALESCE(l.reviewed_by, '') = '';
