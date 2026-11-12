package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	gormlogger "gorm.io/gorm/logger"
)

// Run tracking for the SuperAdmin monitor (legal_sync_runs): deploy/vbpl-sync
// calls this binary around its steps.
//
//	id=$(legal-catalog-import -run-start -run-worker vbpl_sync -run-step crawl -run-summary '{"schedule":"30 2 * * *"}')
//	legal-catalog-import -run-id "$id" -run-step import -run-summary-key crawl -run-summary-file crawl.json
//	legal-catalog-import -documents … -run-id "$id"   # the import adds its counts under "import"
//	legal-catalog-import -run-id "$id" -run-heartbeat               # a sign of life (sync.sh ticker)
//	legal-catalog-import -run-id "$id" -run-finish failed -run-error-file step.log
var (
	runStart       = flag.Bool("run-start", false, "Open a run (legal_sync_runs) and print its id")
	runWorker      = flag.String("run-worker", types.LegalSyncWorkerVBPL, "Worker of the run opened by -run-start")
	runID          = flag.Int64("run-id", 0, "Run to update (with -run-step / -run-summary* / -run-finish) or to record the import's counts in")
	runStep        = flag.String("run-step", "", "Step the run moves to")
	runFinish      = flag.String("run-finish", "", "Close the run: ok or failed")
	runSummary     = flag.String("run-summary", "", "JSON object merged into the run's summary")
	runSummaryFile = flag.String("run-summary-file", "", "JSON file merged into the run's summary (under -run-summary-key when set)")
	runSummaryKey  = flag.String("run-summary-key", "", "Summary key the -run-summary-file object goes under")
	runErrorFile   = flag.String("run-error-file", "", "File whose tail is the failed run's error")
	runHeartbeat   = flag.Bool("run-heartbeat", false, "Record a sign of life of the run (a running run silent for 30 minutes is closed)")
)

// runControlConnectTimeout bounds the connection of the run-control mode:
// a tracking call must never hold the sync up.
const runControlConnectTimeout = 10

// importStats are the import's counts, recorded under "import" in the run.
var importStats = map[string]any{}

// stat records one count of the import.
func stat(key string, v any) { importStats[key] = v }

// importWarnings are the non-fatal failures of the import.
var importWarnings []string

// warnf logs a failure the import goes on after and keeps it for the run.
func warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Print(msg)
	importWarnings = append(importWarnings, msg)
}

// runControlRequested: the flags ask for run tracking only, no import.
func runControlRequested() bool {
	return *runStart || (*runID > 0 && (*runFinish != "" || *runStep != "" || *runSummary != "" || *runSummaryFile != "" ||
		*runHeartbeat))
}

// runControl opens, moves or closes a run.
func runControl() error {
	db, err := openDBWithTimeout(gormlogger.Discard, runControlConnectTimeout)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	ctx := context.Background()
	runs := repository.NewLegalSyncRunRepository(db)
	patch, err := runPatch()
	if err != nil {
		return err
	}
	if *runStart {
		id, err := runs.Start(ctx, strings.TrimSpace(*runWorker), *runStep, patch)
		if err != nil {
			return err
		}
		fmt.Println(id)
		return nil
	}
	if *runFinish != "" {
		errText := ""
		if *runErrorFile != "" {
			if b, err := os.ReadFile(*runErrorFile); err == nil {
				errText = string(b)
			}
		}
		return runs.Finish(ctx, *runID, *runFinish, *runStep, errText, patch)
	}
	if *runHeartbeat && *runStep == "" && len(patch) == 0 {
		return runs.Touch(ctx, *runID)
	}
	return runs.Update(ctx, *runID, *runStep, patch)
}

func runPatch() (map[string]any, error) {
	patch := map[string]any{}
	if *runSummary != "" {
		if err := json.Unmarshal([]byte(*runSummary), &patch); err != nil {
			return nil, fmt.Errorf("-run-summary: %w", err)
		}
	}
	if *runSummaryFile != "" {
		b, err := os.ReadFile(*runSummaryFile)
		if err != nil {
			// a step that failed early wrote no summary: nothing to merge
			log.Printf("run summary %s: %v", *runSummaryFile, err)
			return patch, nil
		}
		var obj map[string]any
		if err := json.Unmarshal(b, &obj); err != nil {
			return nil, fmt.Errorf("-run-summary-file: %w", err)
		}
		if *runSummaryKey != "" {
			patch[*runSummaryKey] = obj
		} else {
			for k, v := range obj {
				patch[k] = v
			}
		}
	}
	return patch, nil
}

// recordImport moves the run to the import step when it starts.
func recordImportStart(ctx context.Context, runs *repository.LegalSyncRunRepository) {
	if *runID <= 0 {
		return
	}
	if err := runs.Update(ctx, *runID, "import", nil); err != nil {
		log.Printf("run %d: %v", *runID, err)
	}
}

// recordImport adds the import's counts to the run.
func recordImport(ctx context.Context, runs *repository.LegalSyncRunRepository) {
	if *runID <= 0 {
		return
	}
	if len(importWarnings) > 0 {
		importStats["warnings"] = importWarnings
	}
	if err := runs.Update(ctx, *runID, "", map[string]any{"import": importStats}); err != nil {
		log.Printf("run %d: %v", *runID, err)
	}
}
