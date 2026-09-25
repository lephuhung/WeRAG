-- Rollback for 000113_platform_public_knowledge_bases.
--
-- Removes the CHECK, the owner + visibility catalog index, and the
-- owner_tenant_id column itself. The backfill is destructive by design:
-- dropping the column loses the owner/data-scope distinction, and rows
-- fall back to tenant_id-only scoping (pre-000113 behavior). Platform
-- public KB semantics from later migrations are NOT restored by this
-- rollback; re-apply 000113 and its successors instead.

ALTER TABLE knowledge_bases
    DROP CONSTRAINT IF EXISTS chk_knowledge_bases_owner_visibility;

DROP INDEX IF EXISTS idx_knowledge_bases_owner_visibility;

ALTER TABLE knowledge_bases DROP COLUMN IF EXISTS owner_tenant_id;
