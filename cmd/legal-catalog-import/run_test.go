package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestRunControlRequested(t *testing.T) {
	reset := func() {
		*runStart, *runID, *runStep, *runFinish, *runSummary, *runSummaryFile = false, 0, "", "", "", ""
	}
	t.Cleanup(reset)
	cases := []struct {
		name string
		set  func()
		want bool
	}{
		{"plain import", func() {}, false},
		{"import with a run id records its counts", func() { *runID = 7 }, false},
		{"run id 0 from an untracked sync", func() { *runID = 0; *runStep = "import" }, false},
		{"start", func() { *runStart = true }, true},
		{"move", func() { *runID = 7; *runStep = "crawl" }, true},
		{"summary", func() { *runID = 7; *runSummaryFile = "crawl.json" }, true},
		{"finish", func() { *runID = 7; *runFinish = "failed" }, true},
	}
	for _, c := range cases {
		reset()
		c.set()
		if got := runControlRequested(); got != c.want {
			t.Errorf("%s: runControlRequested() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRunPatch(t *testing.T) {
	reset := func() { *runSummary, *runSummaryFile, *runSummaryKey = "", "", "" }
	t.Cleanup(reset)
	dir := t.TempDir()
	crawl := filepath.Join(dir, "crawl.json")
	require.NoError(t, os.WriteFile(crawl, []byte(`{"fetched":12,"fetch_failed":1}`), 0o600))

	reset()
	*runSummary, *runSummaryFile, *runSummaryKey = `{"schedule":"30 2 * * *"}`, crawl, "crawl"
	patch, err := runPatch()
	require.NoError(t, err)
	require.Equal(t, map[string]any{"schedule": "30 2 * * *",
		"crawl": map[string]any{"fetched": float64(12), "fetch_failed": float64(1)}}, patch)

	reset()
	*runSummaryFile = crawl
	patch, err = runPatch()
	require.NoError(t, err)
	require.Equal(t, float64(12), patch["fetched"], "no key: merged at the top")

	reset()
	*runSummaryFile, *runSummaryKey = filepath.Join(dir, "missing.json"), "crawl"
	patch, err = runPatch()
	require.NoError(t, err, "a step that failed early wrote no summary")
	require.Empty(t, patch)

	reset()
	*runSummary = "not json"
	_, err = runPatch()
	require.Error(t, err)
}

// The import records its step and counts in the run sync.sh opened.
func TestRecordImport(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.LegalSyncRun{}))
	runs := repository.NewLegalSyncRunRepository(db)
	ctx := context.Background()
	t.Cleanup(func() {
		*runID = 0
		importStats, importWarnings = map[string]any{}, nil
	})

	// no run id (an untracked sync or a manual import): nothing recorded
	*runID = 0
	recordImportStart(ctx, runs)
	recordImport(ctx, runs)
	all, err := runs.List(ctx, "", 10)
	require.NoError(t, err)
	require.Empty(t, all)

	id, err := runs.Start(ctx, types.LegalSyncWorkerVBPL, "crawl", nil)
	require.NoError(t, err)
	*runID = id
	recordImportStart(ctx, runs)
	stat("documents", 17962)
	stat("queued_auto_ingest", 3)
	warnf("link portal documents: %v", "timeout")
	recordImport(ctx, runs)

	last, _, err := runs.Latest(ctx, types.LegalSyncWorkerVBPL)
	require.NoError(t, err)
	require.Equal(t, "import", last.Step)
	require.Equal(t, types.LegalSyncRunning, last.Status, "sync.sh closes the run after the push")
	var sum map[string]any
	require.NoError(t, json.Unmarshal(last.Summary, &sum))
	imp := sum["import"].(map[string]any)
	require.Equal(t, float64(17962), imp["documents"])
	require.Equal(t, float64(3), imp["queued_auto_ingest"])
	require.Equal(t, []any{"link portal documents: timeout"}, imp["warnings"])
	steps := sum[types.LegalSyncRunSteps].(map[string]any)
	require.Contains(t, steps["crawl"], "finished_at", "moving to import closes the crawl step")
}
