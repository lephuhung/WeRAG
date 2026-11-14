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
	beat := e.now
	if status != types.LegalSyncRunning {
		end := r.StartedAt.Add(took)
		r.FinishedAt, beat = &end, end
	}
	r.HeartbeatAt = &beat
	require.NoError(t, e.db.Create(r).Error)
}

// beat records a sign of life of the running sync runs now.
func (e *legalSyncAlertEnv) beat(t *testing.T) {
	t.Helper()
	require.NoError(t, e.db.Model(&types.LegalSyncRun{}).Where("status = ?", types.LegalSyncRunning).
		Update("heartbeat_at", e.now).Error)
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
	e.beat(t)
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

// M1: the night after a failure, the next run in progress keeps the alert
// open (no resolve, no new toast); only its success closes it.
func TestLegalSyncAlertKeptWhileNextRunGoesOn(t *testing.T) {
	e := newLegalSyncAlertEnv(t)
	e.run(t, types.LegalSyncOK, "push", 48*time.Hour, time.Hour, "")
	e.run(t, types.LegalSyncFailed, "import", 24*time.Hour, 30*time.Minute, "pq: connection refused")
	e.check(t)
	first, p := e.alert(t, "sa1")
	require.NotNil(t, first)
	require.Equal(t, "vbpl_sync:last_failed", p.Signature)
	upserts := e.notices.upserts.Load()

	// tonight's run starts: still the same incident, named by the failed night
	e.run(t, types.LegalSyncRunning, "crawl", 10*time.Minute, 0, "")
	e.now = e.now.Add(10 * time.Minute)
	e.beat(t)
	e.check(t)
	n, p := e.alert(t, "sa1")
	require.NotNil(t, n, "not resolved while the next run goes on")
	require.Equal(t, first.ID, n.ID)
	require.Equal(t, upserts, e.notices.upserts.Load(), "not written again")
	require.Equal(t, "import", p.Workers[0].Step)

	// it succeeds: closed
	require.NoError(t, e.db.Model(&types.LegalSyncRun{}).Where("status = ?", types.LegalSyncRunning).
		Updates(map[string]any{"status": types.LegalSyncOK, "finished_at": e.now}).Error)
	e.now = e.now.Add(10 * time.Minute)
	e.check(t)
	e.closed(t, types.UserNotificationLegalSyncAlert)
}

// M2: a running run silent for 30 minutes is closed as interrupted by the
// check and alerted as a failed run.
func TestLegalSyncAlertClosesSilentRun(t *testing.T) {
	e := newLegalSyncAlertEnv(t)
	e.run(t, types.LegalSyncOK, "push", 24*time.Hour, time.Hour, "")
	e.run(t, types.LegalSyncRunning, "crawl", 20*time.Minute, 0, "")
	e.check(t)
	n, _ := e.alert(t, "sa1")
	require.Nil(t, n)

	e.now = e.now.Add(31 * time.Minute) // no beat since
	e.check(t)
	n, p := e.alert(t, "sa1")
	require.NotNil(t, n)
	require.Equal(t, "vbpl_sync:last_failed", p.Signature)
	require.Equal(t, "crawl", p.Workers[0].Step)
	require.Equal(t, repository.LegalSyncHeartbeatLost, p.Workers[0].Note)
	var run types.LegalSyncRun
	require.NoError(t, e.db.Order("id DESC").First(&run).Error)
	require.Equal(t, types.LegalSyncFailed, run.Status)
	require.NotNil(t, run.FinishedAt)
}

type syncAlertSwitch struct {
	on    bool
	muted string
}

func (s syncAlertSwitch) GetInt(_ context.Context, _ string, _ string, def int64) int64 { return def }

func (s syncAlertSwitch) GetString(_ context.Context, key string, _ string, def string) string {
	if key == types.LegalSyncAlertDisabledWorkersSettingKey {
		return s.muted
	}
	return def
}

func (s syncAlertSwitch) GetBool(_ context.Context, key string, _ string, def bool) bool {
	if key == types.LegalSyncAlertEnabledSettingKey {
		return s.on
	}
	return def
}

// L4: legal.sync_alert.enabled off silences the alert and closes an open one.
func TestLegalSyncAlertSwitchedOff(t *testing.T) {
	e := newLegalSyncAlertEnv(t)
	e.run(t, types.LegalSyncFailed, "crawl", time.Hour, time.Minute, "boom")
	e.check(t)
	n, _ := e.alert(t, "sa1")
	require.NotNil(t, n)

	e.monitor.settings = syncAlertSwitch{on: false}
	e.now = e.now.Add(10 * time.Minute)
	e.check(t)
	e.closed(t, types.UserNotificationLegalSyncAlert)
	st, err := e.monitor.Status(context.Background())
	if err == nil { // the env has no legal tables for the queues: only the flag matters here
		require.False(t, st.AlertsEnabled)
	}
	require.False(t, e.monitor.alertsEnabled(context.Background()))

	e.monitor.settings = syncAlertSwitch{on: true}
	e.check(t)
	n, _ = e.alert(t, "sa1")
	require.NotNil(t, n, "back on: alerted again")
}
