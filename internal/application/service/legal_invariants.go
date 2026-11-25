package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// The daily data invariant check (worker "invariants"): once a Vietnam day,
// after the nightly vbpl.vn sync, the backend takes a snapshot of the legal
// tables' counts with a handful of aggregate queries and compares it with
// the previous snapshot and with a few absolutes. It is the general net for
// the failures nobody predicted (an old importer image deleted every text
// relation three nights in a row and nothing compared one night with the
// next). A violation fails the run, so the monitor's last_failed alert
// fires; each run compares with the previous snapshot, so a legitimate
// change alerts once and is the baseline of the next run.

const (
	// legalInvariantsTick: how often the worker looks whether today's check
	// is due (it runs at most once per Vietnam day; the tick only bounds the
	// delay after the configured time or a restart).
	legalInvariantsTick = 10 * time.Minute
	// legalInvariantsFirstDelay lets the server settle before the first look.
	legalInvariantsFirstDelay = 2 * time.Minute
	// legalInvariantsTimeout bounds one check (the queries take well under a
	// second each on the live data).
	legalInvariantsTimeout = 2 * time.Minute
	// legalInvariantsMaxTries: scheduled runs of one day that recorded no
	// snapshot (a query error, a restart mid-run) are retried at most this
	// many times, so a broken query does not write a row every tick.
	legalInvariantsMaxTries = 3
	legalInvariantsLockKey  = "werag:legal-invariants:lock"
	// legalInvariantsLockLease outlives a check (legalInvariantsTimeout).
	legalInvariantsLockLease = 3 * time.Minute
	// legalInvariantsStep is the step of a run (the snapshot and the rules).
	legalInvariantsStep = "audit"

	// legalInvariantsDefaultDropPercent / legalInvariantsDefaultDropMinRows:
	// a guarded count that fell by more than 5 % AND more than 20 rows since
	// the previous check is a violation. Both, so that a small class (a
	// KB of 30 texts losing 2) and normal churn of a big one (a few vbpl
	// relations relabelled each night out of 130k) stay quiet, while the
	// failure we know (a whole class wiped) and a partial loss of a KB are
	// caught. A class that had rows and has none now is always a violation.
	legalInvariantsDefaultDropPercent = 5
	legalInvariantsDefaultDropMinRows = 20
	// legalInvariantsDanglingGrowth: approved links pointing at a deleted
	// copy are normal leftovers (a text replaced on the portal deletes the
	// old copy and the decision is carried to the new one; retention and
	// operators delete copies), so their count is not a rule; a night's
	// replacements and retention removals add tens at most: more than this
	// many new ones since the previous check means copies were deleted in
	// bulk.
	legalInvariantsDanglingGrowth = 50
	// legalInvariantsStuckAfter: a public-KB copy pending or processing
	// without a change for this long is stuck (a parse takes minutes, an
	// OCR'd scan up to an hour or two; the auto-ingest adds a few at a time).
	legalInvariantsStuckAfter = 6 * time.Hour
	// legalInvariantsListMax bounds the ids/titles kept per list.
	legalInvariantsListMax = 10
	// legalInvariantsMessageItems: how many of a list a message names.
	legalInvariantsMessageItems = 5
	legalInvariantsTitleRunes   = 100
	// legalInvariantsPreviousScan: how many earlier runs are read to find
	// the previous snapshot (runs without one are rare: query errors).
	legalInvariantsPreviousScan = 30

	// legalInvariantsTriggerSchedule / legalInvariantsTriggerManual: the
	// summary's "trigger"; the daily guard counts the scheduled runs only.
	legalInvariantsTriggerSchedule = "schedule"
	legalInvariantsTriggerManual   = "manual"
)

// ErrLegalInvariantsBusy: a check is already running (this process or
// another instance).
var ErrLegalInvariantsBusy = errors.New("a data check is already running")

// LegalInvariantItem is one row named by a snapshot list.
type LegalInvariantItem struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

// LegalInvariantSnapshot is what one check measured (the run's summary
// "snapshot"). Counts keys:
//
//	relations.list                 vbpl.vn list edges (label <> "toàn văn")
//	relations.text.<source>        "toàn văn" edges by the from-document's
//	                               source (missing: no catalogue row)
//	relations.orphan               edges whose from or to row is missing (info)
//	catalogue.<source>             catalogue rows
//	kb.<id>.readable|in_progress|failed   public-KB copies by parse state
//	kb.<id>.approved_links         approved links whose knowledge is live
//	copies.failed|stuck            public-KB copies failed / stuck
//	copies.no_chunks               approved completed copies without chunks (info)
//	links.dangling_knowledge       approved links to a deleted/missing knowledge
//	                               (rule: growth over legalInvariantsDanglingGrowth)
//	links.dangling_superseded      of those, links whose document also has an
//	                               approved link on a live knowledge: leftovers
//	                               of a replacement (rule: must be 0)
//	links.dangling_catalogue       approved links to a missing catalogue row
//	links.kb_mismatch              links whose KB is not the knowledge's (info)
//	provisions.total|<source>      legal_provision_links
//	provisions.orphan              provision links to a missing document (info)
//	candidates.<review_status>     legal_provision_link_candidates
//	requests.<status>.<source>     open legal_document_requests
type LegalInvariantSnapshot struct {
	Counts map[string]int64 `json:"counts"`
	// KBs names the public KBs of the kb.* counts (id → name).
	KBs               map[string]string    `json:"kbs,omitempty"`
	FailedCopies      []LegalInvariantItem `json:"failed_copies,omitempty"`
	StuckCopies       []LegalInvariantItem `json:"stuck_copies,omitempty"`
	DanglingKnowledge []string             `json:"dangling_knowledge,omitempty"`
	DanglingCatalogue []string             `json:"dangling_catalogue,omitempty"`
	// DanglingSuperseded: the old copies' ids of links.dangling_superseded.
	DanglingSuperseded []string `json:"dangling_superseded,omitempty"`
}

// legalInvariantsKnownSources are always in the relation and catalogue
// counts (0 when absent), so the page shows them and a vanished source is
// compared like any other.
var legalInvariantsKnownSources = []string{types.LegalDocumentSourceVBPL, "dangcongsan", "tvpl",
	types.LegalDocumentSourceManual}

// LegalInvariants is the worker.
type LegalInvariants struct {
	db       *gorm.DB
	runs     *repository.LegalSyncRunRepository
	settings legalSyncSettings
	rdb      *redis.Client
	now      func() time.Time
	// running: one check at a time in this process
	running sync.Mutex
}

// NewLegalInvariants creates the worker; settings may be nil (defaults).
func NewLegalInvariants(db *gorm.DB, runs *repository.LegalSyncRunRepository, settings legalSyncSettings) *LegalInvariants {
	return &LegalInvariants{db: db, runs: runs, settings: settings, now: time.Now}
}

// WithLock makes a check take a Redis lock (several backend instances);
// nil: this process only.
func (w *LegalInvariants) WithLock(rdb *redis.Client) *LegalInvariants {
	w.rdb = rdb
	return w
}

// Invariants is the daily data check over the monitor's database, runs
// and settings (the manual trigger of the page, and the worker
// StartLegalInvariants runs).
func (m *LegalSyncMonitor) Invariants() *LegalInvariants {
	m.invariantsOnce.Do(func() {
		m.invariants = NewLegalInvariants(m.db, m.runs, m.settings)
		if m.alerts != nil {
			m.invariants.rdb = m.alerts.redis
		}
	})
	return m.invariants
}

// RunInvariants runs the data check now (a SuperAdmin's request) and
// returns the run; ErrLegalInvariantsBusy while one is running.
func (m *LegalSyncMonitor) RunInvariants(ctx context.Context) (*types.LegalSyncRun, error) {
	return m.Invariants().Run(ctx, legalInvariantsTriggerManual)
}

// StartLegalInvariants runs the worker until the returned func is called.
func StartLegalInvariants(w *LegalInvariants) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		wait := time.NewTimer(legalInvariantsFirstDelay)
		defer wait.Stop()
		select {
		case <-ctx.Done():
			return
		case <-wait.C:
		}
		t := time.NewTicker(legalInvariantsTick)
		defer t.Stop()
		for {
			w.Tick(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// Tick runs today's check when it is due (legalInvariantsDue); reports
// whether it ran one.
func (w *LegalInvariants) Tick(ctx context.Context) (ran bool) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf(ctx, "[LegalInvariants] panic: %v", r)
		}
	}()
	now := w.now()
	h, m := w.runAt(ctx)
	today, err := w.runsOfDay(ctx, now)
	if err != nil {
		logger.Warnf(ctx, "[LegalInvariants] reading today's runs: %v", err)
		return false
	}
	if !legalInvariantsDue(now, h, m, today) {
		return false
	}
	if _, err := w.Run(ctx, legalInvariantsTriggerSchedule); err != nil {
		if !errors.Is(err, ErrLegalInvariantsBusy) {
			logger.Warnf(ctx, "[LegalInvariants] %v", err)
		}
		return false
	}
	return true
}

// legalInvariantsZone is the calendar of the daily guard (Vietnam).
func legalInvariantsZone() *time.Location { return types.UsageLocation() }

// legalInvariantsDue: the scheduled check of now's Vietnam day is due once
// the day reached hour:minute and no scheduled run of the day recorded a
// snapshot or is still alive; runs that recorded none (a query error, a
// restart mid-run) are retried up to legalInvariantsMaxTries. Derived from
// the recorded runs, so a restart neither skips nor repeats the day. A
// manual run does not count.
func legalInvariantsDue(now time.Time, hour, minute int, today []*types.LegalSyncRun) bool {
	vn := now.In(legalInvariantsZone())
	at := time.Date(vn.Year(), vn.Month(), vn.Day(), hour, minute, 0, 0, legalInvariantsZone())
	if vn.Before(at) {
		return false
	}
	tries := 0
	for _, r := range today {
		if legalRunString(r, "trigger") != legalInvariantsTriggerSchedule {
			continue
		}
		tries++
		if legalInvariantsHasSnapshot(r) {
			return false
		}
		if r.Status == types.LegalSyncRunning {
			beat := r.StartedAt
			if r.HeartbeatAt != nil && r.HeartbeatAt.After(beat) {
				beat = *r.HeartbeatAt
			}
			if now.Sub(beat) <= legalSyncSilentAfter {
				return false // in progress (another instance)
			}
		}
	}
	return tries < legalInvariantsMaxTries
}

// runsOfDay are the worker's runs started on now's Vietnam day.
func (w *LegalInvariants) runsOfDay(ctx context.Context, now time.Time) ([]*types.LegalSyncRun, error) {
	vn := now.In(legalInvariantsZone())
	midnight := time.Date(vn.Year(), vn.Month(), vn.Day(), 0, 0, 0, 0, legalInvariantsZone())
	var rows []*types.LegalSyncRun
	err := w.db.WithContext(ctx).Where("worker = ? AND started_at >= ?", types.LegalSyncWorkerInvariants, midnight).
		Order("started_at").Order("id").Find(&rows).Error
	return rows, err
}

// runAt is the configured time of day (legal.invariants.run_at; a bad
// value falls back to the default).
func (w *LegalInvariants) runAt(ctx context.Context) (hour, minute int) {
	s := types.LegalInvariantsDefaultRunAt
	if w.settings != nil {
		s = w.settings.GetString(ctx, types.LegalInvariantsRunAtSettingKey, "", types.LegalInvariantsDefaultRunAt)
	}
	h, m, ok := types.ParseLegalInvariantsRunAt(s)
	if !ok {
		h, m, _ = types.ParseLegalInvariantsRunAt(types.LegalInvariantsDefaultRunAt)
	}
	return h, m
}

// legalInvariantLimits are the drop thresholds of the rules.
type legalInvariantLimits struct {
	dropPercent, dropMinRows int64
}

func (w *LegalInvariants) limits(ctx context.Context) legalInvariantLimits {
	l := legalInvariantLimits{dropPercent: legalInvariantsDefaultDropPercent, dropMinRows: legalInvariantsDefaultDropMinRows}
	if w.settings == nil {
		return l
	}
	if p := w.settings.GetInt(ctx, types.LegalInvariantsDropPercentSettingKey, "", l.dropPercent); p >= 1 && p <= 100 {
		l.dropPercent = p
	}
	if n := w.settings.GetInt(ctx, types.LegalInvariantsDropMinRowsSettingKey, "", l.dropMinRows); n >= 0 {
		l.dropMinRows = n
	}
	return l
}

// Run takes a snapshot, compares it with the previous one and records the
// run (failed when a rule is broken: one Vietnamese line per violation,
// most severe first, the last line the gist the alert shows).
func (w *LegalInvariants) Run(ctx context.Context, trigger string) (*types.LegalSyncRun, error) {
	if !w.running.TryLock() {
		return nil, ErrLegalInvariantsBusy
	}
	defer w.running.Unlock()
	release, ok := monitorLock(ctx, w.rdb, legalInvariantsLockKey, legalInvariantsLockLease, "LegalInvariants")
	if !ok {
		return nil, ErrLegalInvariantsBusy
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, legalInvariantsTimeout)
	defer cancel()

	h, m := w.runAt(ctx)
	id, err := w.runs.Start(ctx, types.LegalSyncWorkerInvariants, legalInvariantsStep, map[string]any{
		"trigger": trigger, "schedule": fmt.Sprintf("%d %d * * *", m, h),
	})
	if err != nil {
		return nil, fmt.Errorf("start run: %w", err)
	}
	snap, err := LegalInvariantsSnapshot(ctx, w.db, w.now())
	if err != nil {
		if ferr := w.runs.Finish(ctx, id, types.LegalSyncFailed, "", "Không đọc được số liệu: "+err.Error(), nil); ferr != nil {
			logger.Warnf(ctx, "[LegalInvariants] closing run %d: %v", id, ferr)
		}
		return w.runs.Get(context.WithoutCancel(ctx), id)
	}
	prev, prevRun, err := w.previous(ctx, id)
	if err != nil {
		logger.Warnf(ctx, "[LegalInvariants] previous snapshot: %v (compared with nothing)", err)
	}
	lines := legalInvariantRules(snap, prev, w.limits(ctx))
	patch := map[string]any{"snapshot": snap, "violations": len(lines)}
	if prevRun != nil {
		patch["previous"] = map[string]any{"run_id": prevRun.ID, "at": prevRun.StartedAt, "counts": prev.Counts}
	} else {
		patch["baseline"] = true
	}
	status, errText := types.LegalSyncOK, ""
	if len(lines) > 0 {
		status, errText = types.LegalSyncFailed, legalInvariantErrorText(lines)
	}
	if err := w.runs.Finish(ctx, id, status, "", errText, patch); err != nil {
		return nil, fmt.Errorf("finish run %d: %w", id, err)
	}
	if len(lines) > 0 {
		logger.Warnf(ctx, "[LegalInvariants] %d violation(s): %s", len(lines), lines[0])
	}
	return w.runs.Get(ctx, id)
}

// previous is the snapshot of the worker's most recent earlier run that
// recorded one (ok or failed); nil when there is none.
func (w *LegalInvariants) previous(ctx context.Context, currentID int64) (*LegalInvariantSnapshot, *types.LegalSyncRun, error) {
	var rows []*types.LegalSyncRun
	if err := w.db.WithContext(ctx).Where("worker = ? AND id <> ? AND status <> ?", types.LegalSyncWorkerInvariants,
		currentID, types.LegalSyncRunning).Order("started_at DESC").Order("id DESC").
		Limit(legalInvariantsPreviousScan).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	for _, r := range rows {
		if s := legalInvariantsSnapshotOf(r); s != nil {
			return s, r, nil
		}
	}
	return nil, nil, nil
}

// legalInvariantsSnapshotOf reads a run's snapshot (nil when it has none).
func legalInvariantsSnapshotOf(r *types.LegalSyncRun) *LegalInvariantSnapshot {
	if r == nil || len(r.Summary) == 0 {
		return nil
	}
	var sum struct {
		Snapshot *LegalInvariantSnapshot `json:"snapshot"`
	}
	if json.Unmarshal(r.Summary, &sum) != nil || sum.Snapshot == nil || sum.Snapshot.Counts == nil {
		return nil
	}
	return sum.Snapshot
}

func legalInvariantsHasSnapshot(r *types.LegalSyncRun) bool {
	return legalInvariantsSnapshotOf(r) != nil
}

// legalInvariantQuery is one aggregate query of the snapshot.
type legalInvariantQuery struct {
	name string
	sql  string
	args []any
}

// legalInvariantRow is a grouped count (kb: a public KB id or a request
// status; k: the group key).
type legalInvariantRow struct {
	KB string
	K  string
	N  int64
}

// legalInvariantQueries are the snapshot's queries. postgres selects the
// form of the one dialect-specific query (chunk probe). Every query reads
// tables of at most a few hundred thousand rows; chunks are only probed
// by knowledge_id (index chunks(knowledge_id, is_enabled, deleted_at)) for
// approved completed copies.
func legalInvariantQueries(postgres bool, now time.Time) []legalInvariantQuery {
	stuckBefore := now.Add(-legalInvariantsStuckAfter)
	const publicKB = ` JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL`
	noChunks := `SELECT COUNT(*) AS n FROM legal_document_links l
	JOIN knowledges k ON k.id = l.knowledge_id AND k.deleted_at IS NULL AND k.parse_status = 'completed'
	WHERE l.review_status = 'approved'
	  AND NOT EXISTS (SELECT 1 FROM chunks c WHERE c.knowledge_id = k.id AND c.deleted_at IS NULL)`
	if postgres {
		// LATERAL … LIMIT 1 keeps it a per-copy index probe: an anti-join
		// could be planned as a hash over every chunk
		noChunks = `SELECT COUNT(*) AS n FROM legal_document_links l
	JOIN knowledges k ON k.id = l.knowledge_id AND k.deleted_at IS NULL AND k.parse_status = 'completed'
	LEFT JOIN LATERAL (SELECT 1 AS one FROM chunks c WHERE c.knowledge_id = k.id AND c.deleted_at IS NULL LIMIT 1) c ON TRUE
	WHERE l.review_status = 'approved' AND c.one IS NULL`
	}
	// a dangling approved link whose document has an approved link on a
	// live copy: the old copy's link left behind by a replacement (a live
	// link a reviewer rejected does not count: that row is history)
	const danglingSuperseded = `
	LEFT JOIN knowledges k ON k.id = l.knowledge_id
	WHERE l.review_status = 'approved' AND (k.id IS NULL OR k.deleted_at IS NOT NULL)
	  AND EXISTS (SELECT 1 FROM legal_document_links l2
	    JOIN knowledges k2 ON k2.id = l2.knowledge_id AND k2.deleted_at IS NULL
	    WHERE l2.legal_document_id = l.legal_document_id AND l2.review_status = 'approved')`
	return []legalInvariantQuery{
		{"relations.list", `SELECT COUNT(*) AS n FROM legal_document_relations WHERE source_label <> ?`,
			[]any{types.LegalRelationSourceText}},
		{"relations.text", `SELECT COALESCE(d.source, '') AS k, COUNT(*) AS n FROM legal_document_relations r
	LEFT JOIN legal_documents d ON d.id = r.from_id
	WHERE r.source_label = ? GROUP BY COALESCE(d.source, '')`, []any{types.LegalRelationSourceText}},
		{"relations.orphan", `SELECT COUNT(*) AS n FROM legal_document_relations r
	LEFT JOIN legal_documents f ON f.id = r.from_id
	LEFT JOIN legal_documents t ON t.id = r.to_id
	WHERE f.id IS NULL OR t.id IS NULL`, nil},
		{"catalogue", `SELECT source AS k, COUNT(*) AS n FROM legal_documents GROUP BY source`, nil},
		{"kbs", `SELECT id AS kb, name AS k FROM knowledge_bases WHERE owner_tenant_id = 0 AND deleted_at IS NULL`, nil},
		{"kb.parse", `SELECT k.knowledge_base_id AS kb, COALESCE(k.parse_status, '') AS k, COUNT(*) AS n FROM knowledges k` +
			publicKB + `
	WHERE k.deleted_at IS NULL GROUP BY k.knowledge_base_id, COALESCE(k.parse_status, '')`, nil},
		{"kb.approved_links", `SELECT k.knowledge_base_id AS kb, COUNT(*) AS n FROM legal_document_links l
	JOIN knowledges k ON k.id = l.knowledge_id AND k.deleted_at IS NULL AND k.parse_status <> 'deleting'` + publicKB + `
	WHERE l.review_status = 'approved' GROUP BY k.knowledge_base_id`, nil},
		{"copies.stuck", `SELECT COUNT(*) AS n FROM knowledges k` + publicKB + `
	WHERE k.deleted_at IS NULL AND k.parse_status IN ('pending', 'processing') AND k.updated_at < ?`, []any{stuckBefore}},
		{"copies.stuck.list", `SELECT k.id AS kb, k.title AS k FROM knowledges k` + publicKB + `
	WHERE k.deleted_at IS NULL AND k.parse_status IN ('pending', 'processing') AND k.updated_at < ?
	ORDER BY k.updated_at, k.id LIMIT ` + strconv.Itoa(legalInvariantsListMax), []any{stuckBefore}},
		{"copies.failed", `SELECT COUNT(*) AS n FROM knowledges k` + publicKB + `
	WHERE k.deleted_at IS NULL AND k.parse_status = 'failed'`, nil},
		{"copies.failed.list", `SELECT k.id AS kb, k.title AS k FROM knowledges k` + publicKB + `
	WHERE k.deleted_at IS NULL AND k.parse_status = 'failed'
	ORDER BY k.updated_at DESC, k.id LIMIT ` + strconv.Itoa(legalInvariantsListMax), nil},
		{"copies.no_chunks", noChunks, nil},
		{"links.dangling_knowledge", `SELECT COUNT(*) AS n FROM legal_document_links l
	LEFT JOIN knowledges k ON k.id = l.knowledge_id
	WHERE l.review_status = 'approved' AND (k.id IS NULL OR k.deleted_at IS NOT NULL)`, nil},
		{"links.dangling_knowledge.list", `SELECT l.knowledge_id AS kb FROM legal_document_links l
	LEFT JOIN knowledges k ON k.id = l.knowledge_id
	WHERE l.review_status = 'approved' AND (k.id IS NULL OR k.deleted_at IS NOT NULL)
	ORDER BY l.knowledge_id LIMIT ` + strconv.Itoa(legalInvariantsListMax), nil},
		{"links.dangling_superseded", `SELECT COUNT(*) AS n FROM legal_document_links l` + danglingSuperseded, nil},
		{"links.dangling_superseded.list", `SELECT l.knowledge_id AS kb FROM legal_document_links l` + danglingSuperseded + `
	ORDER BY l.knowledge_id LIMIT ` + strconv.Itoa(legalInvariantsListMax), nil},
		{"links.dangling_catalogue", `SELECT COUNT(*) AS n FROM legal_document_links l
	LEFT JOIN legal_documents d ON d.id = l.legal_document_id
	WHERE l.review_status = 'approved' AND d.id IS NULL`, nil},
		{"links.dangling_catalogue.list", `SELECT l.knowledge_id AS kb FROM legal_document_links l
	LEFT JOIN legal_documents d ON d.id = l.legal_document_id
	WHERE l.review_status = 'approved' AND d.id IS NULL
	ORDER BY l.knowledge_id LIMIT ` + strconv.Itoa(legalInvariantsListMax), nil},
		{"links.kb_mismatch", `SELECT COUNT(*) AS n FROM legal_document_links l
	JOIN knowledges k ON k.id = l.knowledge_id
	WHERE l.knowledge_base_id <> k.knowledge_base_id`, nil},
		{"provisions", `SELECT source AS k, COUNT(*) AS n FROM legal_provision_links GROUP BY source`, nil},
		{"provisions.orphan", `SELECT COUNT(*) AS n FROM legal_provision_links p
	LEFT JOIN legal_documents d ON d.id = p.target_doc_id
	WHERE d.id IS NULL`, nil},
		{"candidates", `SELECT review_status AS k, COUNT(*) AS n FROM legal_provision_link_candidates GROUP BY review_status`, nil},
		{"requests", `SELECT status AS kb, source AS k, COUNT(*) AS n FROM legal_document_requests
	WHERE status IN ? GROUP BY status, source`, []any{types.LegalRequestOpenStatuses}},
	}
}

// LegalInvariantsSQL lists the snapshot's queries as run on Postgres (name
// and SQL with ? placeholders and their arguments), for reading them back
// against the live database.
func LegalInvariantsSQL(now time.Time) []string {
	qs := legalInvariantQueries(true, now)
	out := make([]string, 0, len(qs))
	for _, q := range qs {
		out = append(out, fmt.Sprintf("-- %s %v\n%s;", q.name, q.args, q.sql))
	}
	return out
}

// LegalInvariantsSnapshot measures the legal tables at now.
func LegalInvariantsSnapshot(ctx context.Context, db *gorm.DB, now time.Time) (*LegalInvariantSnapshot, error) {
	s := &LegalInvariantSnapshot{Counts: map[string]int64{}, KBs: map[string]string{}}
	for _, src := range legalInvariantsKnownSources {
		s.Counts["relations.text."+src] = 0
		s.Counts["catalogue."+src] = 0
	}
	s.Counts["provisions.total"] = 0
	db = db.WithContext(ctx)
	for _, q := range legalInvariantQueries(db.Dialector.Name() == "postgres", now) {
		var rows []legalInvariantRow
		if err := db.Raw(q.sql, q.args...).Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("%s: %w", q.name, err)
		}
		legalInvariantsApply(s, q.name, rows)
	}
	for id := range s.KBs {
		for _, part := range []string{"readable", "in_progress", "failed", "approved_links"} {
			if _, ok := s.Counts["kb."+id+"."+part]; !ok {
				s.Counts["kb."+id+"."+part] = 0
			}
		}
	}
	return s, nil
}

// legalInvariantsApply adds one query's rows to the snapshot.
func legalInvariantsApply(s *LegalInvariantSnapshot, name string, rows []legalInvariantRow) {
	one := func() int64 {
		if len(rows) == 0 {
			return 0
		}
		return rows[0].N
	}
	key := func(k string) string {
		if k = strings.TrimSpace(k); k == "" {
			return "none"
		}
		return k
	}
	switch name {
	case "relations.text":
		for _, r := range rows {
			k := r.K
			if k == "" {
				k = "missing"
			}
			s.Counts["relations.text."+k] += r.N
		}
	case "catalogue":
		for _, r := range rows {
			s.Counts["catalogue."+key(r.K)] += r.N
		}
	case "kbs":
		for _, r := range rows {
			s.KBs[r.KB] = queueEllipsis(r.K, legalInvariantsTitleRunes)
		}
	case "kb.parse":
		for _, r := range rows {
			part := ""
			switch r.K {
			case types.ParseStatusCompleted, types.ParseStatusFinalizing:
				part = "readable"
			case types.ParseStatusPending, types.ParseStatusProcessing:
				part = "in_progress"
			case types.ParseStatusFailed:
				part = "failed"
			default:
				continue // cancelled, deleting
			}
			s.Counts["kb."+r.KB+"."+part] += r.N
		}
	case "kb.approved_links":
		for _, r := range rows {
			s.Counts["kb."+r.KB+".approved_links"] += r.N
		}
	case "copies.stuck.list", "copies.failed.list":
		items := make([]LegalInvariantItem, 0, len(rows))
		for _, r := range rows {
			items = append(items, LegalInvariantItem{ID: r.KB, Title: queueEllipsis(r.K, legalInvariantsTitleRunes)})
		}
		if name == "copies.stuck.list" {
			s.StuckCopies = items
		} else {
			s.FailedCopies = items
		}
	case "links.dangling_knowledge.list", "links.dangling_catalogue.list", "links.dangling_superseded.list":
		ids := make([]string, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.KB)
		}
		switch name {
		case "links.dangling_knowledge.list":
			s.DanglingKnowledge = ids
		case "links.dangling_catalogue.list":
			s.DanglingCatalogue = ids
		default:
			s.DanglingSuperseded = ids
		}
	case "provisions":
		for _, r := range rows {
			s.Counts["provisions."+key(r.K)] += r.N
			s.Counts["provisions.total"] += r.N
		}
	case "candidates":
		for _, r := range rows {
			s.Counts["candidates."+key(r.K)] += r.N
		}
	case "requests":
		for _, r := range rows {
			s.Counts["requests."+key(r.KB)+"."+key(r.K)] += r.N
		}
	default: // a single count
		s.Counts[name] = one()
	}
}

// legalInvariantGuarded: the counts the drop rule watches (classes that
// only grow or are rebuilt whole; not queues or in-progress counts).
func legalInvariantGuarded(key string) bool {
	switch {
	case key == "relations.list", key == "provisions.total":
		return true
	case key == "relations.text.missing", key == "provisions.orphan":
		return false
	case strings.HasPrefix(key, "relations.text."), strings.HasPrefix(key, "catalogue."),
		strings.HasPrefix(key, "provisions."):
		return true
	case strings.HasPrefix(key, "kb."):
		return strings.HasSuffix(key, ".readable") || strings.HasSuffix(key, ".approved_links")
	}
	return false
}

// Severity of a violation, most severe first.
const (
	legalInvariantRankZero = iota
	legalInvariantRankDangling
	legalInvariantRankSuperseded
	legalInvariantRankDrop
	legalInvariantRankDanglingGrowth
	legalInvariantRankFailed
	legalInvariantRankStuck
)

type legalInvariantViolation struct {
	rank int
	size int64
	text string
}

// legalInvariantRules compares cur with prev (nil: the first check, a
// baseline) and the absolutes; returns the violations' lines, most severe
// first.
func legalInvariantRules(cur, prev *LegalInvariantSnapshot, l legalInvariantLimits) []string {
	var vs []legalInvariantViolation
	if prev != nil {
		keys := map[string]bool{}
		for k := range prev.Counts {
			keys[k] = true
		}
		for k := range cur.Counts {
			keys[k] = true
		}
		for k := range keys {
			if !legalInvariantGuarded(k) {
				continue
			}
			before, now := prev.Counts[k], cur.Counts[k]
			drop := before - now
			if before <= 0 || drop <= 0 {
				continue
			}
			label := legalInvariantLabel(k, cur, prev)
			if now == 0 {
				vs = append(vs, legalInvariantViolation{legalInvariantRankZero, drop,
					fmt.Sprintf("%s %s → 0 (mất %s)", label, legalFormatInt(before), legalFormatInt(before))})
				continue
			}
			if drop > l.dropMinRows && drop*100 > l.dropPercent*before {
				pct := strings.Replace(strconv.FormatFloat(float64(drop)*100/float64(before), 'f', 1, 64), ".", ",", 1)
				vs = append(vs, legalInvariantViolation{legalInvariantRankDrop, drop,
					fmt.Sprintf("%s %s → %s (giảm %s, %s %%)", label, legalFormatInt(before), legalFormatInt(now),
						legalFormatInt(drop), pct)})
			}
		}
	}
	if prev != nil {
		before, now := prev.Counts["links.dangling_knowledge"], cur.Counts["links.dangling_knowledge"]
		if now-before > legalInvariantsDanglingGrowth {
			vs = append(vs, legalInvariantViolation{legalInvariantRankDanglingGrowth, now - before,
				fmt.Sprintf("Liên kết đã duyệt trỏ tới toàn văn đã xóa: %s → %s (thêm %s)", legalFormatInt(before),
					legalFormatInt(now), legalFormatInt(now-before))})
		}
	}
	if n := cur.Counts["links.dangling_catalogue"]; n > 0 {
		vs = append(vs, legalInvariantViolation{legalInvariantRankDangling, n,
			fmt.Sprintf("%s liên kết đã duyệt trỏ tới văn bản không có trong danh mục (mã bản sao): %s", legalFormatInt(n),
				legalInvariantList(cur.DanglingCatalogue, n))})
	}
	if n := cur.Counts["links.dangling_superseded"]; n > 0 {
		vs = append(vs, legalInvariantViolation{legalInvariantRankSuperseded, n,
			fmt.Sprintf("%s liên kết đã duyệt còn trỏ tới bản toàn văn đã thay (mã bản sao cũ): %s", legalFormatInt(n),
				legalInvariantList(cur.DanglingSuperseded, n))})
	}
	if n := cur.Counts["copies.failed"]; n > 0 {
		vs = append(vs, legalInvariantViolation{legalInvariantRankFailed, n,
			fmt.Sprintf("%s văn bản tách đoạn lỗi: %s", legalFormatInt(n), legalInvariantItems(cur.FailedCopies, n))})
	}
	if n := cur.Counts["copies.stuck"]; n > 0 {
		vs = append(vs, legalInvariantViolation{legalInvariantRankStuck, n,
			fmt.Sprintf("%s văn bản đang xử lý quá %d giờ: %s", legalFormatInt(n), int(legalInvariantsStuckAfter/time.Hour),
				legalInvariantItems(cur.StuckCopies, n))})
	}
	sort.SliceStable(vs, func(i, j int) bool {
		if vs[i].rank != vs[j].rank {
			return vs[i].rank < vs[j].rank
		}
		if vs[i].size != vs[j].size {
			return vs[i].size > vs[j].size
		}
		return vs[i].text < vs[j].text
	})
	var out []string
	for _, v := range vs {
		out = append(out, v.text)
	}
	return out
}

// legalInvariantErrorText is the run's error: the violations, then the
// gist (the alert shows the last line).
func legalInvariantErrorText(lines []string) string {
	if len(lines) == 1 {
		return lines[0]
	}
	gist := fmt.Sprintf("%d vi phạm, nặng nhất: %s", len(lines), lines[0])
	return strings.Join(lines, "\n") + "\n" + gist
}

// legalInvariantSourceNames word the sources in the messages.
var legalInvariantSourceNames = map[string]string{
	types.LegalDocumentSourceVBPL:        "vbpl.vn",
	"dangcongsan":                        "văn bản Đảng",
	"tvpl":                               "TVPL",
	types.LegalDocumentSourceManual:      "nhập tay",
	types.LegalProvisionLinkSourceEditor: "biên tập viên vbpl.vn",
	"none":                               "không ghi nguồn",
}

func legalInvariantSource(s string) string {
	if n := legalInvariantSourceNames[s]; n != "" {
		return n
	}
	return s
}

// legalInvariantLabel words a guarded count, ending where the numbers
// follow ("Quan hệ đọc từ toàn văn (văn bản Đảng):", "Kho X: văn bản đọc
// được"). A KB gone since prev is named from prev.
func legalInvariantLabel(key string, cur, prev *LegalInvariantSnapshot) string {
	switch {
	case key == "relations.list":
		return "Quan hệ theo danh sách vbpl.vn:"
	case strings.HasPrefix(key, "relations.text."):
		return "Quan hệ đọc từ toàn văn (" + legalInvariantSource(strings.TrimPrefix(key, "relations.text.")) + "):"
	case strings.HasPrefix(key, "catalogue."):
		return "Danh mục văn bản (" + legalInvariantSource(strings.TrimPrefix(key, "catalogue.")) + "):"
	case key == "provisions.total":
		return "Liên kết điều khoản (tổng):"
	case strings.HasPrefix(key, "provisions."):
		return "Liên kết điều khoản (" + legalInvariantSource(strings.TrimPrefix(key, "provisions.")) + "):"
	case strings.HasPrefix(key, "kb."):
		rest := strings.TrimPrefix(key, "kb.")
		dot := strings.LastIndex(rest, ".")
		if dot < 0 {
			return key + ":"
		}
		id, part := rest[:dot], rest[dot+1:]
		name := cur.KBs[id]
		if name == "" && prev != nil {
			name = prev.KBs[id]
		}
		if name == "" {
			name = "Kho " + id
		}
		switch part {
		case "readable":
			return name + ": văn bản đọc được"
		case "approved_links":
			return name + ": liên kết đã duyệt"
		}
		return name + ": " + part
	}
	return key + ":"
}

// legalInvariantList names the first ids of a list of total n.
func legalInvariantList(ids []string, n int64) string {
	shown := ids
	if len(shown) > legalInvariantsMessageItems {
		shown = shown[:legalInvariantsMessageItems]
	}
	s := strings.Join(shown, ", ")
	if int64(len(shown)) < n {
		s += ", …"
	}
	return s
}

// legalInvariantItems names the first copies (title, else id) of n.
func legalInvariantItems(items []LegalInvariantItem, n int64) string {
	names := make([]string, 0, len(items))
	for _, it := range items {
		t := strings.TrimSpace(it.Title)
		if t == "" {
			t = it.ID
		}
		names = append(names, "«"+t+"»")
	}
	return legalInvariantList(names, n)
}

// legalFormatInt writes 6845 as "6.845".
func legalFormatInt(n int64) string {
	return strings.TrimSuffix(formatVND(n), " đ")
}
