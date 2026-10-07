-- Rollback for 000119_kb_published_and_subscriptions. Published KBs fall
-- back to tenant visibility (owner-only) and every subscription is lost.

DROP TABLE IF EXISTS kb_subscriptions;

UPDATE knowledge_bases SET visibility = 'tenant', updated_at = CURRENT_TIMESTAMP
WHERE visibility = 'published';

ALTER TABLE knowledge_bases
    DROP CONSTRAINT IF EXISTS chk_knowledge_bases_owner_visibility;

ALTER TABLE knowledge_bases
    ADD CONSTRAINT chk_knowledge_bases_owner_visibility CHECK (
        (visibility = 'public' AND owner_tenant_id = 0)
        OR (visibility = 'tenant' AND owner_tenant_id > 0)
    );
