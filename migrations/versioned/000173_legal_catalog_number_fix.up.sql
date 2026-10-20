-- Migration: 000173_legal_catalog_number_fix
-- Two repairs of legal_documents numbers, matching the code shipped with
-- this migration:
--
-- 1. CatalogNumberKey now folds the look-alike "Ð" (U+00D0, Eth) to D like
--    "Đ". 750 vbpl rows (mostly pre-2005 Bộ Tài chính documents) were
--    keyed with Ð, so a number typed with Đ or D never found them. Their
--    number_key is recomputed and document_number gets the real Đ.
-- 2. The importer read "Nghị quyết số 66.12/2026/NQ-CP" as "12/2026/NQ-CP",
--    which is the key of another Nghị quyết. Rows whose title carries the
--    dotted number while document_number holds it without "66." get the
--    number from the title. ("66.1NQ/2025/NQ-CP" and "66.24" are left as
--    the portal wrote them.)
--
-- Collisions: about 390 Ð rows now share a key with a row written with
-- Đ/D, nearly all the same document imported twice (same title, issued
-- within days). Every number lookup already returns all rows of a key,
-- newest issued_on first (FindByNumberKey, Candidates, the request gate,
-- domain-pack reconcile, legal review suggestions), so a key that gains a
-- row lists one more document; nothing picks a single row blindly. The
-- dotted numbers must not collide: the migration stops if one already
-- exists on a row it does not fix itself (66.13 is catalogued twice, both
-- rows move together).
--
-- The previous values are kept in legal_documents_number_fix_000173 for
-- the down migration; a later catalogue import writes the same values
-- (CleanVBPLNumber folds Ð and keeps the dotted number).
DO $$ BEGIN RAISE NOTICE '[Migration 000173] Repairing legal catalogue number keys (Ð, Nghị quyết 66.x)'; END $$;

CREATE TABLE IF NOT EXISTS legal_documents_number_fix_000173 (
    id              VARCHAR(64)  PRIMARY KEY,
    document_number VARCHAR(128) NOT NULL,
    number_key      VARCHAR(128) NOT NULL,
    new_number      VARCHAR(128) NOT NULL,
    new_key         VARCHAR(128) NOT NULL,
    fix             VARCHAR(16)  NOT NULL
);

-- 2 first: a dotted row whose number also had Ð would take both fixes
INSERT INTO legal_documents_number_fix_000173 (id, document_number, number_key, new_number, new_key, fix)
SELECT id, document_number, number_key, num, num, 'dotted_66'
FROM (
    SELECT id, document_number, number_key,
           substring(title from '(?i)^nghị quyết (?:số )?(66\.[0-9]+/[0-9]{4}/NQ-CP)') AS num
    FROM legal_documents
    WHERE title ~* '^nghị quyết (số )?66\.[0-9]+/[0-9]{4}/NQ-CP'
) t
WHERE document_number = substring(num from '^66\.(.*)$')
ON CONFLICT (id) DO NOTHING;

INSERT INTO legal_documents_number_fix_000173 (id, document_number, number_key, new_number, new_key, fix)
SELECT id, document_number, number_key,
       translate(document_number, 'Ðð', 'Đđ'), translate(number_key, 'Ðð', 'DD'), 'eth_fold'
FROM legal_documents
WHERE number_key ~ '[Ðð]' OR document_number ~ '[Ðð]'
ON CONFLICT (id) DO NOTHING;

DO $$
DECLARE
    clash  TEXT;
    shared INT;
BEGIN
    SELECT string_agg(f.new_key || ' (' || f.id || ' vs ' || d.id || ')', ', ') INTO clash
    FROM legal_documents_number_fix_000173 f
    JOIN legal_documents d ON d.number_key = f.new_key AND d.id <> f.id
    WHERE f.fix = 'dotted_66'
      -- the same Nghị quyết imported twice (66.13) moves together
      AND d.id NOT IN (SELECT id FROM legal_documents_number_fix_000173 WHERE fix = 'dotted_66');
    IF clash IS NOT NULL THEN
        RAISE EXCEPTION '[Migration 000173] dotted number already catalogued: %', clash;
    END IF;
    SELECT count(*) INTO shared
    FROM legal_documents_number_fix_000173 f
    WHERE f.fix = 'eth_fold' AND EXISTS (
        SELECT 1 FROM legal_documents d
        WHERE d.number_key = f.new_key AND d.id <> f.id
          AND d.id NOT IN (SELECT id FROM legal_documents_number_fix_000173));
    RAISE NOTICE '[Migration 000173] % Ð rows now share their key with a row written with Đ/D', shared;
END $$;

UPDATE legal_documents d
SET document_number = f.new_number, number_key = f.new_key
FROM legal_documents_number_fix_000173 f
WHERE d.id = f.id;

-- requests are keyed the same way (none carry Ð today)
UPDATE legal_document_requests r
SET number_key = translate(r.number_key, 'Ðð', 'DD')
WHERE r.number_key ~ '[Ðð]'
  AND NOT EXISTS (
      SELECT 1 FROM legal_document_requests o
      WHERE o.number_key = translate(r.number_key, 'Ðð', 'DD')
        AND o.legal_document_id IS NOT DISTINCT FROM r.legal_document_id AND o.id <> r.id);
