DROP INDEX IF EXISTS idx_wiki_pages_search_tsv;
ALTER TABLE wiki_pages DROP COLUMN IF EXISTS search_tsv;
DROP FUNCTION IF EXISTS immutable_unaccent(text);
-- The unaccent extension is left installed; other objects may rely on it.
