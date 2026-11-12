package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// legalSyncErrorMax bounds the stored error: the tail of a step's output.
const legalSyncErrorMax = 4000

// LegalSyncRecordKeep is how long a backend worker's passes are kept: they
// come every few minutes while there is work.
const LegalSyncRecordKeep = 30 * 24 * time.Hour

// legalSyncInterrupted is the error of a run left open by a sync that was
// killed (container stop, redeploy) or could not reach the database at its
// end: the next run of the worker closes it.
const legalSyncInterrupted = "Bị ngắt: lần chạy không được đóng (container dừng hoặc mất kết nối cơ sở dữ liệu)."

// LegalSyncRunRepository writes and reads types.LegalSyncRun rows. The
// nightly sync (cmd/legal-catalog-import run flags) and the backend workers
// share it. A run's summary is merged in Go (read, merge, write): one run
// has one writer, and the same code runs on the SQLite test schema.
type LegalSyncRunRepository struct {
	db  *gorm.DB
	now func() time.Time
}

// NewLegalSyncRunRepository creates the repository.
func NewLegalSyncRunRepository(db *gorm.DB) *LegalSyncRunRepository {
	return &LegalSyncRunRepository{db: db, now: time.Now}
}

// Start opens a running run at step (its timing starts now) and returns its
// id. Runs of the worker still open are closed as failed first: one sync
// runs at a time (sync.sh holds a lock), so they were interrupted.
func (r *LegalSyncRunRepository) Start(ctx context.Context, worker, step string, summary map[string]any) (int64, error) {
	now := r.now()
	if err := r.db.WithContext(ctx).Model(&types.LegalSyncRun{}).
		Where("worker = ? AND status = ?", worker, types.LegalSyncRunning).
		Updates(map[string]any{"status": types.LegalSyncFailed, "finished_at": now, "error": legalSyncInterrupted}).Error; err != nil {
		return 0, fmt.Errorf("close interrupted %s runs: %w", worker, err)
	}
	sum := mergeLegalSyncSummary(nil, summary)
	if step != "" {
		legalSyncOpenStep(sum, step, now)
	}
	raw, err := json.Marshal(sum)
	if err != nil {
		return 0, err
	}
	run := &types.LegalSyncRun{Worker: worker, Status: types.LegalSyncRunning, Step: step, StartedAt: now, Summary: raw}
	if err := r.db.WithContext(ctx).Create(run).Error; err != nil {
		return 0, fmt.Errorf("start %s run: %w", worker, err)
	}
	return run.ID, nil
}

// Update moves a running run to step (closing the timing of the step it
// was at) and merges patch into its summary; step "" keeps the step.
func (r *LegalSyncRunRepository) Update(ctx context.Context, id int64, step string, patch map[string]any) error {
	return r.change(ctx, id, func(run *types.LegalSyncRun, sum map[string]any, now time.Time) {
		if step != "" && step != run.Step {
			legalSyncCloseStep(sum, run.Step, now)
			legalSyncOpenStep(sum, step, now)
			run.Step = step
		}
		mergeLegalSyncSummary(sum, patch)
	})
}

// Finish closes a run as ok or failed. A failed run keeps the step it
// failed at (or step, when given) and the error's tail.
func (r *LegalSyncRunRepository) Finish(ctx context.Context, id int64, status, step, errText string, patch map[string]any) error {
	if status != types.LegalSyncOK && status != types.LegalSyncFailed {
		return fmt.Errorf("finish run %d: status %q is not ok or failed", id, status)
	}
	return r.change(ctx, id, func(run *types.LegalSyncRun, sum map[string]any, now time.Time) {
		if step != "" && step != run.Step {
			legalSyncCloseStep(sum, run.Step, now)
			legalSyncOpenStep(sum, step, now)
			run.Step = step
		}
		legalSyncCloseStep(sum, run.Step, now)
		mergeLegalSyncSummary(sum, patch)
		run.Status = status
		run.FinishedAt = &now
		run.Error = legalSyncTail(errText, legalSyncErrorMax)
	})
}

// Record writes a finished run in one go: a backend worker's pass. Passes
// older than LegalSyncRecordKeep are pruned.
func (r *LegalSyncRunRepository) Record(ctx context.Context, worker string, started time.Time, status, errText string,
	summary map[string]any,
) error {
	now := r.now()
	raw, err := json.Marshal(mergeLegalSyncSummary(nil, summary))
	if err != nil {
		return err
	}
	db := r.db.WithContext(ctx)
	if err := db.Create(&types.LegalSyncRun{
		Worker: worker, Status: status, StartedAt: started, FinishedAt: &now, Summary: raw,
		Error: legalSyncTail(errText, legalSyncErrorMax),
	}).Error; err != nil {
		return err
	}
	return db.Where("worker = ? AND started_at < ?", worker, now.Add(-LegalSyncRecordKeep)).Delete(&types.LegalSyncRun{}).Error
}

// List returns the latest runs, newest first; worker "" lists every worker.
func (r *LegalSyncRunRepository) List(ctx context.Context, worker string, limit int) ([]*types.LegalSyncRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	q := r.db.WithContext(ctx).Model(&types.LegalSyncRun{}).Order("started_at DESC").Order("id DESC").Limit(limit)
	if worker != "" {
		q = q.Where("worker = ?", worker)
	}
	var runs []*types.LegalSyncRun
	return runs, q.Find(&runs).Error
}

// Latest returns the worker's newest run and its newest successful run
// (either nil when there is none).
func (r *LegalSyncRunRepository) Latest(ctx context.Context, worker string) (last, lastOK *types.LegalSyncRun, err error) {
	db := r.db.WithContext(ctx)
	var rows []*types.LegalSyncRun
	if err = db.Where("worker = ?", worker).Order("started_at DESC").Order("id DESC").Limit(1).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	if len(rows) > 0 {
		last = rows[0]
	}
	rows = nil
	if err = db.Where("worker = ? AND status = ?", worker, types.LegalSyncOK).
		Order("started_at DESC").Order("id DESC").Limit(1).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	if len(rows) > 0 {
		lastOK = rows[0]
	}
	return last, lastOK, nil
}

// change loads a running run, applies edit and saves it. One run has one
// writer (the sync's steps run one after another), so no row lock.
func (r *LegalSyncRunRepository) change(ctx context.Context, id int64,
	edit func(run *types.LegalSyncRun, sum map[string]any, now time.Time),
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run types.LegalSyncRun
		if err := tx.First(&run, "id = ?", id).Error; err != nil {
			return fmt.Errorf("run %d: %w", id, err)
		}
		if run.Status != types.LegalSyncRunning {
			return fmt.Errorf("run %d is %s, not running", id, run.Status)
		}
		sum := map[string]any{}
		if len(run.Summary) > 0 {
			if err := json.Unmarshal(run.Summary, &sum); err != nil {
				sum = map[string]any{}
			}
		}
		edit(&run, sum, r.now())
		raw, err := json.Marshal(sum)
		if err != nil {
			return err
		}
		run.Summary = raw
		return tx.Model(&types.LegalSyncRun{}).Where("id = ?", id).Updates(map[string]any{
			"status": run.Status, "step": run.Step, "finished_at": run.FinishedAt, "summary": run.Summary, "error": run.Error,
		}).Error
	})
}

// mergeLegalSyncSummary merges patch into sum (one level deep for maps, so
// {"import": {...}} adds to an existing "import"); a nil sum is created.
func mergeLegalSyncSummary(sum, patch map[string]any) map[string]any {
	if sum == nil {
		sum = map[string]any{}
	}
	for k, v := range patch {
		if pv, ok := v.(map[string]any); ok {
			if sv, ok := sum[k].(map[string]any); ok {
				for kk, vv := range pv {
					sv[kk] = vv
				}
				continue
			}
		}
		sum[k] = v
	}
	return sum
}

func legalSyncSteps(sum map[string]any) map[string]any {
	steps, ok := sum[types.LegalSyncRunSteps].(map[string]any)
	if !ok {
		steps = map[string]any{}
		sum[types.LegalSyncRunSteps] = steps
	}
	return steps
}

func legalSyncOpenStep(sum map[string]any, step string, now time.Time) {
	legalSyncSteps(sum)[step] = map[string]any{"started_at": now.UTC().Format(time.RFC3339)}
}

func legalSyncCloseStep(sum map[string]any, step string, now time.Time) {
	if step == "" {
		return
	}
	steps := legalSyncSteps(sum)
	t, ok := steps[step].(map[string]any)
	if !ok {
		return
	}
	if _, done := t["finished_at"]; !done {
		t["finished_at"] = now.UTC().Format(time.RFC3339)
	}
}

// legalSyncTail keeps the last max bytes of s on a rune boundary: the end
// of a step's output holds the error. Invalid UTF-8 (a shell tail cut inside
// a character) is dropped: Postgres refuses it.
func legalSyncTail(s string, max int) string {
	s = strings.TrimSpace(strings.ToValidUTF8(s, ""))
	if len(s) <= max {
		return s
	}
	s = s[len(s)-max:]
	for i := 0; i < len(s) && i < 4; i++ {
		if s[i]&0xC0 != 0x80 {
			return "…" + s[i:]
		}
	}
	return "…" + s
}
