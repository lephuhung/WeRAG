-- Migration: 000175_legal_catalog_number_fix
-- Two repairs of legal_documents numbers, matching the code shipped with
-- this migration:
--
-- 1. CatalogNumberKey now folds the look-alike "Ð" (U+00D0, Eth) to D like
--    "Đ". 750 vbpl rows (mostly pre-2005 Bộ Tài chính documents) were
--    keyed with Ð, so a number typed with Đ or D never found them. The
--    358 whose folded key no other row holds are rekeyed and their
--    document_number gets the real Đ.
--    The 392 "twins" — the folded key already belongs to a row written
--    with Đ/D, nearly always the same document imported twice — are left
--    exactly as they are, so still unreachable by number: reviving them
--    would change the newest row of 281 keys and could drop the expired
--    warning of 119 (the validity note reads "any live row" per key). They
--    are listed for a SuperAdmin decision; nothing is deleted or merged.
-- 2. The importer read "Nghị quyết số 66.12/2026/NQ-CP" as "12/2026/NQ-CP",
--    which is the key of another Nghị quyết. Rows whose title carries the
--    dotted number while document_number holds it without "66." get the
--    number from the title. ("66.1NQ/2025/NQ-CP" and "66.24" are left as
--    the portal wrote them.) The migration stops if a dotted number is
--    already catalogued on a row it does not fix itself (66.13 is
--    catalogued twice; both rows move together).
--
-- The previous values are kept in legal_documents_number_fix_000175 for
-- the down migration. A later full catalogue import writes Đ and the
-- dotted number (CleanVBPLNumber) for every row, twins included.
DO $$ BEGIN RAISE NOTICE '[Migration 000175] Repairing legal catalogue number keys (Ð, Nghị quyết 66.x)'; END $$;

CREATE TABLE IF NOT EXISTS legal_documents_number_fix_000175 (
    id              VARCHAR(64)  PRIMARY KEY,
    document_number VARCHAR(128) NOT NULL,
    number_key      VARCHAR(128) NOT NULL,
    new_number      VARCHAR(128) NOT NULL,
    new_key         VARCHAR(128) NOT NULL,
    fix             VARCHAR(16)  NOT NULL
);

-- 2 first: a dotted row whose number also had Ð would take both fixes
INSERT INTO legal_documents_number_fix_000175 (id, document_number, number_key, new_number, new_key, fix)
SELECT id, document_number, number_key, num, num, 'dotted_66'
FROM (
    SELECT id, document_number, number_key,
           substring(title from '(?i)^nghị quyết (?:số )?(66\.[0-9]+/[0-9]{4}/NQ-CP)') AS num
    FROM legal_documents
    WHERE title ~* '^nghị quyết (số )?66\.[0-9]+/[0-9]{4}/NQ-CP'
) t
WHERE document_number = substring(num from '^66\.(.*)$')
ON CONFLICT (id) DO NOTHING;

-- twins (folded key held by a row without Ð) stay as they are
INSERT INTO legal_documents_number_fix_000175 (id, document_number, number_key, new_number, new_key, fix)
SELECT e.id, e.document_number, e.number_key,
       translate(e.document_number, 'Ðð', 'Đđ'), translate(e.number_key, 'Ðð', 'DD'), 'eth_fold'
FROM legal_documents e
WHERE (e.number_key ~ '[Ðð]' OR e.document_number ~ '[Ðð]')
  AND NOT EXISTS (
      SELECT 1 FROM legal_documents d
      WHERE d.id <> e.id AND d.number_key = translate(e.number_key, 'Ðð', 'DD')
        AND d.number_key !~ '[Ðð]')
ON CONFLICT (id) DO NOTHING;

DO $$
DECLARE
    clash  TEXT;
    shared INT;
BEGIN
    SELECT string_agg(f.new_key || ' (' || f.id || ' vs ' || d.id || ')', ', ') INTO clash
    FROM legal_documents_number_fix_000175 f
    JOIN legal_documents d ON d.number_key = f.new_key AND d.id <> f.id
    WHERE f.fix = 'dotted_66'
      -- the same Nghị quyết imported twice (66.13) moves together
      AND d.id NOT IN (SELECT id FROM legal_documents_number_fix_000175 WHERE fix = 'dotted_66');
    IF clash IS NOT NULL THEN
        RAISE EXCEPTION '[Migration 000175] dotted number already catalogued: %', clash;
    END IF;
    SELECT count(*) INTO shared
    FROM legal_documents e
    WHERE e.number_key ~ '[Ðð]'
      AND e.id NOT IN (SELECT id FROM legal_documents_number_fix_000175);
    RAISE NOTICE '[Migration 000175] % Ð twin rows left as they are', shared;
END $$;

UPDATE legal_documents d
SET document_number = f.new_number, number_key = f.new_key
FROM legal_documents_number_fix_000175 f
WHERE d.id = f.id;

-- requests are keyed the same way (none carry Ð today)
UPDATE legal_document_requests r
SET number_key = translate(r.number_key, 'Ðð', 'DD')
WHERE r.number_key ~ '[Ðð]'
  AND NOT EXISTS (
      SELECT 1 FROM legal_document_requests o
      WHERE o.number_key = translate(r.number_key, 'Ðð', 'DD')
        AND o.legal_document_id IS NOT DISTINCT FROM r.legal_document_id AND o.id <> r.id);
