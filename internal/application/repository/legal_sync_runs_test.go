package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newLegalSyncRunRepo(t *testing.T) (*LegalSyncRunRepository, *time.Time) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.LegalSyncRun{}))
	now := time.Date(2026, 11, 10, 2, 30, 0, 0, time.UTC)
	r := NewLegalSyncRunRepository(db)
	r.now = func() time.Time { return now }
	return r, &now
}

func legalRunSummary(t *testing.T, run *types.LegalSyncRun) map[string]any {
	t.Helper()
	var sum map[string]any
	require.NoError(t, json.Unmarshal(run.Summary, &sum))
	return sum
}

func TestLegalSyncRunStepsAndSummary(t *testing.T) {
	r, now := newLegalSyncRunRepo(t)
	ctx := context.Background()
	id, err := r.Start(ctx, types.LegalSyncWorkerVBPL, "prepare", map[string]any{"schedule": "30 2 * * *"})
	require.NoError(t, err)

	*now = now.Add(time.Minute)
	require.NoError(t, r.Update(ctx, id, "crawl", nil))
	*now = now.Add(40 * time.Minute)
	require.NoError(t, r.Update(ctx, id, "import", map[string]any{"crawl": map[string]any{"fetched": 12, "fetch_failed": 1}}))
	require.NoError(t, r.Update(ctx, id, "", map[string]any{"import": map[string]any{"documents": 17962}}))
	require.NoError(t, r.Update(ctx, id, "", map[string]any{"import": map[string]any{"queued_auto_ingest": 1}}))

	last, lastOK, err := r.Latest(ctx, types.LegalSyncWorkerVBPL)
	require.NoError(t, err)
	require.Nil(t, lastOK)
	require.Equal(t, types.LegalSyncRunning, last.Status)
	require.Equal(t, "import", last.Step)
	sum := legalRunSummary(t, last)
	require.Equal(t, "30 2 * * *", sum["schedule"])
	require.Equal(t, map[string]any{"documents": float64(17962), "queued_auto_ingest": float64(1)}, sum["import"],
		"a step's counts merge, not replace")
	steps := sum[types.LegalSyncRunSteps].(map[string]any)
	require.Equal(t, map[string]any{"started_at": "2026-11-10T02:30:00Z", "finished_at": "2026-11-10T02:31:00Z"}, steps["prepare"])
	require.Equal(t, map[string]any{"started_at": "2026-11-10T02:31:00Z", "finished_at": "2026-11-10T03:11:00Z"}, steps["crawl"])
	require.Equal(t, map[string]any{"started_at": "2026-11-10T03:11:00Z"}, steps["import"])

	*now = now.Add(5 * time.Minute)
	require.NoError(t, r.Finish(ctx, id, types.LegalSyncFailed, "push", strings.Repeat("x", 5000)+"mc: access denied", nil))
	last, _, err = r.Latest(ctx, types.LegalSyncWorkerVBPL)
	require.NoError(t, err)
	require.Equal(t, types.LegalSyncFailed, last.Status)
	require.Equal(t, "push", last.Step, "the step it failed at")
	require.NotNil(t, last.FinishedAt)
	require.LessOrEqual(t, len(last.Error), legalSyncErrorMax+len("…"))
	require.True(t, strings.HasSuffix(last.Error, "mc: access denied"), "the tail holds the error")
	steps = legalRunSummary(t, last)[types.LegalSyncRunSteps].(map[string]any)
	require.Equal(t, "2026-11-10T03:16:00Z", steps["import"].(map[string]any)["finished_at"])
	require.Equal(t, "2026-11-10T03:16:00Z", steps["push"].(map[string]any)["finished_at"])

	require.Error(t, r.Update(ctx, id, "crawl", nil), "a finished run does not move")
	require.Error(t, r.Finish(ctx, id, types.LegalSyncOK, "", "", nil))
	require.Error(t, r.Finish(ctx, id, "maybe", "", "", nil))
}

func TestLegalSyncRunLatestAndList(t *testing.T) {
	r, now := newLegalSyncRunRepo(t)
	ctx := context.Background()
	ok1, err := r.Start(ctx, types.LegalSyncWorkerVBPL, "crawl", nil)
	require.NoError(t, err)
	require.NoError(t, r.Finish(ctx, ok1, types.LegalSyncOK, "", "", nil))
	*now = now.Add(24 * time.Hour)
	bad, err := r.Start(ctx, types.LegalSyncWorkerVBPL, "crawl", nil)
	require.NoError(t, err)
	require.NoError(t, r.Finish(ctx, bad, types.LegalSyncFailed, "", "boom", nil))
	require.NoError(t, r.Record(ctx, types.LegalSyncWorkerAutoIngest, *now, types.LegalSyncOK, "", map[string]any{"added": 2}))

	last, lastOK, err := r.Latest(ctx, types.LegalSyncWorkerVBPL)
	require.NoError(t, err)
	require.Equal(t, bad, last.ID)
	require.Equal(t, ok1, lastOK.ID)

	all, err := r.List(ctx, "", 0)
	require.NoError(t, err)
	require.Len(t, all, 3)
	only, err := r.List(ctx, types.LegalSyncWorkerAutoIngest, 10)
	require.NoError(t, err)
	require.Len(t, only, 1)
	require.Equal(t, map[string]any{"added": float64(2)}, legalRunSummary(t, only[0]))
	require.NotNil(t, only[0].FinishedAt)
}

func TestLegalSyncRunStartClosesInterruptedRuns(t *testing.T) {
	r, now := newLegalSyncRunRepo(t)
	ctx := context.Background()
	left, err := r.Start(ctx, types.LegalSyncWorkerVBPL, "crawl", nil)
	require.NoError(t, err)
	*now = now.Add(24 * time.Hour)
	next, err := r.Start(ctx, types.LegalSyncWorkerVBPL, "prepare", nil)
	require.NoError(t, err)
	runs, err := r.List(ctx, types.LegalSyncWorkerVBPL, 10)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	require.Equal(t, next, runs[0].ID)
	require.Equal(t, types.LegalSyncRunning, runs[0].Status)
	require.Equal(t, left, runs[1].ID)
	require.Equal(t, types.LegalSyncFailed, runs[1].Status, "the killed run is closed by the next one")
	require.Contains(t, runs[1].Error, "Bị ngắt")
	require.NotNil(t, runs[1].FinishedAt)
}

func TestLegalSyncRecordPrunesOldPasses(t *testing.T) {
	r, now := newLegalSyncRunRepo(t)
	ctx := context.Background()
	require.NoError(t, r.Record(ctx, types.LegalSyncWorkerAutoIngest, *now, types.LegalSyncOK, "", nil))
	vbpl, err := r.Start(ctx, types.LegalSyncWorkerVBPL, "crawl", nil)
	require.NoError(t, err)
	require.NoError(t, r.Finish(ctx, vbpl, types.LegalSyncOK, "", "", nil))
	*now = now.Add(LegalSyncRecordKeep + time.Hour)
	require.NoError(t, r.Record(ctx, types.LegalSyncWorkerAutoIngest, *now, types.LegalSyncOK, "", nil))
	auto, err := r.List(ctx, types.LegalSyncWorkerAutoIngest, 10)
	require.NoError(t, err)
	require.Len(t, auto, 1, "the old pass is pruned")
	kept, err := r.List(ctx, types.LegalSyncWorkerVBPL, 10)
	require.NoError(t, err)
	require.Len(t, kept, 1, "other workers' runs stay")
}

func TestLegalSyncFinishDropsInvalidUTF8(t *testing.T) {
	r, _ := newLegalSyncRunRepo(t)
	ctx := context.Background()
	id, err := r.Start(ctx, types.LegalSyncWorkerVBPL, "import", nil)
	require.NoError(t, err)
	cut := string([]byte("Nghị định")[1:]) + " lỗi \xe1\xba" // a tail cut inside characters
	require.NoError(t, r.Finish(ctx, id, types.LegalSyncFailed, "", cut, nil))
	last, _, err := r.Latest(ctx, types.LegalSyncWorkerVBPL)
	require.NoError(t, err)
	require.True(t, json.Valid([]byte(`"`+strings.ReplaceAll(last.Error, `\`, `\\`)+`"`)))
	require.Contains(t, last.Error, "lỗi")
}

func TestLegalSyncTailKeepsRunes(t *testing.T) {
	s := strings.Repeat("ạ", 3000) // 3 bytes each
	got := legalSyncTail(s, 1000)
	require.True(t, strings.HasPrefix(got, "…"))
	require.True(t, strings.HasSuffix(got, "ạ"))
	require.True(t, json.Valid([]byte(`"`+got+`"`)), "valid UTF-8 after the cut")
	require.Equal(t, "short", legalSyncTail("short", 1000))
}
