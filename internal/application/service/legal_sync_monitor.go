package service

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

const (
	// legalSyncStaleAfter: no successful nightly sync started within a day
	// and a margin (the cron is daily: a slower run the next night is not
	// late).
	legalSyncStaleAfter = 26 * time.Hour
	// legalSyncLongRun: a sync still running after this is likely stuck.
	legalSyncLongRun = 12 * time.Hour
	// legalBeatStaleTicks: a backend worker that missed this many ticks
	// has stopped.
	legalBeatStaleTicks = 3
	// legalFeedWindow is how far back the push activity of the feeds goes.
	legalFeedWindow = 30 * 24 * time.Hour
)

// Health of a worker on the monitor, worst last.
const (
	LegalSyncHealthOK      = "ok"
	LegalSyncHealthUnknown = "unknown"
	LegalSyncHealthWarning = "warning"
	LegalSyncHealthError   = "error"
)

// Reasons behind a health below ok (the frontend words them).
const (
	LegalSyncReasonNoRuns      = "no_runs"
	LegalSyncReasonLastFailed  = "last_failed"
	LegalSyncReasonStale       = "stale"
	LegalSyncReasonRunningLong = "running_long"
	LegalSyncReasonNoHeartbeat = "no_heartbeat"
	LegalSyncReasonStarting    = "starting"
	LegalSyncReasonBeatStale   = "heartbeat_stale"
	LegalSyncReasonTickError   = "tick_error"
	// LegalSyncReasonNotStarted: the backend worker does not run in this
	// process (no Redis: the background workers are off).
	LegalSyncReasonNotStarted = "not_started"
)

// legalRunRecorder writes a backend worker's pass (repository.LegalSyncRunRepository).
type legalRunRecorder interface {
	Record(ctx context.Context, worker string, started time.Time, status, errText string, summary map[string]any) error
}

// LegalWorkerBeat is the heartbeat of a backend worker in this process:
// when it last ran a pass and the error of its last pass, if any.
// FailingSince is the end of the first of the failed passes in a row
// (nil after a pass without error): the alert waits until a failure has
// lasted legalTickErrorAlertAfter.
type LegalWorkerBeat struct {
	Interval     time.Duration
	LastTick     time.Time
	LastError    string
	LastErrorAt  *time.Time
	FailingSince *time.Time
}

var (
	legalWorkerBeats   sync.Map // worker → LegalWorkerBeat
	legalWorkerStarted sync.Map // worker → time.Time the worker was started in this process
)

// noteLegalWorkerStarted records that a backend worker runs in this
// process (Start*): until its first pass it is starting, not missing.
func noteLegalWorkerStarted(worker string, _ time.Duration, at time.Time) {
	legalWorkerStarted.Store(worker, at)
}

// legalWorkerStartedAt is when the worker was started in this process.
func legalWorkerStartedAt(worker string) (time.Time, bool) {
	v, ok := legalWorkerStarted.Load(worker)
	if !ok {
		return time.Time{}, false
	}
	return v.(time.Time), true
}

// noteLegalWorkerBeat records the end of a pass of a backend worker; a
// pass without error clears the last error and the failure streak.
func noteLegalWorkerBeat(worker string, interval time.Duration, at time.Time, errText string) {
	prev, _ := legalWorkerBeat(worker)
	b := LegalWorkerBeat{Interval: interval, LastTick: at}
	if errText != "" {
		b.LastError, b.LastErrorAt = errText, &at
		b.FailingSince = prev.FailingSince
		if b.FailingSince == nil {
			b.FailingSince = &at
		}
	}
	legalWorkerBeats.Store(worker, b)
}

// noteLegalWorkerPassStart beats when a pass starts and keeps the error
// (and failure streak) of the previous pass until this one ends.
func noteLegalWorkerPassStart(worker string, interval time.Duration, at time.Time) {
	prev, _ := legalWorkerBeat(worker)
	legalWorkerBeats.Store(worker, LegalWorkerBeat{Interval: interval, LastTick: at,
		LastError: prev.LastError, LastErrorAt: prev.LastErrorAt, FailingSince: prev.FailingSince})
}

func legalWorkerBeat(worker string) (LegalWorkerBeat, bool) {
	v, ok := legalWorkerBeats.Load(worker)
	if !ok {
		return LegalWorkerBeat{}, false
	}
	return v.(LegalWorkerBeat), true
}

// LegalSyncWorkerStatus is one worker on the monitor.
type LegalSyncWorkerStatus struct {
	Worker string `json:"worker"`
	// Kind is "container" (deploy/vbpl-sync, seen through its runs) or
	// "backend" (a goroutine of this server, with a heartbeat).
	Kind   string              `json:"kind"`
	Last   *types.LegalSyncRun `json:"last"`
	LastOK *types.LegalSyncRun `json:"last_ok"`
	// Schedule is the sync's cron line as its last run reported it.
	Schedule        string     `json:"schedule,omitempty"`
	IntervalSeconds int        `json:"interval_seconds,omitempty"`
	LastTickAt      *time.Time `json:"last_tick_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	LastErrorAt     *time.Time `json:"last_error_at,omitempty"`
	// FailingSince: the backend worker's passes have failed since then.
	FailingSince *time.Time `json:"failing_since,omitempty"`
	Health       string     `json:"health"`
	Reasons      []string   `json:"reasons"`
}

// LegalSyncQueues are the queues the workers drain.
type LegalSyncQueues struct {
	// AutoIngest: documents with a portal text waiting for the worker;
	// AutoIngestRetrying: those of them its last attempt failed on.
	AutoIngest         int64 `json:"auto_ingest"`
	AutoIngestRetrying int64 `json:"auto_ingest_retrying"`
	// Requested: open requests for a document from another source (the
	// operators' queue: TVPL extension, Party documents script …), and how
	// many came from each source (vbpl_no_text: queued by the sync).
	Requested         int64            `json:"requested"`
	RequestedBySource map[string]int64 `json:"requested_by_source"`
	// RetentionWaiting: fully expired documents whose text is still in a
	// public KB; RetentionDue: those past the period; RetentionNextDue:
	// when the next one is due.
	RetentionDays    int64      `json:"retention_days"`
	RetentionWaiting int64      `json:"retention_waiting"`
	RetentionDue     int64      `json:"retention_due"`
	RetentionNextDue *time.Time `json:"retention_next_due,omitempty"`
}

// LegalFeedActivity is the push activity of one legal feed (TVPL extension,
// Party documents script, …) over legalFeedWindow.
type LegalFeedActivity struct {
	Feed     string           `json:"feed"`
	LastAt   *time.Time       `json:"last_at"`
	Last24h  int64            `json:"last_24h"`
	Last7d   int64            `json:"last_7d"`
	Last30d  int64            `json:"last_30d"`
	ByAction map[string]int64 `json:"by_action"`
}

// LegalSyncStatus is the monitor's page.
type LegalSyncStatus struct {
	Now     time.Time               `json:"now"`
	Workers []LegalSyncWorkerStatus `json:"workers"`
	Queues  LegalSyncQueues         `json:"queues"`
	Feeds   []LegalFeedActivity     `json:"feeds"`
}

// LegalSyncMonitor reads the state of the legal data workers.
type LegalSyncMonitor struct {
	db       *gorm.DB
	runs     *repository.LegalSyncRunRepository
	settings interface {
		GetInt(ctx context.Context, key string, envName string, def int64) int64
	}
	now func() time.Time
	// alerts keeps the SuperAdmin digest (WithAlerts); nil: page only
	alerts *legalSyncAlerts
}

// NewLegalSyncMonitor creates the monitor.
func NewLegalSyncMonitor(db *gorm.DB, runs *repository.LegalSyncRunRepository,
	settings interface {
		GetInt(ctx context.Context, key string, envName string, def int64) int64
	},
) *LegalSyncMonitor {
	return &LegalSyncMonitor{db: db, runs: runs, settings: settings, now: time.Now}
}

// Runs lists the latest runs of a worker ("" for all).
func (m *LegalSyncMonitor) Runs(ctx context.Context, worker string, limit int) ([]*types.LegalSyncRun, error) {
	return m.runs.List(ctx, worker, limit)
}

// Status reads the workers, queues and feeds.
func (m *LegalSyncMonitor) Status(ctx context.Context) (*LegalSyncStatus, error) {
	now := m.now()
	out := &LegalSyncStatus{Now: now}
	var err error
	if out.Workers, err = m.workers(ctx, now); err != nil {
		return nil, err
	}
	if out.Queues, err = m.queues(ctx, now); err != nil {
		return nil, err
	}
	if out.Feeds, err = m.feeds(ctx, now); err != nil {
		return nil, err
	}
	return out, nil
}

// workers reads the state of every watched worker at now.
func (m *LegalSyncMonitor) workers(ctx context.Context, now time.Time) ([]LegalSyncWorkerStatus, error) {
	var out []LegalSyncWorkerStatus
	for _, w := range []struct {
		name, kind string
		interval   time.Duration
	}{
		{types.LegalSyncWorkerVBPL, "container", 0},
		{types.LegalSyncWorkerAutoIngest, "backend", legalAutoIngestTick},
		{types.LegalSyncWorkerExpiredRetention, "backend", legalRetentionTick},
	} {
		last, lastOK, err := m.runs.Latest(ctx, w.name)
		if err != nil {
			return nil, err
		}
		st := LegalSyncWorkerStatus{Worker: w.name, Kind: w.kind, Last: last, LastOK: lastOK}
		if w.kind == "container" {
			st.Schedule = legalRunString(last, "schedule")
			st.Health, st.Reasons = legalSyncContainerHealth(last, lastOK, now)
		} else {
			st.IntervalSeconds = int(w.interval / time.Second)
			beat, ok := legalWorkerBeat(w.name)
			if ok {
				t := beat.LastTick
				st.LastTickAt, st.LastError, st.LastErrorAt = &t, beat.LastError, beat.LastErrorAt
				st.FailingSince = beat.FailingSince
			}
			startedAt, started := legalWorkerStartedAt(w.name)
			st.Health, st.Reasons = legalSyncBackendHealth(beat, ok, w.interval, now, startedAt, started)
		}
		out = append(out, st)
	}
	return out, nil
}

// legalSyncContainerHealth judges the nightly sync from its runs.
func legalSyncContainerHealth(last, lastOK *types.LegalSyncRun, now time.Time) (string, []string) {
	if last == nil {
		return LegalSyncHealthUnknown, []string{LegalSyncReasonNoRuns}
	}
	health, reasons := LegalSyncHealthOK, []string{}
	worse := func(h, reason string) {
		if legalHealthRank(h) > legalHealthRank(health) {
			health = h
		}
		reasons = append(reasons, reason)
	}
	inProgress := false // a run going on normally: not late yet
	switch last.Status {
	case types.LegalSyncFailed:
		worse(LegalSyncHealthError, LegalSyncReasonLastFailed)
	case types.LegalSyncRunning:
		if now.Sub(last.StartedAt) > legalSyncLongRun {
			worse(LegalSyncHealthWarning, LegalSyncReasonRunningLong)
		} else {
			inProgress = true
		}
	}
	if !inProgress && (lastOK == nil || now.Sub(lastOK.StartedAt) > legalSyncStaleAfter) {
		worse(LegalSyncHealthError, LegalSyncReasonStale)
	}
	return health, reasons
}

// legalSyncBackendHealth judges a backend worker from its heartbeat (beaten
// when a pass starts and when it ends; the end carries the pass's error).
// started: the worker was started in this process at startedAt.
func legalSyncBackendHealth(beat LegalWorkerBeat, ok bool, interval time.Duration, now, startedAt time.Time,
	started bool,
) (string, []string) {
	if !ok && !started {
		return LegalSyncHealthUnknown, []string{LegalSyncReasonNotStarted}
	}
	if !ok {
		if now.Sub(startedAt) <= 2*interval {
			return LegalSyncHealthUnknown, []string{LegalSyncReasonStarting}
		}
		return LegalSyncHealthWarning, []string{LegalSyncReasonNoHeartbeat}
	}
	if now.Sub(beat.LastTick) > legalBeatStaleTicks*interval {
		return LegalSyncHealthError, []string{LegalSyncReasonBeatStale}
	}
	if beat.LastError != "" {
		return LegalSyncHealthWarning, []string{LegalSyncReasonTickError}
	}
	return LegalSyncHealthOK, []string{}
}

func legalHealthRank(h string) int {
	switch h {
	case LegalSyncHealthOK:
		return 0
	case LegalSyncHealthUnknown:
		return 1
	case LegalSyncHealthWarning:
		return 2
	}
	return 3
}

// legalRunString reads a top-level string of a run's summary.
func legalRunString(r *types.LegalSyncRun, key string) string {
	if r == nil || len(r.Summary) == 0 {
		return ""
	}
	var sum map[string]any
	if json.Unmarshal(r.Summary, &sum) != nil {
		return ""
	}
	s, _ := sum[key].(string)
	return s
}

func (m *LegalSyncMonitor) queues(ctx context.Context, now time.Time) (LegalSyncQueues, error) {
	var q LegalSyncQueues
	db := m.db.WithContext(ctx)
	reqs := func() *gorm.DB { return db.Model(&types.LegalDocumentRequest{}) }
	if err := reqs().Where("status = ?", types.LegalRequestAutoIngest).Count(&q.AutoIngest).Error; err != nil {
		return q, err
	}
	if err := reqs().Where("status = ? AND detail->>? IS NOT NULL", types.LegalRequestAutoIngest,
		types.LegalRequestDetailAutoIngestError).Count(&q.AutoIngestRetrying).Error; err != nil {
		return q, err
	}
	var bySource []struct {
		Source string
		N      int64
	}
	if err := reqs().Select("source, count(*) AS n").Where("status = ?", types.LegalRequestRequested).
		Group("source").Scan(&bySource).Error; err != nil {
		return q, err
	}
	q.RequestedBySource = map[string]int64{}
	for _, r := range bySource {
		q.RequestedBySource[r.Source] = r.N
		q.Requested += r.N
	}
	q.RetentionDays = types.LegalExpiredRetentionDefaultDays
	if m.settings != nil {
		if d := m.settings.GetInt(ctx, types.LegalExpiredRetentionDaysSettingKey, "", types.LegalExpiredRetentionDefaultDays); d >= 1 {
			q.RetentionDays = d
		}
	}
	var clocks []string
	if err := db.Raw(`SELECT k.metadata->>'`+legalExpiredAtKey+`'`+legalRetentionFrom+
		`AND d.status LIKE ? AND k.metadata->>'`+legalExpiredAtKey+`' IS NOT NULL`, LegalExpiredStatusPrefix+"%").
		Scan(&clocks).Error; err != nil {
		return q, err
	}
	period := time.Duration(q.RetentionDays) * 24 * time.Hour
	for _, c := range clocks {
		at, err := time.Parse(time.RFC3339, c)
		if err != nil {
			continue
		}
		q.RetentionWaiting++
		due := at.Add(period)
		if !due.After(now) {
			q.RetentionDue++
		} else if q.RetentionNextDue == nil || due.Before(*q.RetentionNextDue) {
			d := due
			q.RetentionNextDue = &d
		}
	}
	return q, nil
}

func (m *LegalSyncMonitor) feeds(ctx context.Context, now time.Time) ([]LegalFeedActivity, error) {
	var rows []struct {
		Feed      string
		Action    string
		CreatedAt time.Time
	}
	if err := m.db.WithContext(ctx).Table("legal_feed_log").
		Select("detail->>'feed' AS feed, action, created_at").
		Where("action LIKE ? AND created_at >= ? AND detail->>'feed' IS NOT NULL", "push_%", now.Add(-legalFeedWindow)).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	byFeed := map[string]*LegalFeedActivity{}
	for _, r := range rows {
		a := byFeed[r.Feed]
		if a == nil {
			a = &LegalFeedActivity{Feed: r.Feed, ByAction: map[string]int64{}}
			byFeed[r.Feed] = a
		}
		at := r.CreatedAt
		if a.LastAt == nil || at.After(*a.LastAt) {
			a.LastAt = &at
		}
		a.Last30d++
		if now.Sub(at) <= 7*24*time.Hour {
			a.Last7d++
			a.ByAction[r.Action]++
		}
		if now.Sub(at) <= 24*time.Hour {
			a.Last24h++
		}
	}
	out := make([]LegalFeedActivity, 0, len(byFeed))
	for _, a := range byFeed {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastAt.After(*out[j].LastAt) })
	return out, nil
}
