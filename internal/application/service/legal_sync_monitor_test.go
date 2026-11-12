package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// fakeRuns records the passes a worker writes.
type fakeRuns struct{ runs []fakeRun }

type fakeRun struct {
	worker, status, err string
	summary             map[string]any
}

func (f *fakeRuns) Record(_ context.Context, worker string, _ time.Time, status, errText string, summary map[string]any) error {
	f.runs = append(f.runs, fakeRun{worker, status, errText, summary})
	return nil
}

func TestLegalSyncContainerHealth(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	run := func(status string, started time.Time, finished *time.Time) *types.LegalSyncRun {
		beat := started
		if finished != nil {
			beat = *finished
		}
		return &types.LegalSyncRun{Status: status, StartedAt: started, FinishedAt: finished, HeartbeatAt: &beat}
	}
	at := func(d time.Duration) *time.Time { t := now.Add(-d); return &t }

	h, r := legalSyncContainerHealth(nil, nil, nil, now)
	require.Equal(t, LegalSyncHealthUnknown, h)
	require.Equal(t, []string{LegalSyncReasonNoRuns}, r)

	ok := run(types.LegalSyncOK, now.Add(-7*time.Hour), at(6*time.Hour))
	h, r = legalSyncContainerHealth(ok, nil, ok, now)
	require.Equal(t, LegalSyncHealthOK, h)
	require.Empty(t, r)

	failed := run(types.LegalSyncFailed, now.Add(-7*time.Hour), at(6*time.Hour))
	h, r = legalSyncContainerHealth(failed, nil, ok, now)
	require.Equal(t, LegalSyncHealthError, h)
	require.Equal(t, []string{LegalSyncReasonLastFailed}, r)

	old := run(types.LegalSyncOK, now.Add(-31*time.Hour), at(30*time.Hour))
	h, r = legalSyncContainerHealth(old, nil, old, now)
	require.Equal(t, LegalSyncHealthError, h)
	require.Equal(t, []string{LegalSyncReasonStale}, r, "no success started within 26 h")
	// yesterday's run went well; tonight's is slower: not late while it runs
	yesterday := run(types.LegalSyncOK, now.Add(-24*time.Hour-30*time.Minute), at(20*time.Hour))
	tonight := run(types.LegalSyncRunning, now.Add(-30*time.Minute), nil)
	h, r = legalSyncContainerHealth(tonight, yesterday, yesterday, now)
	require.Equal(t, LegalSyncHealthOK, h)
	require.Empty(t, r)
	// … even past 26 h since yesterday's start: staleness counts at its start
	long := run(types.LegalSyncRunning, now.Add(-5*time.Hour), nil)
	long.HeartbeatAt = at(-3*time.Hour + 2*time.Minute)
	h, r = legalSyncContainerHealth(long, yesterday, yesterday, now.Add(3*time.Hour))
	require.Equal(t, LegalSyncHealthOK, h, "%v", r)
	h, _ = legalSyncContainerHealth(tonight, nil, nil, now)
	require.Equal(t, LegalSyncHealthOK, h, "the first run ever, in progress")

	// last night failed: tonight's run in progress keeps the problem
	h, r = legalSyncContainerHealth(tonight, failed, ok, now)
	require.Equal(t, LegalSyncHealthError, h)
	require.Equal(t, []string{LegalSyncReasonLastFailed}, r)
	// no success for two nights: still stale while the third runs
	h, r = legalSyncContainerHealth(tonight, old, old, now)
	require.Equal(t, LegalSyncHealthError, h)
	require.Equal(t, []string{LegalSyncReasonStale}, r)
	h, r = legalSyncContainerHealth(tonight, failed, nil, now)
	require.Equal(t, []string{LegalSyncReasonLastFailed, LegalSyncReasonStale}, r, "failed, never a success")

	stuck := run(types.LegalSyncRunning, now.Add(-13*time.Hour), nil)
	stuck.HeartbeatAt = at(time.Minute)
	h, r = legalSyncContainerHealth(stuck, ok, ok, now)
	require.Equal(t, LegalSyncHealthWarning, h)
	require.Equal(t, []string{LegalSyncReasonRunningLong}, r)

	// no sign of life for 30 minutes: dead
	silent := run(types.LegalSyncRunning, now.Add(-2*time.Hour), nil)
	silent.HeartbeatAt = at(31 * time.Minute)
	h, r = legalSyncContainerHealth(silent, yesterday, yesterday, now)
	require.Equal(t, LegalSyncHealthError, h)
	require.Equal(t, []string{LegalSyncReasonSilent}, r)
	silent.HeartbeatAt = at(29 * time.Minute)
	h, _ = legalSyncContainerHealth(silent, yesterday, yesterday, now)
	require.Equal(t, LegalSyncHealthOK, h, "the ticker beats every 5 minutes")

	h, r = legalSyncContainerHealth(failed, nil, nil, now)
	require.Equal(t, LegalSyncHealthError, h)
	require.Equal(t, []string{LegalSyncReasonLastFailed, LegalSyncReasonStale}, r)
}

func TestLegalSyncBackendHealth(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	boot := now.Add(-24 * time.Hour)
	h, r := legalSyncBackendHealth(LegalWorkerBeat{}, false, time.Hour, now, time.Time{}, false)
	require.Equal(t, LegalSyncHealthUnknown, h, "not run in this process (no Redis)")
	require.Equal(t, []string{LegalSyncReasonNotStarted}, r)
	h, r = legalSyncBackendHealth(LegalWorkerBeat{}, false, time.Hour, now, boot, true)
	require.Equal(t, LegalSyncHealthWarning, h)
	require.Equal(t, []string{LegalSyncReasonNoHeartbeat}, r)
	h, r = legalSyncBackendHealth(LegalWorkerBeat{}, false, 3*time.Minute, now, now.Add(-time.Minute), true)
	require.Equal(t, LegalSyncHealthUnknown, h, "just started")
	require.Equal(t, []string{LegalSyncReasonStarting}, r)
	h, _ = legalSyncBackendHealth(LegalWorkerBeat{LastTick: now.Add(-50 * time.Minute)}, true, time.Hour, now, boot, true)
	require.Equal(t, LegalSyncHealthOK, h)
	h, r = legalSyncBackendHealth(LegalWorkerBeat{LastTick: now.Add(-4 * time.Hour)}, true, time.Hour, now, boot, true)
	require.Equal(t, LegalSyncHealthError, h)
	require.Equal(t, []string{LegalSyncReasonBeatStale}, r)
	h, r = legalSyncBackendHealth(LegalWorkerBeat{LastTick: now, LastError: "listing: db down"}, true, time.Hour, now, boot, true)
	require.Equal(t, LegalSyncHealthWarning, h)
	require.Equal(t, []string{LegalSyncReasonTickError}, r)
}

func TestLegalWorkerBeatFailureStreak(t *testing.T) {
	const w = "test_streak"
	t.Cleanup(func() { legalWorkerBeats.Delete(w) })
	t0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	noteLegalWorkerBeat(w, time.Minute, t0, "db down")
	noteLegalWorkerPassStart(w, time.Minute, t0.Add(time.Minute))
	b, _ := legalWorkerBeat(w)
	require.Equal(t, t0, *b.FailingSince, "a pass in progress keeps the streak")
	noteLegalWorkerBeat(w, time.Minute, t0.Add(2*time.Minute), "db down again")
	b, _ = legalWorkerBeat(w)
	require.Equal(t, t0, *b.FailingSince, "the streak starts at the first failed pass")
	require.Equal(t, "db down again", b.LastError)
	noteLegalWorkerBeat(w, time.Minute, t0.Add(3*time.Minute), "")
	b, _ = legalWorkerBeat(w)
	require.Nil(t, b.FailingSince, "a good pass ends the streak")
	require.Empty(t, b.LastError)
}

func TestLegalAutoIngestRecordsItsPasses(t *testing.T) {
	row := func(id string) *types.LegalDocumentRequest {
		return &types.LegalDocumentRequest{ID: id, LegalDocumentID: id, DocumentNumber: id + "/2026/NĐ-CP", DocType: "Nghị định",
			Status: types.LegalRequestAutoIngest}
	}
	repo := &autoRepo{queueRepo: queueRepo{rows: map[string]*types.LegalDocumentRequest{
		"ok": row("ok"), "flaky": row("flaky"), "expired": row("expired"),
	}}}
	runs := &fakeRuns{}
	w := (&LegalAutoIngest{requests: repo, kbs: autoKBs{}, tenants: autoTenants{}, ingest: func(_ context.Context, id string,
		_ *types.KnowledgeBase, _ LegalFeedActor, _ string,
	) (*LegalIngestResult, error) {
		switch id {
		case "flaky":
			return nil, errors.New("portal 503")
		case "expired":
			return nil, refuse("fully expired")
		}
		repo.rows[id].Status, repo.rows[id].ClaimedBy = types.LegalRequestAdded, ""
		return &LegalIngestResult{Result: "created", KnowledgeID: "k"}, nil
	}}).WithRuns(runs)
	legalWorkerBeats.Delete(types.LegalSyncWorkerAutoIngest)

	require.Equal(t, 1, w.Tick(context.Background()))
	require.Len(t, runs.runs, 1)
	got := runs.runs[0]
	require.Equal(t, types.LegalSyncWorkerAutoIngest, got.worker)
	require.Equal(t, types.LegalSyncOK, got.status)
	require.Equal(t, map[string]any{"tried": 3, legalAutoIngestAdded: 1, legalAutoIngestRetry: 1, legalAutoIngestRejected: 1}, got.summary)
	require.NotEmpty(t, got.err)
	beat, ok := legalWorkerBeat(types.LegalSyncWorkerAutoIngest)
	require.True(t, ok, "every pass beats")
	require.Empty(t, beat.LastError, "row failures are not a failed pass")

	// only transient failures: a failed pass, shown by the heartbeat too
	runs.runs = nil
	require.Equal(t, 0, w.Tick(context.Background()))
	require.Len(t, runs.runs, 1)
	require.Equal(t, types.LegalSyncFailed, runs.runs[0].status)
	require.Contains(t, runs.runs[0].err, "portal 503")
	beat, _ = legalWorkerBeat(types.LegalSyncWorkerAutoIngest)
	require.Contains(t, beat.LastError, "portal 503")

	// nothing to do: a heartbeat, no row
	repo.rows = map[string]*types.LegalDocumentRequest{}
	runs.runs = nil
	require.Equal(t, 0, w.Tick(context.Background()))
	require.Empty(t, runs.runs)

	// a panic is a failed pass and the heartbeat's error
	repo.rows = map[string]*types.LegalDocumentRequest{"ok": row("ok")}
	w.ingest = func(context.Context, string, *types.KnowledgeBase, LegalFeedActor, string) (*LegalIngestResult, error) {
		panic("boom")
	}
	require.NotPanics(t, func() { w.Tick(context.Background()) })
	require.Len(t, runs.runs, 1)
	require.Equal(t, types.LegalSyncFailed, runs.runs[0].status)
	require.Contains(t, runs.runs[0].err, "panic: boom")
	beat, _ = legalWorkerBeat(types.LegalSyncWorkerAutoIngest)
	require.Contains(t, beat.LastError, "boom")
}

func TestLegalExpiredRetentionRecordsItsPasses(t *testing.T) {
	f := newRetentionFixture(t, 15)
	runs := &fakeRuns{}
	f.w.WithRuns(runs)
	ctx := context.Background()

	f.w.Tick(ctx)
	require.Len(t, runs.runs, 1, "starting a clock is worth a row")
	require.Equal(t, types.LegalSyncOK, runs.runs[0].status)
	require.Equal(t, 1, runs.runs[0].summary["clocks_started"])

	f.now = f.now.Add(time.Hour)
	f.w.Tick(ctx)
	require.Len(t, runs.runs, 1, "a pass with nothing to do writes no row")

	f.now = f.now.AddDate(0, 0, 15)
	require.Equal(t, 1, f.w.Tick(ctx))
	require.Len(t, runs.runs, 2)
	require.Equal(t, 1, runs.runs[1].summary["removed"])
	beat, ok := legalWorkerBeat(types.LegalSyncWorkerExpiredRetention)
	require.True(t, ok)
	require.Equal(t, f.now, beat.LastTick)
	require.Empty(t, beat.LastError)
}

func TestLegalSyncMonitorStatus(t *testing.T) {
	f := newRetentionFixture(t, 15)
	require.NoError(t, f.db.AutoMigrate(&types.LegalSyncRun{}, &types.LegalDocumentRequest{}))
	ctx := context.Background()
	f.w.Tick(ctx) // starts the clock of k-exp at f.now
	runsRepo := repository.NewLegalSyncRunRepository(f.db)
	id, err := runsRepo.Start(ctx, types.LegalSyncWorkerVBPL, "crawl", map[string]any{"schedule": "30 2 * * *"})
	require.NoError(t, err)
	require.NoError(t, runsRepo.Finish(ctx, id, types.LegalSyncOK, "", "", nil))
	for _, r := range []*types.LegalDocumentRequest{
		{ID: "a", NumberKey: "a", Status: types.LegalRequestAutoIngest, Source: "vbpl_html"},
		{ID: "b", NumberKey: "b", Status: types.LegalRequestAutoIngest, Source: "vbpl_html", Detail: types.JSON(`{"auto_ingest_error":"503"}`)},
		{ID: "c", NumberKey: "c", Status: types.LegalRequestRequested, Source: types.LegalRequestSourceNoFullText},
		{ID: "d", NumberKey: "d", Status: types.LegalRequestRequested, Source: "catalogue"},
		{ID: "e", NumberKey: "e", Status: types.LegalRequestAdded, Source: "catalogue"},
	} {
		require.NoError(t, f.db.Create(r).Error)
	}
	now := f.now.Add(2 * time.Hour)
	for _, e := range []struct {
		action, feed string
		at           time.Time
	}{
		{"push_full", "tvpl", now.Add(-time.Hour)},
		{"push_links", "tvpl", now.Add(-3 * 24 * time.Hour)},
		{"push_catalogue", "dangcongsan", now.Add(-20 * 24 * time.Hour)},
		{"push_full", "dangcongsan", now.Add(-40 * 24 * time.Hour)},
		{"ingest_created", "tvpl", now.Add(-time.Hour)},
	} {
		require.NoError(t, f.db.Exec(`INSERT INTO legal_feed_log (action, vbpl_id, detail, created_at) VALUES (?, 'x', ?, ?)`,
			e.action, `{"feed":"`+e.feed+`"}`, e.at).Error)
	}
	legalWorkerBeats.Delete(types.LegalSyncWorkerAutoIngest)
	legalWorkerStarted.Delete(types.LegalSyncWorkerAutoIngest)
	noteLegalWorkerBeat(types.LegalSyncWorkerExpiredRetention, legalRetentionTick, now.Add(-10*time.Minute), "")

	m := NewLegalSyncMonitor(f.db, runsRepo, retentionSettings{15})
	m.now = func() time.Time { return now }
	st, err := m.Status(ctx)
	require.NoError(t, err)
	require.Len(t, st.Workers, 3)
	byName := map[string]LegalSyncWorkerStatus{}
	for _, w := range st.Workers {
		byName[w.Worker] = w
	}
	vbpl := byName[types.LegalSyncWorkerVBPL]
	require.Equal(t, "30 2 * * *", vbpl.Schedule)
	require.NotNil(t, vbpl.LastOK)
	require.Equal(t, LegalSyncHealthOK, vbpl.Health, "%v", vbpl.Reasons)
	require.Equal(t, LegalSyncHealthUnknown, byName[types.LegalSyncWorkerAutoIngest].Health, "not started in this process")
	require.Equal(t, []string{LegalSyncReasonNotStarted}, byName[types.LegalSyncWorkerAutoIngest].Reasons)
	require.Equal(t, LegalSyncHealthOK, byName[types.LegalSyncWorkerExpiredRetention].Health)
	require.Equal(t, 3600, byName[types.LegalSyncWorkerExpiredRetention].IntervalSeconds)

	q := st.Queues
	require.EqualValues(t, 2, q.AutoIngest)
	require.EqualValues(t, 1, q.AutoIngestRetrying)
	require.EqualValues(t, 2, q.Requested)
	require.Equal(t, map[string]int64{types.LegalRequestSourceNoFullText: 1, "catalogue": 1}, q.RequestedBySource)
	require.EqualValues(t, 15, q.RetentionDays)
	require.EqualValues(t, 1, q.RetentionWaiting)
	require.EqualValues(t, 0, q.RetentionDue)
	require.NotNil(t, q.RetentionNextDue)
	require.Equal(t, f.now.AddDate(0, 0, 15).UTC(), q.RetentionNextDue.UTC())

	require.Len(t, st.Feeds, 2)
	require.Equal(t, "tvpl", st.Feeds[0].Feed, "most recent first")
	require.EqualValues(t, 1, st.Feeds[0].Last24h)
	require.EqualValues(t, 2, st.Feeds[0].Last7d)
	require.Equal(t, map[string]int64{"push_full": 1, "push_links": 1}, st.Feeds[0].ByAction, "ingest rows are not pushes")
	require.EqualValues(t, 1, st.Feeds[1].Last30d, "older than 30 days is out")
	require.EqualValues(t, 0, st.Feeds[1].Last7d)
}
