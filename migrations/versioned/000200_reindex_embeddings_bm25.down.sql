-- Rollback: 000200_reindex_embeddings_bm25
-- Nothing to undo. The up migration only rebuilt an index; it changed no
-- schema and no index definition, and a consolidated index cannot be
-- un-consolidated (nor would anyone want it to be). Deliberately a no-op
-- rather than a REINDEX, so rolling back does not spend another minute
-- holding ACCESS EXCLUSIVE on embeddings for no effect.
DO $$ BEGIN
    RAISE NOTICE '[Rollback 000200] no-op: the up migration only reindexed embeddings_search_idx';
END $$;
