package service

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// The BM25 index behind keyword retrieval loses performance as it
// fragments, and it does so silently: the cost it adds is fixed per query
// rather than proportional to the rows matched, so nothing in the query
// shape hints at it. This deployment reached 187 segments, the largest only
// 2.35 MB, while still carrying 20,607 deleted documents, and a keyword
// query cost 864 ms against 311 ms on a rebuilt index — a filter matching no
// rows at all still took 667 ms.
//
// Migration 000200_reindex_embeddings_bm25 rebuilt it, but why it fragmented
// is not established: VACUUM, VACUUM (ANALYZE, DISABLE_PAGE_SKIPPING) and
// the deprecated paradedb.force_merge all declined to merge those segments,
// paradedb.merge_info was empty, nothing held an old snapshot, and a
// simulation on a copy of the same cluster did not reproduce it. Since the
// rebuild erased the evidence, a recurrence has to be noticed rather than
// deduced — hence this check. It reports on the SuperAdmin runtime page and
// logs a warning on its own schedule, so the signal does not depend on
// somebody opening that page.
//
// The remedy is one statement, documented in docs/paradedb-upgrade.md:
//
//	REINDEX INDEX public.embeddings_search_idx;
//
// It is deliberately NOT run from here. REINDEX takes SHARE on embeddings,
// which blocks every write to the table, including ingest, for about a
// minute per 300,000 rows; choosing when to pay that belongs to an operator,
// not to a background ticker.
const (
	// SearchIndexName is the BM25 index this check watches.
	SearchIndexName = "embeddings_search_idx"

	// SearchIndexSegmentThresholdKey is the system setting holding the
	// segment count above which the index is reported as wanting a rebuild.
	// 60 is three times what a fresh bulk build produced here (about 20) and
	// well clear of the 31-38 a simulated 200,000-row ingest settled at, so
	// ordinary growth does not produce an hourly false alarm — which would
	// only teach whoever reads the log to ignore the line. The degraded index
	// that prompted this check sat at 187.
	SearchIndexSegmentThresholdKey     = "search.index_segment_threshold"
	SearchIndexSegmentThresholdEnv     = "WEKNORA_SEARCH_INDEX_SEGMENT_THRESHOLD"
	DefaultSearchIndexSegmentThreshold = 60

	// SearchIndexHealthInterval is how often the background check runs.
	// Fragmentation builds up over weeks, so this only needs to be often
	// enough that a warning appears in the logs of any given day.
	SearchIndexHealthInterval = time.Hour

	// searchIndexHealthTTL bounds how stale a served report may be. The
	// runtime page polls, and index_info reads index metadata rather than
	// the table, but there is no reason to repeat it per request.
	searchIndexHealthTTL = time.Minute
)

// Reason codes for an unavailable report. They are codes rather than
// sentences so the UI can translate them — the clients render Vietnamese —
// and so a provider's error text, which can name internal hosts, stays in
// the logs instead of reaching a page.
const (
	SearchIndexReasonNotConfigured = "not_configured"
	SearchIndexReasonNoDatabase    = "no_database"
	SearchIndexReasonNotPostgres   = "not_postgres"
	SearchIndexReasonNoExtension   = "pg_search_missing"
	SearchIndexReasonIndexMissing  = "index_missing"
	SearchIndexReasonQueryFailed   = "query_failed"
)

// SearchIndexHealthReport is the state of the BM25 index at CheckedAt.
// Available is false where the report cannot be produced at all — a
// non-PostgreSQL dialect (the unit-test SQLite), a deployment without
// pg_search or without the index (app.skip_embedding), or a failing query —
// in which case Reason says which and the counters are meaningless.
type SearchIndexHealthReport struct {
	Index        string    `json:"index"`
	Available    bool      `json:"available"`
	Segments     int       `json:"segments"`
	Documents    int64     `json:"documents"`
	DeletedDocs  int64     `json:"deleted_docs"`
	TotalBytes   int64     `json:"total_bytes"`
	LargestBytes int64     `json:"largest_bytes"`
	Threshold    int       `json:"threshold"`
	NeedsRebuild bool      `json:"needs_rebuild"`
	CheckedAt    time.Time `json:"checked_at"`
	Reason       string    `json:"reason,omitempty"`
}

// SearchIndexHealth checks the BM25 index and caches the last report.
// digests and redis are set by WithAlerts; without them the check reports
// and logs but raises no SuperAdmin notice.
type SearchIndexHealth struct {
	db       *gorm.DB
	settings interfaces.SystemSettingService
	now      func() time.Time
	digests  *reviewDigests
	redis    *redis.Client

	mu     sync.Mutex
	cached *SearchIndexHealthReport
}

func NewSearchIndexHealth(db *gorm.DB, settings interfaces.SystemSettingService) *SearchIndexHealth {
	return &SearchIndexHealth{db: db, settings: settings, now: time.Now}
}

// threshold reads the configured segment threshold. A value of 0 or less
// turns the NeedsRebuild verdict off while still reporting the counters.
func (h *SearchIndexHealth) threshold(ctx context.Context) int {
	if h.settings == nil {
		return DefaultSearchIndexSegmentThreshold
	}
	return int(h.settings.GetInt(ctx, SearchIndexSegmentThresholdKey,
		SearchIndexSegmentThresholdEnv, int64(DefaultSearchIndexSegmentThreshold)))
}

// needsRebuild is the only verdict the page and the warning act on: strictly
// above the threshold, and never when the threshold is switched off.
func needsRebuild(segments, threshold int) bool {
	return threshold > 0 && segments > threshold
}

// Report returns the cached report, refreshing it when older than
// searchIndexHealthTTL. Never nil.
func (h *SearchIndexHealth) Report(ctx context.Context) SearchIndexHealthReport {
	if h == nil {
		return SearchIndexHealthReport{Index: SearchIndexName, Reason: SearchIndexReasonNotConfigured}
	}
	h.mu.Lock()
	cached := h.cached
	h.mu.Unlock()
	if cached != nil && h.now().Sub(cached.CheckedAt) < searchIndexHealthTTL {
		return *cached
	}
	return h.refresh(ctx)
}

// refresh reads the index and stores the result as the cached report. A
// failed read is cached like any other: the TTL is what stops a page that
// polls from re-running a query that is currently timing out, and the only
// failure expected in practice lasts as long as a REINDEX holds the index.
func (h *SearchIndexHealth) refresh(ctx context.Context) SearchIndexHealthReport {
	rep := h.read(ctx)
	h.mu.Lock()
	h.cached = &rep
	h.mu.Unlock()
	return rep
}

// searchIndexReadTimeout bounds the index read. index_info has to open the
// index, so it waits out a REINDEX — around a minute — and this check is
// reached from an HTTP handler that holds a pooled connection while it
// waits. Failing fast keeps the runtime page responsive (and the connection
// pool free) precisely when someone is rebuilding the index.
const searchIndexReadTimeout = 3 * time.Second

func (h *SearchIndexHealth) read(ctx context.Context) SearchIndexHealthReport {
	rep := SearchIndexHealthReport{
		Index:     SearchIndexName,
		Threshold: h.threshold(ctx),
		CheckedAt: h.now(),
	}
	if h.db == nil {
		rep.Reason = SearchIndexReasonNoDatabase
		return rep
	}
	// paradedb.index_info is PostgreSQL + pg_search only; unit tests run on
	// SQLite, where asking at all would just produce a syntax error.
	if h.db.Dialector.Name() != "postgres" {
		rep.Reason = SearchIndexReasonNotPostgres
		return rep
	}

	// WithoutCancel so one visitor navigating away does not cancel a read
	// whose result is cached for everyone, then our own deadline on top.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), searchIndexReadTimeout)
	defer cancel()

	// Probing by name rather than signature: index_info is
	// (regclass, boolean DEFAULT false) today, and a missing pg_search would
	// otherwise fail at parse time on every pass and warn forever.
	var hasExtension bool
	if err := h.db.WithContext(ctx).Raw(`
		SELECT EXISTS (
			SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			 WHERE n.nspname = 'paradedb' AND p.proname = 'index_info'
		)
	`).Scan(&hasExtension).Error; err != nil {
		rep.Reason = SearchIndexReasonQueryFailed
		logger.Warnf(ctx, "[SearchIndexHealth] probing paradedb.index_info failed: %v", err)
		return rep
	}
	if !hasExtension {
		rep.Reason = SearchIndexReasonNoExtension
		return rep
	}

	var row struct {
		Segments     int
		Documents    int64
		DeletedDocs  int64
		TotalBytes   int64
		LargestBytes int64
	}
	// to_regclass rather than a bare cast: a missing index must come back as
	// "not present" rather than raising, and index_info(NULL) yields no rows.
	// The sums are cast because num_docs/num_deleted/byte_size are numeric,
	// which a driver is free to hand back as a string.
	err := h.db.WithContext(ctx).Raw(`
		SELECT count(*)                                  AS segments,
		       coalesce(sum(num_docs), 0)::bigint        AS documents,
		       coalesce(sum(num_deleted), 0)::bigint     AS deleted_docs,
		       coalesce(sum(byte_size), 0)::bigint       AS total_bytes,
		       coalesce(max(byte_size), 0)::bigint       AS largest_bytes
		  FROM paradedb.index_info(to_regclass('public.' || ?))
	`, SearchIndexName).Scan(&row).Error
	if err != nil {
		rep.Reason = SearchIndexReasonQueryFailed
		logger.Warnf(ctx, "[SearchIndexHealth] reading %s failed: %v", SearchIndexName, err)
		return rep
	}
	if row.Segments == 0 {
		rep.Reason = SearchIndexReasonIndexMissing
		return rep
	}

	rep.Available = true
	rep.Segments = row.Segments
	rep.Documents = row.Documents
	rep.DeletedDocs = row.DeletedDocs
	rep.TotalBytes = row.TotalBytes
	rep.LargestBytes = row.LargestBytes
	rep.NeedsRebuild = needsRebuild(rep.Segments, rep.Threshold)
	return rep
}

// Tick refreshes the report and warns once per pass while the index is over
// the threshold, naming the remedy so the log line is actionable on its own.
func (h *SearchIndexHealth) Tick(ctx context.Context) SearchIndexHealthReport {
	rep := h.refresh(ctx)
	switch {
	case !rep.Available:
		logger.Debugf(ctx, "[SearchIndexHealth] %s not checked: %s", rep.Index, rep.Reason)
	case rep.NeedsRebuild:
		logger.Warnf(ctx,
			"[SearchIndexHealth] %s is fragmented: %d segments (threshold %d), %d deleted documents. "+
				"Keyword search pays a fixed cost per query until it is rebuilt: "+
				"REINDEX INDEX public.%s (holds SHARE on embeddings, so ingest waits; "+
				"the last rebuild took 68s for 318k rows; see docs/paradedb-upgrade.md). "+
				"Why it fragments is still unknown, so before rebuilding, keep "+
				"paradedb.index_info('%s', true) and paradedb.merge_info('%s') — the rebuild erases them",
			rep.Index, rep.Segments, rep.Threshold, rep.DeletedDocs, rep.Index, rep.Index, rep.Index)
	default:
		logger.Debugf(ctx, "[SearchIndexHealth] %s healthy: %d segments (threshold %d), %d deleted documents",
			rep.Index, rep.Segments, rep.Threshold, rep.DeletedDocs)
	}
	// The log line above waits for somebody to read it; the digest does not.
	h.alert(ctx, rep)
	return rep
}

// StartSearchIndexHealth runs Tick now and then on every interval, returning
// the stop function. Every replica checks: the work is one metadata read and
// a log line, so there is nothing to coordinate.
func StartSearchIndexHealth(h *SearchIndexHealth, interval time.Duration) (stop func()) {
	if h == nil {
		return func() {}
	}
	if interval <= 0 {
		interval = SearchIndexHealthInterval
	}
	done := make(chan struct{})
	go func() {
		h.Tick(context.Background())
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				h.Tick(context.Background())
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}
