package types

import "time"

// Workers of the legal data pipeline watched by the SuperAdmin monitor.
const (
	// LegalSyncWorkerVBPL is the nightly vbpl.vn sync (deploy/vbpl-sync):
	// steps crawl → import → push.
	LegalSyncWorkerVBPL = "vbpl_sync"
	// LegalSyncWorkerAutoIngest is the backend worker adding the documents
	// the sync queued (service.LegalAutoIngest).
	LegalSyncWorkerAutoIngest = "auto_ingest"
	// LegalSyncWorkerExpiredRetention is the backend worker removing the
	// full text of fully expired documents (service.LegalExpiredRetention).
	LegalSyncWorkerExpiredRetention = "expired_retention"
)

// Statuses of a LegalSyncRun.
const (
	LegalSyncRunning = "running"
	LegalSyncOK      = "ok"
	LegalSyncFailed  = "failed"
)

// LegalSyncRunSteps is the summary key of the per-step timings:
// {"crawl": {"started_at": …, "finished_at": …}, …}.
const LegalSyncRunSteps = "steps"

// LegalSyncRun is one run of a legal data worker. Summary holds the run's
// counts (by step: "crawl", "import", or the worker's own keys) and the
// step timings under LegalSyncRunSteps; Error the failed step's output.
type LegalSyncRun struct {
	ID         int64      `json:"id"          gorm:"primaryKey;autoIncrement"`
	Worker     string     `json:"worker"      gorm:"type:varchar(64);not null;index:idx_legal_sync_runs_worker_started,priority:1"`
	Status     string     `json:"status"      gorm:"type:varchar(16);not null;default:'running'"`
	Step       string     `json:"step"        gorm:"type:varchar(32);not null;default:''"`
	StartedAt  time.Time  `json:"started_at"  gorm:"not null;index:idx_legal_sync_runs_worker_started,priority:2,sort:desc"`
	FinishedAt *time.Time `json:"finished_at"`
	Summary    JSON       `json:"summary"     gorm:"type:jsonb"`
	Error      string     `json:"error"       gorm:"type:text;not null;default:''"`
}

// TableName is the table of LegalSyncRun.
func (LegalSyncRun) TableName() string { return "legal_sync_runs" }
