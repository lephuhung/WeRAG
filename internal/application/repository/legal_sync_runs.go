package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
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

// LegalSyncRunKeep is how long the runs of the nightly sync (and any other
// worker) are kept; Start prunes older ones.
const LegalSyncRunKeep = 180 * 24 * time.Hour

// legalSyncInterrupted is the error of a run left open by a sync that was
// killed (container stop, redeploy) or could not reach the database at its
// end: the next run of the worker closes it.
const legalSyncInterrupted = "Bị ngắt: lần chạy không được đóng (container dừng hoặc mất kết nối cơ sở dữ liệu)."

// LegalSyncHeartbeatLost is the error of a running run the monitor closed
// because it gave no sign of life (CloseSilent). Such a run comes back when
// its sync writes again (the database was only out of reach).
const LegalSyncHeartbeatLost = "Bị ngắt: lần chạy không còn tín hiệu hơn 30 phút (container dừng hoặc mất kết nối cơ sở dữ liệu)."

// legalSyncControl drops control characters but newline and tab, ANSI
// escape sequences and carriage returns (progress bars) from an error.
var legalSyncControl = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b[@-_]|[\x00-\x08\x0b-\x1f\x7f]`)

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
	run := &types.LegalSyncRun{Worker: worker, Status: types.LegalSyncRunning, Step: step, StartedAt: now, Summary: raw,
		HeartbeatAt: &now}
	if err := r.db.WithContext(ctx).Create(run).Error; err != nil {
		return 0, fmt.Errorf("start %s run: %w", worker, err)
	}
	// keeping the history is not worth failing the start
	_ = r.db.WithContext(ctx).Where("worker = ? AND started_at < ?", worker, now.Add(-LegalSyncRunKeep)).
		Delete(&types.LegalSyncRun{}).Error
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

// Touch records a sign of life of a running run (the sync's ticker): one
// UPDATE; only a run that is not running (closed for silence: it may come
// back) goes through change.
func (r *LegalSyncRunRepository) Touch(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Model(&types.LegalSyncRun{}).
		Where("id = ? AND status = ?", id, types.LegalSyncRunning).Update("heartbeat_at", r.now())
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}
	return r.change(ctx, id, func(*types.LegalSyncRun, map[string]any, time.Time) {})
}

// CloseSilent closes as failed (LegalSyncHeartbeatLost) the running runs
// of worker whose last sign of life is before silentBefore; returns how
// many.
func (r *LegalSyncRunRepository) CloseSilent(ctx context.Context, worker string, silentBefore time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Model(&types.LegalSyncRun{}).
		Where("worker = ? AND status = ? AND COALESCE(heartbeat_at, started_at) < ?", worker, types.LegalSyncRunning, silentBefore).
		Updates(map[string]any{"status": types.LegalSyncFailed, "finished_at": r.now(), "error": LegalSyncHeartbeatLost})
	return res.RowsAffected, res.Error
}

// LatestFinished returns the worker's newest run that is not running (nil
// when there is none).
func (r *LegalSyncRunRepository) LatestFinished(ctx context.Context, worker string) (*types.LegalSyncRun, error) {
	var rows []*types.LegalSyncRun
	if err := r.db.WithContext(ctx).Where("worker = ? AND status <> ?", worker, types.LegalSyncRunning).
		Order("started_at DESC").Order("id DESC").Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
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
			// closed by the monitor for silence while the sync went on (the
			// database was out of reach): it comes back, unless a newer run
			// of the worker started meanwhile
			var newer int64
			if run.Status == types.LegalSyncFailed && run.Error == LegalSyncHeartbeatLost {
				if err := tx.Model(&types.LegalSyncRun{}).Where("worker = ? AND id > ?", run.Worker, run.ID).
					Count(&newer).Error; err != nil {
					return err
				}
			}
			if run.Status != types.LegalSyncFailed || run.Error != LegalSyncHeartbeatLost || newer > 0 {
				return fmt.Errorf("run %d is %s, not running", id, run.Status)
			}
			run.Status, run.FinishedAt, run.Error = types.LegalSyncRunning, nil, ""
		}
		sum := map[string]any{}
		if len(run.Summary) > 0 {
			if err := json.Unmarshal(run.Summary, &sum); err != nil {
				sum = map[string]any{}
			}
		}
		now := r.now()
		edit(&run, sum, now)
		raw, err := json.Marshal(sum)
		if err != nil {
			return err
		}
		run.Summary = raw
		return tx.Model(&types.LegalSyncRun{}).Where("id = ?", id).Updates(map[string]any{
			"status": run.Status, "step": run.Step, "finished_at": run.FinishedAt, "summary": run.Summary, "error": run.Error,
			"heartbeat_at": now,
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
// a character) is dropped: Postgres refuses it; so are NUL, other control
// characters and ANSI colour codes (a carriage return becomes a newline).
func legalSyncTail(s string, max int) string {
	s = strings.ReplaceAll(strings.ToValidUTF8(s, ""), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimSpace(legalSyncControl.ReplaceAllString(s, ""))
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
