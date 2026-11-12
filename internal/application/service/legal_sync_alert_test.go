package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

type legalSyncAlertEnv struct {
	*queueNoticeEnv
	monitor *LegalSyncMonitor
	now     time.Time
}

// newLegalSyncAlertEnv: no runs, the backend workers not started in this
// process (their heartbeats are process-wide: reset here and after).
func newLegalSyncAlertEnv(t *testing.T) *legalSyncAlertEnv {
	q := newQueueNoticeEnv(t, nil)
	require.NoError(t, q.db.AutoMigrate(&types.LegalSyncRun{}))
	e := &legalSyncAlertEnv{queueNoticeEnv: q, now: time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)}
	e.monitor = NewLegalSyncMonitor(q.db, repository.NewLegalSyncRunRepository(q.db), nil).
		WithAlerts(repository.NewUserRepository(q.db), q.notices, nil)
	e.monitor.now = func() time.Time { return e.now }
	e.monitor.alerts.incident.digests.now = func() time.Time { return e.now }
	reset := func() {
		for _, w := range []string{types.LegalSyncWorkerAutoIngest, types.LegalSyncWorkerExpiredRetention} {
			legalWorkerBeats.Delete(w)
			legalWorkerStarted.Delete(w)
		}
	}
	reset()
	t.Cleanup(reset)
	return e
}

// run stores a sync run that started ago before now.
func (e *legalSyncAlertEnv) run(t *testing.T, status, step string, ago, took time.Duration, errText string) {
	t.Helper()
	r := &types.LegalSyncRun{Worker: types.LegalSyncWorkerVBPL, Status: status, Step: step,
		StartedAt: e.now.Add(-ago), Summary: types.JSON(`{"schedule":"30 2 * * *"}`), Error: errText}
	if status != types.LegalSyncRunning {
		end := r.StartedAt.Add(took)
		r.FinishedAt = &end
	}
	require.NoError(t, e.db.Create(r).Error)
}

func (e *legalSyncAlertEnv) check(t *testing.T) {
	t.Helper()
	require.NoError(t, e.monitor.Check(context.Background()))
}

func (e *legalSyncAlertEnv) alert(t *testing.T, user string) (*types.UserNotification, types.UserNotificationLegalSyncAlertPayload) {
	t.Helper()
	n := e.unread(t, user, types.UserNotificationLegalSyncAlert)
	var p types.UserNotificationLegalSyncAlertPayload
	if n != nil {
		require.NoError(t, json.Unmarshal(n.Payload, &p))
	}
	return n, p
}

func TestLegalSyncAlertRaisesUpdatesAndCloses(t *testing.T) {
	e := newLegalSyncAlertEnv(t)
	e.check(t)
	n, _ := e.alert(t, "sa1")
	require.Nil(t, n, "no sync reported yet and no backend worker here: nothing to alert")

	// yesterday's run went well, tonight's failed at the crawl
	e.run(t, types.LegalSyncOK, "push", 30*time.Hour+30*time.Minute, time.Hour, "")
	e.run(t, types.LegalSyncFailed, "crawl", 6*time.Hour+30*time.Minute, 20*time.Minute,
		"2026-11-12T02:31:00Z fetching 1/20\nTraceback …\nurllib.error.HTTPError: HTTP Error 503\n")
	e.check(t)
	for _, sa := range []string{"sa1", "sa2"} {
		n, p := e.alert(t, sa)
		require.NotNil(t, n, sa)
		require.Equal(t, "Thu thập dữ liệu pháp lý gặp sự cố: 1 việc", n.Title)
		require.Equal(t, LegalSyncAlertLink, n.Link)
		require.Equal(t, types.PlatformTenantID, n.TenantID)
		require.Equal(t, "vbpl_sync:last_failed", p.Signature, "a failed run that is also stale is one incident")
		require.Len(t, p.Workers, 1)
		w := p.Workers[0]
		require.Equal(t, "crawl", w.Step)
		require.Equal(t, "urllib.error.HTTPError: HTTP Error 503", w.Note, "the last line of the step's output")
		require.NotNil(t, w.At)
		require.NotNil(t, w.LastOKAt)
		require.Contains(t, n.Body, "Đồng bộ vbpl.vn: lỗi ở bước cào dữ liệu (lần chạy lúc 09h30 - 12/11/2026)")
		require.Contains(t, n.Body, "lần thành công cuối 10h30 - 11/11/2026")
	}
	e.nobodyElse(t)
	require.EqualValues(t, 2, e.notices.upserts.Load())

	// ten minutes later, nothing new: not written (nor toasted) again
	e.now = e.now.Add(10 * time.Minute)
	e.check(t)
	require.EqualValues(t, 2, e.notices.upserts.Load())
	first, _ := e.alert(t, "sa1")

	// the auto-ingest worker stopped passing: the digest is updated
	noteLegalWorkerStarted(types.LegalSyncWorkerAutoIngest, legalAutoIngestTick, e.now.Add(-24*time.Hour))
	noteLegalWorkerBeat(types.LegalSyncWorkerAutoIngest, legalAutoIngestTick, e.now.Add(-time.Hour), "")
	e.check(t)
	n, p := e.alert(t, "sa1")
	require.Equal(t, first.ID, n.ID, "same open digest")
	require.Equal(t, "auto_ingest:heartbeat_stale,vbpl_sync:last_failed", p.Signature)
	require.Equal(t, "Thu thập dữ liệu pháp lý gặp sự cố: 2 việc", n.Title)
	require.Contains(t, n.Body, "Tự nạp văn bản: đã dừng")

	// everything healthy again: closed for everyone, the latest stamped resolved
	e.now = e.now.Add(10 * time.Minute)
	e.run(t, types.LegalSyncOK, "push", time.Hour, 40*time.Minute, "")
	noteLegalWorkerBeat(types.LegalSyncWorkerAutoIngest, legalAutoIngestTick, e.now.Add(-time.Minute), "")
	e.check(t)
	e.closed(t, types.UserNotificationLegalSyncAlert)
	last, err := e.notices.LatestOfKind(context.Background(), "sa1", types.UserNotificationLegalSyncAlert)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(last.Payload, &p))
	require.NotNil(t, p.ResolvedAt)
	require.Equal(t, "auto_ingest:heartbeat_stale,vbpl_sync:last_failed", p.Signature, "the kind's fields are kept")
	require.Len(t, p.Workers, 2)

	// the same failure later is a new incident: raised again
	e.now = e.now.Add(24 * time.Hour)
	e.run(t, types.LegalSyncFailed, "import", time.Hour, time.Minute, "pq: connection refused")
	e.check(t)
	n, _ = e.alert(t, "sa2")
	require.NotNil(t, n)
	require.NotEqual(t, first.ID, n.ID)
}

func TestLegalSyncAlertProblems(t *testing.T) {
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { x := now.Add(-d); return &x }
	sync := func(reasons ...string) LegalSyncWorkerStatus {
		return LegalSyncWorkerStatus{Worker: types.LegalSyncWorkerVBPL, Kind: "container", Reasons: reasons}
	}
	backend := func(failing *time.Time, reasons ...string) LegalSyncWorkerStatus {
		return LegalSyncWorkerStatus{Worker: types.LegalSyncWorkerAutoIngest, Kind: "backend", Reasons: reasons, FailingSince: failing}
	}
	cases := []struct {
		name string
		w    LegalSyncWorkerStatus
		want string
	}{
		{"healthy sync", sync(), ""},
		{"never reported: not deployed", sync(LegalSyncReasonNoRuns), ""},
		{"failed and stale", sync(LegalSyncReasonLastFailed, LegalSyncReasonStale), LegalSyncReasonLastFailed},
		{"stuck", sync(LegalSyncReasonRunningLong, LegalSyncReasonStale), LegalSyncReasonRunningLong},
		{"did not run", sync(LegalSyncReasonStale), LegalSyncReasonStale},
		{"backend starting", backend(nil, LegalSyncReasonStarting), ""},
		{"backend not started here", backend(nil, LegalSyncReasonNotStarted), ""},
		{"backend never passed", backend(nil, LegalSyncReasonNoHeartbeat), LegalSyncReasonNoHeartbeat},
		{"backend stopped", backend(nil, LegalSyncReasonBeatStale), LegalSyncReasonBeatStale},
		{"one failed pass", backend(at(10*time.Minute), LegalSyncReasonTickError), ""},
		{"failing for an hour", backend(at(time.Hour), LegalSyncReasonTickError), LegalSyncReasonTickError},
	}
	for _, c := range cases {
		require.Equal(t, c.want, legalSyncAlertProblem(c.w, now), c.name)
	}
}

func TestLegalSyncAlertRunningSync(t *testing.T) {
	e := newLegalSyncAlertEnv(t)
	e.run(t, types.LegalSyncOK, "push", 24*time.Hour+30*time.Minute, time.Hour, "")
	e.run(t, types.LegalSyncRunning, "crawl", 30*time.Minute, 0, "")
	e.check(t)
	n, _ := e.alert(t, "sa1")
	require.Nil(t, n, "a run in progress is not late")

	e.now = e.now.Add(12 * time.Hour)
	e.check(t)
	n, p := e.alert(t, "sa1")
	require.NotNil(t, n)
	require.Equal(t, "vbpl_sync:running_long", p.Signature)
	require.Equal(t, "crawl", p.Workers[0].Step)
	require.Contains(t, n.Body, "chạy quá 12 giờ ở bước cào dữ liệu")
}

func TestLegalSyncMonitorWithoutAlerts(t *testing.T) {
	var nilMonitor *LegalSyncMonitor
	require.NoError(t, nilMonitor.Check(context.Background()))
	m := NewLegalSyncMonitor(nil, nil, nil)
	require.NoError(t, m.Check(context.Background()), "page-only monitor: no check")
	m.Start(time.Minute)() // does not start, stop returns at once
}

func TestIncidentResolvedPayload(t *testing.T) {
	at := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	raw, ok := incidentResolvedPayload(types.JSON(`{"signature":"a","count":1}`), at)
	require.True(t, ok)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, "a", got["signature"])
	require.EqualValues(t, 1, got["count"])
	require.Equal(t, "2026-11-12T09:00:00Z", got["resolved_at"])
	_, ok = incidentResolvedPayload(raw, at.Add(time.Hour))
	require.False(t, ok, "stamped once")
	_, ok = incidentResolvedPayload(types.JSON(`[1]`), at)
	require.False(t, ok, "not ours to rewrite")
}
