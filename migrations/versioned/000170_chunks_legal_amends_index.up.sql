-- Migration: 000168_chunks_legal_amends_index
-- The amendment history and the current wording shown to agents look up,
-- on every legal answer and every knowledge search, the chunks whose
-- metadata.legal.amends names a given Điều ("@>" on the amends array).
-- Without an index each lookup scanned the whole chunks table (~150 ms on
-- dev). The index is partial: only chunks that carry amendments, and the
-- queries repeat its predicate (jsonb_exists) so the planner can use it.
--
-- CONCURRENTLY keeps writes to chunks flowing while the index builds. It
-- cannot run inside a transaction block, so this file must hold exactly this
-- one statement (see 000106). If the build is interrupted it leaves an
-- INVALID index behind; drop it and rerun.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chunks_legal_amends
    ON chunks USING gin ((metadata->'legal'->'amends') jsonb_path_ops)
    WHERE jsonb_exists(metadata->'legal', 'amends');
