-- Full-text search for wiki pages.
--
-- The wiki search used to be a regex substring match over title/content,
-- so word order mattered and "thue thu nhap" never matched "thuế thu nhập".
-- This adds a stored, diacritic-folded tsvector (title weighted A, summary
-- B, content C) with a GIN index; the repository queries it with
-- to_tsquery('simple', ...) and ranks by ts_rank_cd. Vietnamese is
-- space-separated, so the 'simple' configuration (no stemming) is right.
--
-- unaccent() is only STABLE, so a generated column needs an IMMUTABLE
-- wrapper. Content is capped at 200k characters per page so the tsvector
-- stays under its 1 MB limit even for very large index pages.

CREATE EXTENSION IF NOT EXISTS unaccent;

CREATE OR REPLACE FUNCTION immutable_unaccent(text)
RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT
AS $$ SELECT public.unaccent('public.unaccent', $1) $$;

ALTER TABLE wiki_pages
    ADD COLUMN IF NOT EXISTS search_tsv tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', immutable_unaccent(coalesce(title, ''))), 'A') ||
        setweight(to_tsvector('simple', immutable_unaccent(coalesce(summary, ''))), 'B') ||
        setweight(to_tsvector('simple', immutable_unaccent(left(coalesce(content, ''), 200000))), 'C')
    ) STORED;

CREATE INDEX IF NOT EXISTS idx_wiki_pages_search_tsv
    ON wiki_pages USING GIN (search_tsv);
