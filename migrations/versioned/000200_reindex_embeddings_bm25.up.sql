-- Migration: 000200_reindex_embeddings_bm25
-- Rebuild the BM25 index on embeddings. It had grown to 187 segments, the
-- largest only 2.35 MB, while still carrying 20,607 deleted documents, and
-- keyword retrieval had become slow in a way that did not depend on how many
-- rows matched: a query whose filter matched no rows at all still took 667 ms,
-- and the same held with parallel workers disabled.
--
-- Measured on a full-size copy of embeddings (318,813 rows), same query and
-- filters, warm cache: 864 ms on the 187-segment index against 295 ms on a
-- freshly built one. REINDEX on the real index takes it from 187 segments and
-- 20,607 deleted documents to 20 segments and none, in about 69 seconds.
--
-- REINDEX rather than a new definition on purpose. Two richer variants were
-- measured and rejected for now:
--
--   * Declaring knowledge_base_id as a keyword field and is_enabled as a
--     boolean field lets pg_search filter inside the index instead of as a
--     heap_filter and takes the query to 112 ms. It also folds those columns
--     into the scoring query. Ranking held for knowledge_base_id filters
--     (identical top-50 and order over 10 queries; scores moved by a constant,
--     which the rank-based RRF fusion ignores), but searches additionally
--     scoped by knowledge_id — the document-scoped path used by
--     search_document_section and by pinning a document named by its số hiệu —
--     came back genuinely reordered on 4 of 10 queries, confirmed against the
--     true ranking rather than a truncated top-K. That belongs behind the
--     retrieval evals, as its own change.
--   * Adding `layer_sizes` would add foreground merging at thresholds the
--     background merge policy already covers (paradedb.background_layer_sizes
--     is {100kb, 1mb, 10mb, 100mb, 1gb, 10gb}), so it would move merge work
--     into the transaction of whichever ingest batch triggers it, for a
--     benefit no measurement here demonstrates: a bulk-built copy stayed at
--     31-38 segments through 200,000 inserts in 1,000-row batches plus
--     scattered deletes and updates, with the default policy.
--
-- So this migration changes no index definition at all, which also means it
-- cannot change retrieval behaviour beyond what any rebuild does: dropping
-- 20,607 deleted documents shifts the corpus statistics slightly, so BM25
-- scores move a little. On a rebuilt copy, result sets were identical across
-- all 30 comparisons and ordering only moved among documents whose scores
-- differ by 1e-5 or less.
--
-- Why the index degraded is NOT established. Neither VACUUM, VACUUM (ANALYZE,
-- DISABLE_PAGE_SKIPPING) nor paradedb.force_merge (deprecated, and it tells
-- you to run VACUUM) merged those segments, paradedb.merge_info was empty,
-- and no replication slot, prepared transaction or long transaction was
-- holding an old snapshot. A simulation on a copy of the same cluster did not
-- reproduce it. Since this rebuild erases the evidence, segment-count
-- monitoring is what will catch a recurrence, and it is still to be added.
--
-- Operational notes:
--   * Locking: REINDEX INDEX takes SHARE on embeddings and ACCESS EXCLUSIVE on
--     the index itself. SHARE blocks every write to embeddings — inserts from
--     ingest included — for the duration, and because the planner opens all of
--     a table's indexes, reads wait as well, vector search along with keyword
--     search. Budget roughly a minute per 300,000 rows. This runs
--     synchronously at startup, so the app does not come up until it finishes.
--   * lock_timeout is set for this transaction so the migration fails cleanly
--     instead of queueing behind an open transaction that has read or written
--     embeddings — a long ingest batch, for instance. (A concurrent pg_dump is
--     not such a case: it holds only ACCESS SHARE, which SHARE does not
--     conflict with.) The index is left untouched on failure, and startup
--     retries once (when AUTO_RECOVER_DIRTY is not disabled).
--   * Prefer a quiet period outside the backup window anyway: the rebuild
--     writes the whole new index, around 300 MB, through WAL, and with
--     archive_mode on that lands in the backup spool.
--   * Concurrent keyword queries wait on the lock rather than failing, which
--     matters because the `|||` operator needs the index and HybridSearch
--     turns a keyword-branch failure into a silently empty result set for the
--     whole knowledge-base group (the Warnf-and-return in
--     concurrentSearchByTargets, internal/agent/tools/search_knowledge.go).
--     Verified on a copy under a steady stream of keyword queries: 40 queries
--     across a 64-second rebuild, no errors. Those probes had no deadline of
--     their own, though — the multi-store retrieve branches time out after
--     30s (internal/application/service/knowledgebase_search_fanout.go), so a
--     request arriving during the rebuild can still give up and take that
--     silent-empty path.
DO $$
BEGIN
    -- Do not queue behind an open transaction that has touched the table:
    -- fail and let startup retry, rather than stalling every read and write
    -- on embeddings for as long as that transaction lasts.
    PERFORM set_config('lock_timeout', '30s', true);

    IF NOT EXISTS (
        SELECT 1
          FROM pg_class c
          JOIN pg_namespace n ON n.oid = c.relnamespace
         WHERE c.relname = 'embeddings_search_idx'
           AND c.relkind = 'i'
           AND n.nspname = 'public'
    ) THEN
        -- 000002 creates this index conditionally, so a deployment without it
        -- (app.skip_embedding) has nothing to rebuild.
        RAISE NOTICE '[Migration 000200] embeddings_search_idx not found, nothing to rebuild';
        RETURN;
    END IF;

    RAISE NOTICE '[Migration 000200] reindexing embeddings_search_idx to consolidate segments';

    -- Safe to repeat: REINDEX is idempotent in effect, so the dirty-recovery
    -- path re-running this migration only costs another rebuild.
    REINDEX INDEX public.embeddings_search_idx;

    RAISE NOTICE '[Migration 000200] embeddings_search_idx reindexed';
END $$;
