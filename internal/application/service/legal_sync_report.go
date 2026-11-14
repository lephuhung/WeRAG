package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"

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
)

// legalSyncStepName: the step column is VARCHAR(32).
var legalSyncStepName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

func legalSyncReportStep(step string) error {
	if step != "" && !legalSyncStepName.MatchString(step) {
		return fmt.Errorf("%w: step %q", ErrLegalSyncReportInput, step)
	}
	return nil
}

// legalSyncReportPatch drops the keys the repository owns (the step
// timings) from a reported summary.
func legalSyncReportPatch(summary map[string]any) map[string]any {
	if len(summary) == 0 {
		return nil
	}
	out := make(map[string]any, len(summary))
	for k, v := range summary {
		if k == types.LegalSyncRunSteps || k == "" {
			continue
		}
		out[k] = v
	}
	return out
}

// reportedRun checks that id is a run of a reporting worker.
func (m *LegalSyncMonitor) reportedRun(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrLegalSyncReportRun
	}
	run, err := m.runs.Get(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrLegalSyncReportRun
	}
	if err != nil {
		return err
	}
	if !LegalSyncReportingWorkers[run.Worker] {
		return ErrLegalSyncReportRun // another worker's run is not the caller's business
	}
	return nil
}

// StartReportedRun opens a running run of a reporting worker at step.
func (m *LegalSyncMonitor) StartReportedRun(ctx context.Context, worker, step string, summary map[string]any) (int64, error) {
	if !LegalSyncReportingWorkers[worker] {
		return 0, fmt.Errorf("%w: %q", ErrLegalSyncReportWorker, worker)
	}
	if err := legalSyncReportStep(step); err != nil {
		return 0, err
	}
	return m.runs.Start(ctx, worker, step, legalSyncReportPatch(summary))
}

// UpdateReportedRun moves a reported run to step ("" keeps it) and merges
// summary into it.
func (m *LegalSyncMonitor) UpdateReportedRun(ctx context.Context, id int64, step string, summary map[string]any) error {
	if err := legalSyncReportStep(step); err != nil {
		return err
	}
	if err := m.reportedRun(ctx, id); err != nil {
		return err
	}
	return m.runs.Update(ctx, id, step, legalSyncReportPatch(summary))
}

// TouchReportedRun records a sign of life of a reported run.
func (m *LegalSyncMonitor) TouchReportedRun(ctx context.Context, id int64) error {
	if err := m.reportedRun(ctx, id); err != nil {
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
	if err := legalSyncReportStep(step); err != nil {
		return err
	}
	if err := m.reportedRun(ctx, id); err != nil {
		return err
	}
	if status == types.LegalSyncOK {
		errText = ""
	}
	return m.runs.Finish(ctx, id, status, step, errText, legalSyncReportPatch(summary))
}
