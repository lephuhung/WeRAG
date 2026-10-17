-- Rollback for 000150_platform_tenant_seed.
--
-- Only removes the seeded row when nothing uses it yet (no memberships,
-- knowledge bases or models). A tenant that has been worked in is kept.
DELETE FROM tenants t
WHERE t.id = 10000
  AND NOT EXISTS (SELECT 1 FROM tenant_members m WHERE m.tenant_id = 10000)
  AND NOT EXISTS (SELECT 1 FROM knowledge_bases k WHERE k.tenant_id = 10000)
  AND NOT EXISTS (SELECT 1 FROM models mo WHERE mo.tenant_id = 10000);
