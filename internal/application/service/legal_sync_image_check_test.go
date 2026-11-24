package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// The host's daily image check (scripts/check-images.sh --report) records
// its runs over HTTP: step "check" and its counts only, a day and a margin
// before it is overdue, an hour before it is stuck.
func TestLegalSyncImageCheckWorker(t *testing.T) {
	sc, ok := legalSyncScheduleOf(types.LegalSyncWorkerImageCheck)
	require.True(t, ok)
	require.Equal(t, legalSyncSchedule{staleAfter: 26 * time.Hour, longRun: time.Hour}, sc)
	require.True(t, LegalSyncReportingWorkers[types.LegalSyncWorkerImageCheck])
	ws, unknown := types.ParseLegalSyncWorkerList("image_check")
	require.Empty(t, unknown)
	require.True(t, ws[types.LegalSyncWorkerImageCheck], "its alert can be muted on its own")

	e := newLegalSyncAlertEnv(t)
	ctx := context.Background()
	m := e.monitor
	m.now = time.Now
	_, err := m.StartReportedRun(ctx, types.LegalSyncWorkerImageCheck, "fetch", nil)
	require.ErrorIs(t, err, ErrLegalSyncReportInput, "dcs-sync's steps are not its own")
	_, err = m.StartReportedRun(ctx, types.LegalSyncWorkerImageCheck, "check",
		map[string]any{"upload": map[string]any{"pushed": 1}})
	require.ErrorIs(t, err, ErrLegalSyncReportInput)
	id, err := m.StartReportedRun(ctx, types.LegalSyncWorkerImageCheck, "check", map[string]any{"schedule": "17 6 * * *"})
	require.NoError(t, err)
	require.NoError(t, m.FinishReportedRun(ctx, id, types.LegalSyncOK, "", "",
		map[string]any{"check": map[string]any{"ok": 7, "stale": 0, "unstamped": 0, "not_running": 4}}))

	// a check run 2 h ago found stale images: the existing last_failed
	// alert names the worker, the step and the gist (the error's last line)
	e2 := newLegalSyncAlertEnv(t)
	started := e2.now.Add(-2 * time.Hour)
	end := started.Add(5 * time.Second)
	require.NoError(t, e2.db.Create(&types.LegalSyncRun{Worker: types.LegalSyncWorkerImageCheck, Status: types.LegalSyncFailed,
		Step: "check", StartedAt: started, FinishedAt: &end, HeartbeatAt: &end, Summary: types.JSON(`{"schedule":"17 6 * * *"}`),
		Error: "docreader      stale      image 1, repo 2\nimage cũ hơn mã nguồn: docreader, frontend (chưa có nhãn: sherpa-asr)"}).Error)
	e2.check(t)
	n, p := e2.alert(t, "sa1")
	require.NotNil(t, n)
	require.Equal(t, "image_check:last_failed", p.Signature)
	require.Equal(t, "image cũ hơn mã nguồn: docreader, frontend (chưa có nhãn: sherpa-asr)", p.Workers[0].Note)
	require.Contains(t, n.Body, "Kiểm tra image: lỗi ở bước so sánh image với mã nguồn")
	require.Contains(t, n.Body, "— image cũ hơn mã nguồn: docreader, frontend (chưa có nhãn: sherpa-asr)")

	// no check for over 26 h (cron gone): overdue
	e3 := newLegalSyncAlertEnv(t)
	okStart := e3.now.Add(-27 * time.Hour)
	okEnd := okStart.Add(time.Second)
	require.NoError(t, e3.db.Create(&types.LegalSyncRun{Worker: types.LegalSyncWorkerImageCheck, Status: types.LegalSyncOK,
		Step: "check", StartedAt: okStart, FinishedAt: &okEnd, HeartbeatAt: &okEnd}).Error)
	e3.check(t)
	_, p = e3.alert(t, "sa1")
	require.Equal(t, "image_check:stale", p.Signature)
	require.Equal(t, 26, p.Workers[0].Hours)
}
