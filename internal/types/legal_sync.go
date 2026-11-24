package types

import (
	"strings"
	"time"
)

// Workers of the legal data pipeline watched by the SuperAdmin monitor.
const (
	// LegalSyncWorkerVBPL is the nightly vbpl.vn sync (deploy/vbpl-sync):
	// steps crawl → import → push.
	LegalSyncWorkerVBPL = "vbpl_sync"
	// LegalSyncWorkerDCS is the Party documents sync (deploy/dcs-sync),
	// every 4 hours: steps fetch → build → upload. It has no database
	// credentials and records its runs through the backend's run endpoints
	// (POST /system/legal-sync/runs …).
	LegalSyncWorkerDCS = "dcs_sync"
	// LegalSyncWorkerAutoIngest is the backend worker adding the documents
	// the sync queued (service.LegalAutoIngest).
	LegalSyncWorkerAutoIngest = "auto_ingest"
	// LegalSyncWorkerExpiredRetention is the backend worker removing the
	// full text of fully expired documents (service.LegalExpiredRetention).
	LegalSyncWorkerExpiredRetention = "expired_retention"
	// LegalSyncWorkerImageCheck is the host's daily check that the
	// containers built from the repo run the code of HEAD
	// (scripts/check-images.sh --report, step "check"): a failed run lists
	// the stale images. It records its runs through the run endpoints.
	LegalSyncWorkerImageCheck = "image_check"
	// LegalSyncWorkerInvariants is the backend's daily data check
	// (service.LegalInvariants, step "audit"): a snapshot of the legal
	// tables' counts compared with the previous one; a failed run lists the
	// drops and the broken links. It writes its own runs.
	LegalSyncWorkerInvariants = "invariants"
)

// LegalInvariantsRunAtSettingKey is the Vietnam time of day ("HH:MM") the
// daily data check runs at, after the nightly vbpl.vn sync (02:30).
const LegalInvariantsRunAtSettingKey = "legal.invariants.run_at"

// LegalInvariantsDefaultRunAt is that setting's default.
const LegalInvariantsDefaultRunAt = "04:30"

// ParseLegalInvariantsRunAt reads an "HH:MM" time of day (24 h).
func ParseLegalInvariantsRunAt(s string) (hour, minute int, ok bool) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, 0, false
	}
	return t.Hour(), t.Minute(), true
}

// LegalInvariantsDropPercentSettingKey / LegalInvariantsDropMinRowsSettingKey:
// a guarded count that fell by more than this percentage AND more than
// this many rows since the previous check fails the check.
const (
	LegalInvariantsDropPercentSettingKey = "legal.invariants.drop_percent"
	LegalInvariantsDropMinRowsSettingKey = "legal.invariants.drop_min_rows"
)

// LegalSyncAlertEnabledSettingKey switches the SuperAdmin alert of the
// legal data workers (legal_sync_alert); on by default, off to silence a
// sync stopped on purpose.
const LegalSyncAlertEnabledSettingKey = "legal.sync_alert.enabled"

// LegalSyncAlertDisabledWorkersSettingKey silences the alert of some
// workers only (comma list of LegalSyncWorker* names, e.g. "dcs_sync"
// while that container is stopped on purpose); the page still judges
// them. Empty by default.
const LegalSyncAlertDisabledWorkersSettingKey = "legal.sync_alert.disabled_workers"

// LegalSyncWorkers are the workers the monitor watches, in its order.
var LegalSyncWorkers = []string{LegalSyncWorkerVBPL, LegalSyncWorkerDCS, LegalSyncWorkerAutoIngest,
	LegalSyncWorkerExpiredRetention, LegalSyncWorkerImageCheck, LegalSyncWorkerInvariants}

// ParseLegalSyncWorkerList splits a comma list of worker names (spaces
// and empty items dropped) and returns the names that are no worker.
func ParseLegalSyncWorkerList(s string) (workers map[string]bool, unknown []string) {
	workers = map[string]bool{}
	known := map[string]bool{}
	for _, w := range LegalSyncWorkers {
		known[w] = true
	}
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !known[item] {
			unknown = append(unknown, item)
			continue
		}
		workers[item] = true
	}
	return workers, unknown
}

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
	// HeartbeatAt is the run's last sign of life (each write, and the
	// sync's ticker every few minutes): a running run silent for long is
	// dead (container killed, database lost).
	HeartbeatAt *time.Time `json:"heartbeat_at"`
}

// TableName is the table of LegalSyncRun.
func (LegalSyncRun) TableName() string { return "legal_sync_runs" }
