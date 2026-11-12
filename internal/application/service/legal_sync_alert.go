package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// The SuperAdmin alert of the legal data workers (legal_sync_alert): every
// LegalSyncMonitorInterval the monitor judges the workers like the page
// does, and a worker with a problem worth a person's attention raises one
// SuperAdmin digest (incidentDigest, like backup_alert); the digest is
// stamped resolved and closed once every worker is healthy again.
//
// Worth an alert: the nightly sync's last run failed, it is running for
// more than 12 h, or no successful run started within 26 h (a sync that
// never reported a run is not deployed: no alert); a backend worker
// missed three passes, never passed since it was started, or its passes
// have failed for legalTickErrorAlertAfter. A backend worker that is
// starting or not started in this process (no Redis) is not a problem.
// The heartbeats live in this process: with Redis one instance checks at
// a time and judges its own workers.

const (
	// LegalSyncMonitorInterval is how often the workers are judged.
	LegalSyncMonitorInterval = 10 * time.Minute
	// LegalSyncAlertLink is the SuperAdmin page of the workers.
	LegalSyncAlertLink = "/platform/admin?section=legal-documents&view=sync"

	// legalSyncMonitorFirstDelay lets the auto-ingest worker beat once
	// before the first check.
	legalSyncMonitorFirstDelay = 5 * time.Minute
	legalSyncMonitorTimeout    = time.Minute
	legalSyncMonitorLockKey    = "werag:legal-sync-monitor:lock"
	legalSyncMonitorLockLease  = 2 * time.Minute
	// legalTickErrorAlertAfter: a backend worker whose passes failed for
	// this long in a row is alerted (one failed pass is not).
	legalTickErrorAlertAfter = time.Hour
	legalSyncAlertNoteRunes  = 120
	legalSyncAlertBodyRunes  = 600
)

// legalSyncAlerts is the alert side of a LegalSyncMonitor.
type legalSyncAlerts struct {
	incident incidentDigest
	redis    *redis.Client
	// checking: one check at a time in this process
	checking sync.Mutex
}

// WithAlerts makes Check keep the legal_sync_alert digest of the
// SuperAdmins.
func (m *LegalSyncMonitor) WithAlerts(users interfaces.UserRepository,
	notifications interfaces.UserNotificationRepository, rdb *redis.Client,
) *LegalSyncMonitor {
	m.alerts = &legalSyncAlerts{
		incident: incidentDigest{
			digests: newReviewDigests("LegalSyncMonitor", users, notifications),
			kind:    types.UserNotificationLegalSyncAlert,
			what:    "legal sync alert",
		},
		redis: rdb,
	}
	return m
}

// Check judges the workers and raises, updates or closes the digest.
func (m *LegalSyncMonitor) Check(ctx context.Context) error {
	if m == nil || m.alerts == nil {
		return nil
	}
	m.alerts.checking.Lock()
	defer m.alerts.checking.Unlock()
	release, ok := monitorLock(ctx, m.alerts.redis, legalSyncMonitorLockKey, legalSyncMonitorLockLease, "LegalSyncMonitor")
	if !ok {
		return nil // another instance is checking
	}
	defer release()
	now := m.now()
	workers, err := m.workers(ctx, now)
	if err != nil {
		return fmt.Errorf("reading the workers: %w", err)
	}
	alert := legalSyncAlertPayload(workers, now)
	if alert.Count == 0 {
		return m.alerts.incident.resolve(ctx)
	}
	return m.alerts.incident.raise(ctx, alert.Signature, func(userID string, at time.Time) *types.UserNotification {
		return legalSyncAlertNotification(userID, alert, at)
	})
}

// legalSyncContainerAlertOrder: the problem a sync's alert names, first
// match wins (a failed run is also stale a day later: one incident).
var legalSyncContainerAlertOrder = []string{LegalSyncReasonLastFailed, LegalSyncReasonRunningLong, LegalSyncReasonStale}

// legalSyncAlertProblem is the problem of w worth an alert, "" for none.
func legalSyncAlertProblem(w LegalSyncWorkerStatus, now time.Time) string {
	has := map[string]bool{}
	for _, r := range w.Reasons {
		has[r] = true
	}
	if w.Kind == "container" {
		for _, r := range legalSyncContainerAlertOrder {
			if has[r] {
				return r
			}
		}
		return ""
	}
	switch {
	case has[LegalSyncReasonBeatStale]:
		return LegalSyncReasonBeatStale
	case has[LegalSyncReasonNoHeartbeat]:
		return LegalSyncReasonNoHeartbeat
	case has[LegalSyncReasonTickError] && w.FailingSince != nil && now.Sub(*w.FailingSince) >= legalTickErrorAlertAfter:
		return LegalSyncReasonTickError
	}
	return ""
}

// legalSyncAlertPayload lists the workers with a problem worth an alert.
func legalSyncAlertPayload(workers []LegalSyncWorkerStatus, now time.Time) types.UserNotificationLegalSyncAlertPayload {
	p := types.UserNotificationLegalSyncAlertPayload{Workers: []types.UserNotificationLegalSyncWorker{}}
	var sig []string
	for _, w := range workers {
		problem := legalSyncAlertProblem(w, now)
		if problem == "" {
			continue
		}
		e := types.UserNotificationLegalSyncWorker{Worker: w.Worker, Problem: problem}
		if w.Kind == "container" {
			if w.Last != nil && problem != LegalSyncReasonStale {
				at := w.Last.StartedAt
				e.At, e.Step = &at, w.Last.Step
				if problem == LegalSyncReasonLastFailed {
					e.Note = legalSyncErrorGist(w.Last.Error)
				}
			}
			if w.LastOK != nil {
				at := w.LastOK.StartedAt
				if w.LastOK.FinishedAt != nil {
					at = *w.LastOK.FinishedAt
				}
				e.LastOKAt = &at
			}
		} else {
			e.At = w.LastTickAt
			if problem == LegalSyncReasonTickError {
				e.Note = legalSyncErrorGist(w.LastError)
			}
		}
		p.Workers = append(p.Workers, e)
		sig = append(sig, w.Worker+":"+problem)
	}
	sort.Strings(sig)
	p.Count, p.Signature = len(p.Workers), strings.Join(sig, ",")
	return p
}

// legalSyncErrorGist is the last non-empty line of an error (a step's
// output tail ends with the error), cut for a notification.
func legalSyncErrorGist(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return queueEllipsis(l, legalSyncAlertNoteRunes)
		}
	}
	return ""
}

// legalSyncAlertNotification builds the digest of userID. Title and body
// are the stored (Vietnamese) text; the clients word it from the payload
// in the reader's language.
func legalSyncAlertNotification(userID string, p types.UserNotificationLegalSyncAlertPayload, at time.Time) *types.UserNotification {
	payload, _ := json.Marshal(p)
	parts := make([]string, 0, len(p.Workers))
	for _, w := range p.Workers {
		parts = append(parts, legalSyncAlertWorkerText(w))
	}
	return &types.UserNotification{
		TenantID:  types.PlatformTenantID,
		UserID:    userID,
		Kind:      types.UserNotificationLegalSyncAlert,
		Title:     fmt.Sprintf("Thu thập dữ liệu pháp lý gặp sự cố: %d việc", p.Count),
		Body:      queueEllipsis(strings.Join(parts, "; "), legalSyncAlertBodyRunes),
		Link:      LegalSyncAlertLink,
		Payload:   types.JSON(payload),
		DedupeKey: types.UserNotificationOpenKey(types.UserNotificationLegalSyncAlert, userID),
		CreatedAt: at,
	}
}

var legalSyncWorkerNamesVI = map[string]string{
	types.LegalSyncWorkerVBPL:             "Đồng bộ vbpl.vn",
	types.LegalSyncWorkerAutoIngest:       "Tự nạp văn bản",
	types.LegalSyncWorkerExpiredRetention: "Xóa toàn văn hết hiệu lực",
}

var legalSyncStepNamesVI = map[string]string{
	"prepare": "chuẩn bị",
	"crawl":   "cào dữ liệu",
	"import":  "nạp danh mục",
	"push":    "sao lưu MinIO",
}

// legalSyncAlertWorkerText: "Đồng bộ vbpl.vn: lỗi ở bước cào dữ liệu lúc
// 02h30 - 12/11/2026, lần thành công cuối … — HTTP 503".
func legalSyncAlertWorkerText(w types.UserNotificationLegalSyncWorker) string {
	name := legalSyncWorkerNamesVI[w.Worker]
	if name == "" {
		name = w.Worker
	}
	step := legalSyncStepNamesVI[w.Step]
	if step == "" {
		step = w.Step
	}
	var s string
	switch w.Problem {
	case LegalSyncReasonLastFailed:
		s = name + ": lỗi"
		if step != "" {
			s += " ở bước " + step
		}
		if w.At != nil {
			s += " (lần chạy lúc " + formatDateTimeVN(*w.At) + ")"
		}
	case LegalSyncReasonRunningLong:
		s = name + ": chạy quá 12 giờ"
		if step != "" {
			s += " ở bước " + step
		}
	case LegalSyncReasonStale:
		s = name + ": quá hạn, không có lần chạy thành công trong ngày qua"
	case LegalSyncReasonBeatStale:
		s = name + ": đã dừng (lỡ ba lượt liên tiếp)"
	case LegalSyncReasonNoHeartbeat:
		s = name + ": chưa chạy lượt nào từ khi khởi động"
	case LegalSyncReasonTickError:
		s = name + ": các lượt chạy liên tiếp bị lỗi"
	default:
		s = name + ": " + w.Problem
	}
	if w.LastOKAt != nil {
		s += ", lần thành công cuối " + formatDateTimeVN(*w.LastOKAt)
	}
	if w.Note != "" {
		s += " — " + w.Note
	}
	return s
}

// Start runs Check after legalSyncMonitorFirstDelay, then every interval,
// until the returned stop is called (which waits for a running check). A
// monitor without alerts does not start.
func (m *LegalSyncMonitor) Start(interval time.Duration) (stop func()) {
	if m == nil || m.alerts == nil || interval <= 0 {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		wait := time.NewTimer(legalSyncMonitorFirstDelay)
		defer wait.Stop()
		select {
		case <-ctx.Done():
			return
		case <-wait.C:
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			m.runCheck(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func (m *LegalSyncMonitor) runCheck(ctx context.Context) {
	run, cancel := context.WithTimeout(ctx, legalSyncMonitorTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf(run, "[LegalSyncMonitor] check panicked: %v", r)
		}
	}()
	if err := m.Check(run); err != nil {
		logger.Warnf(run, "[LegalSyncMonitor] %v", err)
	}
}
