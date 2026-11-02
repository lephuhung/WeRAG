-- Rollback for 000178_legal_provision_link_candidates: pending candidates
-- are lost (the feed pushes them again); approved ones stay in
-- legal_provision_links.
DROP TABLE IF EXISTS legal_provision_link_candidates;
