-- Rollback for 000173_legal_catalog_number_fix: the numbers and keys as
-- they were, from the saved copy. A row changed since (by a later import or
-- a SuperAdmin edit) keeps its current values. Request keys folded from Ð
-- stay folded: the code no longer produces Ð keys.
UPDATE legal_documents d
SET document_number = f.document_number, number_key = f.number_key
FROM legal_documents_number_fix_000173 f
WHERE d.id = f.id AND d.document_number = f.new_number AND d.number_key = f.new_key;

DROP TABLE IF EXISTS legal_documents_number_fix_000173;
