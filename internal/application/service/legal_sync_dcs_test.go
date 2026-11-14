package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

// The Party documents sync runs every 4 hours: stale after 9 h without a
// successful start, stuck after 2 h, dead after 30 minutes of silence.
func TestLegalSyncDCSThresholds(t *testing.T) {
	now := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { x := now.Add(-d); return &x }
	run := func(status string, ago time.Duration, finished *time.Time) *types.LegalSyncRun {
		beat := now.Add(-ago)
		if finished != nil {
			beat = *finished
		}
		return &types.LegalSyncRun{Worker: types.LegalSyncWorkerDCS, Status: status, StartedAt: now.Add(-ago),
			FinishedAt: finished, HeartbeatAt: &beat}
	}
	sc, ok := legalSyncScheduleOf(types.LegalSyncWorkerDCS)
	require.True(t, ok)
	require.Equal(t, legalSyncDCS, sc)
	_, ok = legalSyncScheduleOf(types.LegalSyncWorkerAutoIngest)
	require.False(t, ok, "a backend worker has no schedule")

	h, r := sc.health(nil, nil, nil, now)
	require.Equal(t, LegalSyncHealthUnknown, h)
	require.Equal(t, []string{LegalSyncReasonNoRuns}, r)

	ok8 := run(types.LegalSyncOK, 8*time.Hour, at(8*time.Hour-5*time.Minute))
	h, r = sc.health(ok8, nil, ok8, now)
	require.Equal(t, LegalSyncHealthOK, h, "two runs missed is still within 9 h")
	require.Empty(t, r)

	ok10 := run(types.LegalSyncOK, 10*time.Hour, at(10*time.Hour-5*time.Minute))
	h, r = sc.health(ok10, nil, ok10, now)
	require.Equal(t, LegalSyncHealthError, h)
	require.Equal(t, []string{LegalSyncReasonStale}, r)
	h, _ = legalSyncVBPL.health(ok10, nil, ok10, now)
	require.Equal(t, LegalSyncHealthOK, h, "the nightly sync's 26 h are its own")

	// the 42-hour incident: every run failed (pushes refused / DNS)
	failed := run(types.LegalSyncFailed, 30*time.Minute, at(25*time.Minute))
	old := run(types.LegalSyncOK, 42*time.Hour, at(42*time.Hour-5*time.Minute))
	h, r = sc.health(failed, nil, old, now)
	require.Equal(t, LegalSyncHealthError, h)
	require.Equal(t, []string{LegalSyncReasonLastFailed, LegalSyncReasonStale}, r)

	long := run(types.LegalSyncRunning, 2*time.Hour+time.Minute, nil)
	long.HeartbeatAt = at(time.Minute)
	h, r = sc.health(long, ok8, ok8, now)
	require.Equal(t, LegalSyncHealthWarning, h)
	require.Equal(t, []string{LegalSyncReasonRunningLong}, r)
	long.StartedAt = now.Add(-time.Hour - 50*time.Minute)
	h, _ = sc.health(long, ok8, ok8, now)
	require.Equal(t, LegalSyncHealthOK, h)

	silent := run(types.LegalSyncRunning, time.Hour, nil)
	silent.HeartbeatAt = at(31 * time.Minute)
	h, r = sc.health(silent, ok8, ok8, now)
	require.Equal(t, LegalSyncHealthError, h)
	require.Equal(t, []string{LegalSyncReasonSilent}, r)
}

// dcsRun stores a dcs_sync run that started ago before now.
func (e *legalSyncAlertEnv) dcsRun(t *testing.T, status, step string, ago, took time.Duration, errText string) {
	t.Helper()
	r := &types.LegalSyncRun{Worker: types.LegalSyncWorkerDCS, Status: status, Step: step,
		StartedAt: e.now.Add(-ago), Summary: types.JSON(`{"schedule":"15 */4 * * *"}`), Error: errText}
	beat := e.now
	if status != types.LegalSyncRunning {
		end := r.StartedAt.Add(took)
		r.FinishedAt, beat = &end, end
	}
	r.HeartbeatAt = &beat
	require.NoError(t, e.db.Create(r).Error)
}

func TestLegalSyncAlertDCS(t *testing.T) {
	e := newLegalSyncAlertEnv(t)
	e.check(t)
	n, _ := e.alert(t, "sa1")
	require.Nil(t, n, "dcs-sync never reported a run: not deployed, no alert")

	// a good run 3 h ago, then one whose pushes failed
	e.dcsRun(t, types.LegalSyncOK, "upload", 3*time.Hour, 5*time.Minute, "")
	e.check(t)
	n, _ = e.alert(t, "sa1")
	require.Nil(t, n)
	e.dcsRun(t, types.LegalSyncFailed, "upload", 20*time.Minute, 4*time.Minute,
		"0 queue     01-QĐ/TW\n12 pushes failed (HTTP 404/429/5xx, transport…): retried next run\n")
	e.check(t)
	n, p := e.alert(t, "sa1")
	require.NotNil(t, n)
	require.Equal(t, "dcs_sync:last_failed", p.Signature)
	require.Equal(t, "upload", p.Workers[0].Step)
	require.Equal(t, "12 pushes failed (HTTP 404/429/5xx, transport…): retried next run", p.Workers[0].Note)
	require.Contains(t, n.Body, "Đồng bộ văn bản của Đảng: lỗi ở bước đẩy lên WeRAG")

	// nothing reported since (tracking unreachable too, like the DNS
	// incident): the failure stays the alert, a success clears it
	e.now = e.now.Add(10 * time.Hour)
	e.check(t)
	n, p = e.alert(t, "sa1")
	require.NotNil(t, n)
	require.Equal(t, "dcs_sync:last_failed", p.Signature, "failed and stale: one incident")
	e.dcsRun(t, types.LegalSyncOK, "upload", 10*time.Minute, 5*time.Minute, "")
	e.check(t)
	e.closed(t, types.UserNotificationLegalSyncAlert)
}

func TestLegalSyncAlertDCSStaleAndLong(t *testing.T) {
	e := newLegalSyncAlertEnv(t)
	e.dcsRun(t, types.LegalSyncOK, "upload", 9*time.Hour+30*time.Minute, 5*time.Minute, "")
	e.check(t)
	n, p := e.alert(t, "sa1")
	require.NotNil(t, n)
	require.Equal(t, "dcs_sync:stale", p.Signature)
	require.Equal(t, 9, p.Workers[0].Hours)
	require.Contains(t, n.Body, "Đồng bộ văn bản của Đảng: quá hạn, không có lần chạy thành công trong 9 giờ qua")

	e.dcsRun(t, types.LegalSyncRunning, "fetch", 2*time.Hour+5*time.Minute, 0, "")
	e.check(t)
	n, p = e.alert(t, "sa1")
	require.Equal(t, "dcs_sync:running_long", p.Signature)
	require.Equal(t, 2, p.Workers[0].Hours)
	require.Contains(t, n.Body, "chạy quá 2 giờ ở bước tải trang")

	// the run goes silent: the check closes it as interrupted
	e.now = e.now.Add(31 * time.Minute)
	e.check(t)
	_, p = e.alert(t, "sa1")
	require.Equal(t, "dcs_sync:last_failed", p.Signature)
	require.Equal(t, repository.LegalSyncHeartbeatLost, p.Workers[0].Note)

	// switched off: closed
	e.monitor.settings = syncAlertSwitch{on: false}
	e.now = e.now.Add(10 * time.Minute)
	e.check(t)
	e.closed(t, types.UserNotificationLegalSyncAlert)
}

func TestLegalSyncReportedRuns(t *testing.T) {
	e := newLegalSyncAlertEnv(t)
	ctx := context.Background()
	m := e.monitor

	_, err := m.StartReportedRun(ctx, types.LegalSyncWorkerVBPL, "crawl", nil)
	require.ErrorIs(t, err, ErrLegalSyncReportWorker, "vbpl-sync writes its own runs")
	_, err = m.StartReportedRun(ctx, types.LegalSyncWorkerDCS, "Fetch; DROP", nil)
	require.ErrorIs(t, err, ErrLegalSyncReportInput)

	left, err := m.StartReportedRun(ctx, types.LegalSyncWorkerDCS, "fetch", map[string]any{"schedule": "15 */4 * * *"})
	require.NoError(t, err)
	id, err := m.StartReportedRun(ctx, types.LegalSyncWorkerDCS, "fetch",
		map[string]any{"schedule": "15 */4 * * *", "steps": "forged"})
	require.NoError(t, err)
	runs := repository.NewLegalSyncRunRepository(e.db)
	prev, err := runs.Get(ctx, left)
	require.NoError(t, err)
	require.Equal(t, types.LegalSyncFailed, prev.Status, "a run left open is closed as interrupted")

	require.NoError(t, m.UpdateReportedRun(ctx, id, "build", map[string]any{"fetch": map[string]any{"loaded": 12}}))
	require.NoError(t, m.TouchReportedRun(ctx, id))
	require.NoError(t, m.UpdateReportedRun(ctx, id, "upload", map[string]any{"build": map[string]any{"text": 3}}))
	require.NoError(t, m.FinishReportedRun(ctx, id, types.LegalSyncFailed, "",
		"x\x1b[31m red\x1b[0m\n2 pushes failed", map[string]any{"upload": map[string]any{"failed": 2}}))
	run, err := runs.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.LegalSyncFailed, run.Status)
	require.Equal(t, "upload", run.Step)
	require.Equal(t, "x red\n2 pushes failed", run.Error, "colour codes dropped")
	require.Contains(t, string(run.Summary), `"loaded":12`)
	require.Contains(t, string(run.Summary), `"failed":2`)
	require.Contains(t, string(run.Summary), `"started_at"`, "the step timings are the repository's")
	require.NotContains(t, string(run.Summary), "forged")

	require.ErrorIs(t, m.TouchReportedRun(ctx, id), ErrLegalSyncReportClosed)
	require.ErrorIs(t, m.FinishReportedRun(ctx, id, "maybe", "", "", nil), ErrLegalSyncReportInput)
	require.ErrorIs(t, m.TouchReportedRun(ctx, 999), ErrLegalSyncReportRun)

	// another worker's run is out of reach
	vbpl, err := runs.Start(ctx, types.LegalSyncWorkerVBPL, "crawl", nil)
	require.NoError(t, err)
	require.ErrorIs(t, m.FinishReportedRun(ctx, vbpl, types.LegalSyncFailed, "", "x", nil), ErrLegalSyncReportRun)
	require.ErrorIs(t, m.TouchReportedRun(ctx, vbpl), ErrLegalSyncReportRun)

	// closed for silence, then the sync writes again: it comes back
	again, err := m.StartReportedRun(ctx, types.LegalSyncWorkerDCS, "fetch", nil)
	require.NoError(t, err)
	n, err := runs.CloseSilent(ctx, types.LegalSyncWorkerDCS, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	require.NoError(t, m.TouchReportedRun(ctx, again))
	require.NoError(t, m.FinishReportedRun(ctx, again, types.LegalSyncOK, "upload", "ignored", nil))
	run, err = runs.Get(ctx, again)
	require.NoError(t, err)
	require.Equal(t, types.LegalSyncOK, run.Status)
	require.Empty(t, run.Error)
}
