package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// Run tracking over HTTP for the scheduled syncs without database
// credentials (LegalSyncReportingWorkers: deploy/dcs-sync). The same
// repository calls as legal-catalog-import's -run-* flags (vbpl-sync), so
// the same semantics: a start closes the worker's runs left open as
// interrupted, every write is a sign of life, a run the monitor closed for
// silence comes back on the next write unless a newer run started.
//
// A key holder must not be able to grow the table: the steps are the
// worker's own (legalSyncReportSteps, which also caps the step timings),
// the summary takes only the known keys with scalar counts, and a run's
// summary stays under legalSyncReportSummaryMax. A start is refused while
// another run of the worker is alive (a write within legalSyncReportBusy):
// two containers would otherwise close each other's runs as interrupted.

var (
	// ErrLegalSyncReportWorker: the worker does not report over HTTP (the
	// nightly vbpl.vn sync and the backend workers write their own runs).
	ErrLegalSyncReportWorker = errors.New("worker does not record its runs through the API")
	// ErrLegalSyncReportRun: no run of a reporting worker has this id.
	ErrLegalSyncReportRun = errors.New("run not found")
	// ErrLegalSyncReportInput: a step name, status or summary is invalid.
	ErrLegalSyncReportInput = errors.New("invalid run report")
	// ErrLegalSyncReportClosed: the run is not running any more.
	ErrLegalSyncReportClosed = repository.ErrLegalSyncRunClosed
	// ErrLegalSyncReportBusy: another run of the worker is alive.
	ErrLegalSyncReportBusy = errors.New("another run of the worker is in progress")
)

const (
	// legalSyncReportSummaryMax bounds a reported run's summary (JSON bytes,
	// step timings included); a real dcs-sync run writes well under 1 KB.
	legalSyncReportSummaryMax = 32 << 10
	// legalSyncReportBusy: a running run written to this recently is alive
	// (sync.sh beats every minute); an older one was left by a killed sync
	// and the start closes it as interrupted.
	legalSyncReportBusy = 2 * time.Minute
	// legalSyncReportCountKeys bounds the counts of one step.
	legalSyncReportCountKeys = 32
	legalSyncReportStringMax = 200
)

// legalSyncReportSteps are the steps of each reporting worker.
var legalSyncReportSteps = map[string]map[string]bool{
	types.LegalSyncWorkerDCS: {"fetch": true, "build": true, "upload": true},
}

func legalSyncReportStep(worker, step string) error {
	if step != "" && !legalSyncReportSteps[worker][step] {
		return fmt.Errorf("%w: step %q", ErrLegalSyncReportInput, step)
	}
	return nil
}

// legalSyncReportPatch checks a reported summary: "schedule" (a string),
// "untracked" (a boolean) and, under a step's name, that step's counts
// (scalar values). Anything else is refused.
func legalSyncReportPatch(worker string, summary map[string]any) (map[string]any, error) {
	if len(summary) == 0 {
		return nil, nil
	}
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: summary "+format, append([]any{ErrLegalSyncReportInput}, args...)...)
	}
	scalar := func(v any) bool {
		switch x := v.(type) {
		case bool, float64, json.Number, int, int64, nil:
			return true
		case string:
			return len(x) <= legalSyncReportStringMax
		}
		return false
	}
	out := make(map[string]any, len(summary))
	for k, v := range summary {
		switch {
		case k == "schedule":
			if s, ok := v.(string); !ok || len(s) > 64 {
				return nil, bad("schedule must be a short string")
			}
		case k == "untracked":
			if _, ok := v.(bool); !ok {
				return nil, bad("untracked must be a boolean")
			}
		case legalSyncReportSteps[worker][k]:
			counts, ok := v.(map[string]any)
			if !ok || len(counts) > legalSyncReportCountKeys {
				return nil, bad("%s must be an object of at most %d counts", k, legalSyncReportCountKeys)
			}
			for ck, cv := range counts {
				if ck == "" || len(ck) > 64 || !scalar(cv) {
					return nil, bad("%s.%s must be a number, a boolean or a short string", k, ck)
				}
			}
		default:
			return nil, bad("key %q is not allowed", k)
		}
		out[k] = v
	}
	if raw, _ := json.Marshal(out); len(raw) > legalSyncReportSummaryMax {
		return nil, bad("larger than %d bytes", legalSyncReportSummaryMax)
	}
	return out, nil
}

// legalSyncReportWrite maps a capped write's refusal to bad input.
func legalSyncReportWrite(err error) error {
	if errors.Is(err, repository.ErrLegalSyncSummaryTooLarge) {
		return fmt.Errorf("%w: %v", ErrLegalSyncReportInput, err)
	}
	return err
}

// reportedRun returns the worker of run id, a run of a reporting worker.
func (m *LegalSyncMonitor) reportedRun(ctx context.Context, id int64) (string, error) {
	if id <= 0 {
		return "", ErrLegalSyncReportRun
	}
	run, err := m.runs.Get(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrLegalSyncReportRun
	}
	if err != nil {
		return "", err
	}
	if !LegalSyncReportingWorkers[run.Worker] {
		return "", ErrLegalSyncReportRun // another worker's run is not the caller's business
	}
	return run.Worker, nil
}

// StartReportedRun opens a running run of a reporting worker at step.
func (m *LegalSyncMonitor) StartReportedRun(ctx context.Context, worker, step string, summary map[string]any) (int64, error) {
	if !LegalSyncReportingWorkers[worker] {
		return 0, fmt.Errorf("%w: %q", ErrLegalSyncReportWorker, worker)
	}
	if err := legalSyncReportStep(worker, step); err != nil {
		return 0, err
	}
	patch, err := legalSyncReportPatch(worker, summary)
	if err != nil {
		return 0, err
	}
	open, err := m.runs.LatestRunning(ctx, worker)
	if err != nil {
		return 0, err
	}
	if open != nil {
		beat := open.StartedAt
		if open.HeartbeatAt != nil && open.HeartbeatAt.After(beat) {
			beat = *open.HeartbeatAt
		}
		if m.now().Sub(beat) < legalSyncReportBusy {
			return 0, fmt.Errorf("%w: run %d, last sign of life %s", ErrLegalSyncReportBusy, open.ID,
				beat.UTC().Format(time.RFC3339))
		}
	}
	return m.runs.Start(ctx, worker, step, patch)
}

// UpdateReportedRun moves a reported run to step ("" keeps it) and merges
// summary into it.
func (m *LegalSyncMonitor) UpdateReportedRun(ctx context.Context, id int64, step string, summary map[string]any) error {
	worker, err := m.reportedRun(ctx, id)
	if err != nil {
		return err
	}
	if err := legalSyncReportStep(worker, step); err != nil {
		return err
	}
	patch, err := legalSyncReportPatch(worker, summary)
	if err != nil {
		return err
	}
	return legalSyncReportWrite(m.runs.UpdateCapped(ctx, id, step, patch, legalSyncReportSummaryMax))
}

// TouchReportedRun records a sign of life of a reported run.
func (m *LegalSyncMonitor) TouchReportedRun(ctx context.Context, id int64) error {
	if _, err := m.reportedRun(ctx, id); err != nil {
		return err
	}
	return m.runs.Touch(ctx, id)
}

// FinishReportedRun closes a reported run as ok or failed; a failed run
// keeps errText's tail (control characters and colour codes dropped).
func (m *LegalSyncMonitor) FinishReportedRun(ctx context.Context, id int64, status, step, errText string,
	summary map[string]any,
) error {
	if status != types.LegalSyncOK && status != types.LegalSyncFailed {
		return fmt.Errorf("%w: status %q is not ok or failed", ErrLegalSyncReportInput, status)
	}
	worker, err := m.reportedRun(ctx, id)
	if err != nil {
		return err
	}
	if err := legalSyncReportStep(worker, step); err != nil {
		return err
	}
	patch, err := legalSyncReportPatch(worker, summary)
	if err != nil {
		return err
	}
	if status == types.LegalSyncOK {
		errText = ""
	}
	return legalSyncReportWrite(m.runs.FinishCapped(ctx, id, status, step, errText, patch, legalSyncReportSummaryMax))
}
