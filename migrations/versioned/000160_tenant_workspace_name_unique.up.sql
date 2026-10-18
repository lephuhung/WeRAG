-- An organization (workspace tenant) is created by the SuperAdmin with the
-- agency's official name, so two live organizations may not share a name
-- (case-insensitive, ignoring surrounding spaces). The service checks first
-- and answers 409; this index is the guard against concurrent creates/renames.
-- Configuration profiles (kind = 'config_profile') and deleted rows are exempt.
DO $$
DECLARE
    dup TEXT;
BEGIN
    RAISE NOTICE '[Migration 000160] Adding unique organization name index...';
    SELECT string_agg(format('%s (ids %s)', k, ids), '; ')
      INTO dup
      FROM (
        SELECT lower(btrim(name)) AS k, string_agg(id::text, ',' ORDER BY id) AS ids
          FROM tenants
         WHERE deleted_at IS NULL AND kind = 'workspace'
         GROUP BY lower(btrim(name))
        HAVING count(*) > 1
      ) d;
    IF dup IS NOT NULL THEN
        RAISE EXCEPTION '[Migration 000160] organizations share a name, rename them first: %', dup;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_tenants_workspace_name_unique
    ON tenants (lower(btrim(name)))
    WHERE deleted_at IS NULL AND kind = 'workspace';
