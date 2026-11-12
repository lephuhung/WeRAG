package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/redis/go-redis/v9"

	"github.com/Tencent/WeKnora/internal/common/redislock"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// BackupMonitor watches the backup jobs of deploy/backup (container
// WeKnora-backup, the Neo4j host timer): every job writes
// status/<prefix>-<job>.json to the backup bucket after each run, and the
// monitor reads them every BackupMonitorInterval with the same BACKUP_S3_*
// account. A job whose last run failed, whose last run is older than its
// limit, or that left no readable status raises one SuperAdmin digest
// (types.UserNotificationBackupAlert); the digest is closed for everyone
// once every job is healthy again. Off (nil) when BACKUP_S3_* is unset.
//
// A digest is written only when the set of failing jobs changes (or a day
// after a SuperAdmin read it while the incident goes on), so the check
// every ten minutes does not toast the same incident again. A read error
// (backup storage unreachable) counts only once it lasted
// backupReadErrorGrace; until then the job keeps its previous state.
// With Redis, one instance at a time runs the check.

const (
	// BackupMonitorInterval is how often the status files are read.
	BackupMonitorInterval = 10 * time.Minute
	// BackupAlertLink is the SuperAdmin page listing the jobs.
	BackupAlertLink = "/platform/admin?section=backups"

	backupMonitorFirstDelay = time.Minute
	backupMonitorTimeout    = 2 * time.Minute
	backupReadErrorGrace    = 30 * time.Minute
	backupRemindAfter       = 24 * time.Hour
	backupStatusMaxBytes    = 64 << 10
	backupMonitorLockKey    = "werag:backup-monitor:lock"
	backupMonitorLockLease  = 5 * time.Minute
	backupAlertNoteRunes    = 120
	backupAlertBodyRunes    = 600
)

// BackupJobSpec is one watched job and how old its last run may be.
type BackupJobSpec struct {
	Job    string
	MaxAge time.Duration
}

// DefaultBackupJobs are the jobs of deploy/backup with their limits: the
// WAL job writes its status at least every 10 minutes, the daily jobs run
// at night (a day + 2 h), the weekly ones on Sunday (a week + 1 day).
var DefaultBackupJobs = []BackupJobSpec{
	{Job: "wal", MaxAge: 15 * time.Minute},
	{Job: "postgres", MaxAge: 26 * time.Hour},
	{Job: "pgbase", MaxAge: 8 * 24 * time.Hour},
	{Job: "files", MaxAge: 26 * time.Hour},
	{Job: "objects", MaxAge: 26 * time.Hour},
	{Job: "neo4j", MaxAge: 26 * time.Hour},
	{Job: "env", MaxAge: 8 * 24 * time.Hour},
	{Job: "restore-check", MaxAge: 8 * 24 * time.Hour},
}

// BackupJobStatus is a status file written by deploy/backup/backup.sh.
type BackupJobStatus struct {
	Job        string    `json:"job"`
	OK         bool      `json:"ok"`
	FinishedAt time.Time `json:"finished_at"`
	DurationS  int64     `json:"duration_s"`
	Object     string    `json:"object"`
	Bytes      int64     `json:"bytes"`
	Note       string    `json:"note"`
	Error      string    `json:"error"`
	Host       string    `json:"host"`
	// objects job only
	Objects     *int64 `json:"objects,omitempty"`
	SourceBytes *int64 `json:"source_bytes,omitempty"`
}

// ErrBackupStatusMissing: the job has no status file.
var ErrBackupStatusMissing = errors.New("backup status file not found")

// BackupStatusReader reads the raw status file of a job.
type BackupStatusReader interface {
	ReadStatus(ctx context.Context, job string) ([]byte, error)
}

// States of a BackupJobReport: ok, a problem (types.BackupProblem*), or
// unknown (a read error still within its grace).
const (
	BackupStateOK      = "ok"
	BackupStateUnknown = "unknown"
)

// BackupJobReport is the state of one job at one check.
type BackupJobReport struct {
	Job       string           `json:"job"`
	State     string           `json:"state"`
	MaxAgeS   int64            `json:"max_age_s"`
	Status    *BackupJobStatus `json:"status,omitempty"`
	LastOKAt  *time.Time       `json:"last_ok_at,omitempty"`
	ReadError string           `json:"read_error,omitempty"`
}

// Problem reports whether the state needs attention.
func (r BackupJobReport) Problem() bool {
	return r.State != BackupStateOK && r.State != BackupStateUnknown
}

// BackupReport is the state of every watched job.
type BackupReport struct {
	Enabled   bool              `json:"enabled"`
	CheckedAt time.Time         `json:"checked_at"`
	Problems  int               `json:"problems"`
	Jobs      []BackupJobReport `json:"jobs"`
}

// BackupMonitor reads the status files and keeps the backup_alert digest.
type BackupMonitor struct {
	reader  BackupStatusReader
	jobs    []BackupJobSpec
	digests *reviewDigests
	redis   *redis.Client
	now     func() time.Time

	mu sync.Mutex
	// firstErr: when the current run of read errors of a job began
	firstErr map[string]time.Time
	// last: the previous report of each job (kept through a read error)
	last map[string]BackupJobReport
	// lastOK: the newest successful run seen of each job
	lastOK map[string]time.Time
	// resolvedAt: the last check that found every job healthy; a digest
	// older than it belongs to an earlier incident
	resolvedAt time.Time
	// checking: one check at a time in this process
	checking sync.Mutex
}

// NewBackupMonitor creates a monitor of jobs read with reader.
func NewBackupMonitor(reader BackupStatusReader, jobs []BackupJobSpec, users interfaces.UserRepository,
	notifications interfaces.UserNotificationRepository, rdb *redis.Client,
) *BackupMonitor {
	if len(jobs) == 0 {
		jobs = DefaultBackupJobs
	}
	return &BackupMonitor{
		reader: reader, jobs: jobs, digests: newReviewDigests("BackupMonitor", users, notifications), redis: rdb,
		now: time.Now, firstErr: map[string]time.Time{}, last: map[string]BackupJobReport{}, lastOK: map[string]time.Time{},
	}
}

// NewBackupMonitorFromEnv creates the monitor from BACKUP_S3_ENDPOINT,
// BACKUP_S3_ACCESS_KEY, BACKUP_S3_SECRET_KEY, BACKUP_S3_BUCKET
// (inventory-backups), BACKUP_PREFIX (werag) and BACKUP_MONITOR_JOBS (a
// comma-separated subset of DefaultBackupJobs); nil when unset. Nothing is
// contacted here: an unreachable storage only shows in the checks.
func NewBackupMonitorFromEnv(users interfaces.UserRepository, notifications interfaces.UserNotificationRepository,
	rdb *redis.Client,
) *BackupMonitor {
	endpoint := strings.TrimSpace(os.Getenv("BACKUP_S3_ENDPOINT"))
	access := strings.TrimSpace(os.Getenv("BACKUP_S3_ACCESS_KEY"))
	secret := strings.TrimSpace(os.Getenv("BACKUP_S3_SECRET_KEY"))
	if endpoint == "" || access == "" || secret == "" {
		return nil
	}
	reader, err := NewMinioBackupStatusReader(endpoint, access, secret,
		envOr("BACKUP_S3_BUCKET", "inventory-backups"), envOr("BACKUP_PREFIX", "werag"))
	if err != nil {
		logger.Warnf(context.Background(), "[BackupMonitor] off: %v", err)
		return nil
	}
	return NewBackupMonitor(reader, BackupJobsFromList(os.Getenv("BACKUP_MONITOR_JOBS")), users, notifications, rdb)
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// BackupJobsFromList keeps the DefaultBackupJobs named in list (comma
// separated, unknown names ignored); all of them when list is empty.
func BackupJobsFromList(list string) []BackupJobSpec {
	if strings.TrimSpace(list) == "" {
		return DefaultBackupJobs
	}
	want := map[string]bool{}
	for _, j := range strings.Split(list, ",") {
		want[strings.TrimSpace(j)] = true
	}
	var out []BackupJobSpec
	for _, s := range DefaultBackupJobs {
		if want[s.Job] {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return DefaultBackupJobs
	}
	return out
}

// minioBackupStatusReader reads status/<prefix>-<job>.json from bucket.
type minioBackupStatusReader struct {
	client *minio.Client
	bucket string
	prefix string
}

// NewMinioBackupStatusReader creates a reader; endpoint is a URL
// ("http://host:9000", as mc takes it) or host:port (plain HTTP).
func NewMinioBackupStatusReader(endpoint, access, secret, bucket, prefix string) (BackupStatusReader, error) {
	host, secure := endpoint, false
	if strings.Contains(endpoint, "://") {
		u, err := url.Parse(endpoint)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("BACKUP_S3_ENDPOINT %q is not a URL", endpoint)
		}
		host, secure = u.Host, u.Scheme == "https"
	}
	client, err := minio.New(host, &minio.Options{Creds: credentials.NewStaticV4(access, secret, ""), Secure: secure})
	if err != nil {
		return nil, fmt.Errorf("backup storage client: %w", err)
	}
	return &minioBackupStatusReader{client: client, bucket: bucket, prefix: prefix}, nil
}

func (r *minioBackupStatusReader) ReadStatus(ctx context.Context, job string) ([]byte, error) {
	obj, err := r.client.GetObject(ctx, r.bucket, "status/"+r.prefix+"-"+job+".json", minio.GetObjectOptions{})
	if err == nil {
		defer obj.Close()
		var data []byte
		data, err = io.ReadAll(io.LimitReader(obj, backupStatusMaxBytes))
		if err == nil {
			return data, nil
		}
	}
	if resp := minio.ToErrorResponse(err); resp.Code == "NoSuchKey" || resp.Code == "NoSuchBucket" {
		return nil, ErrBackupStatusMissing
	}
	return nil, err
}

// parseBackupStatus decodes a status file; a file without finish time is
// unreadable.
func parseBackupStatus(raw []byte) (*BackupJobStatus, error) {
	var st BackupJobStatus
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("status file is not JSON: %w", err)
	}
	if st.FinishedAt.IsZero() {
		return nil, errors.New("status file has no finished_at")
	}
	return &st, nil
}

// evaluateBackupJob is the state of spec from its read (raw or readErr) at
// now. A transient read error (not a missing or malformed file) since
// firstErr (zero: none before) is unknown until backupReadErrorGrace.
func evaluateBackupJob(spec BackupJobSpec, raw []byte, readErr error, now, firstErr time.Time) BackupJobReport {
	r := BackupJobReport{Job: spec.Job, MaxAgeS: int64(spec.MaxAge / time.Second)}
	switch {
	case errors.Is(readErr, ErrBackupStatusMissing):
		r.State = types.BackupProblemMissing
		return r
	case readErr != nil:
		r.ReadError = readErr.Error()
		if firstErr.IsZero() || now.Sub(firstErr) < backupReadErrorGrace {
			r.State = BackupStateUnknown
		} else {
			r.State = types.BackupProblemUnreadable
		}
		return r
	}
	st, err := parseBackupStatus(raw)
	if err != nil {
		r.State, r.ReadError = types.BackupProblemUnreadable, err.Error()
		return r
	}
	r.Status = st
	switch {
	case !st.OK:
		r.State = types.BackupProblemFailed
	case now.Sub(st.FinishedAt) > spec.MaxAge:
		r.State = types.BackupProblemStale
	default:
		r.State = BackupStateOK
	}
	if st.OK {
		at := st.FinishedAt
		r.LastOKAt = &at
	}
	return r
}

// transientReadError: a read error that may go away on its own.
func transientReadError(err error) bool {
	return err != nil && !errors.Is(err, ErrBackupStatusMissing)
}

// read evaluates every job now; commit records the read errors and states
// for the next checks (the periodic check), a page view does not.
func (m *BackupMonitor) read(ctx context.Context, commit bool) BackupReport {
	now := m.now()
	rep := BackupReport{Enabled: true, CheckedAt: now}
	for _, spec := range m.jobs {
		raw, err := m.reader.ReadStatus(ctx, spec.Job)
		m.mu.Lock()
		first := m.firstErr[spec.Job]
		if transientReadError(err) {
			if first.IsZero() {
				first = now
				if commit {
					m.firstErr[spec.Job] = now
				}
			}
		} else if commit {
			delete(m.firstErr, spec.Job)
		}
		r := evaluateBackupJob(spec, raw, err, now, first)
		if r.State == BackupStateUnknown {
			// within the grace the job keeps what the last check found
			if prev, ok := m.last[spec.Job]; ok {
				prev.ReadError = r.ReadError
				r = prev
			}
		} else if commit {
			m.last[spec.Job] = r
		}
		if r.LastOKAt != nil && commit && r.LastOKAt.After(m.lastOK[spec.Job]) {
			m.lastOK[spec.Job] = *r.LastOKAt
		}
		if r.LastOKAt == nil {
			if at, ok := m.lastOK[spec.Job]; ok {
				r.LastOKAt = &at
			}
		}
		m.mu.Unlock()
		if r.Problem() {
			rep.Problems++
		}
		rep.Jobs = append(rep.Jobs, r)
	}
	return rep
}

// Report reads the status of every job now (the SuperAdmin page); nil
// monitor = backups not configured.
func (m *BackupMonitor) Report(ctx context.Context) BackupReport {
	if m == nil {
		return BackupReport{Enabled: false, CheckedAt: time.Now(), Jobs: []BackupJobReport{}}
	}
	return m.read(ctx, false)
}

// Check reads every job and raises, updates or closes the digest.
func (m *BackupMonitor) Check(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.checking.Lock()
	defer m.checking.Unlock()
	release, ok := m.lock(ctx)
	if !ok {
		return nil // another instance is checking
	}
	defer release()
	rep := m.read(ctx, true)
	alert := backupAlertPayload(rep)
	if alert.Count == 0 {
		m.mu.Lock()
		m.resolvedAt = m.digests.now()
		m.mu.Unlock()
		return m.digests.closeIfEmpty(ctx, types.UserNotificationBackupAlert,
			func(context.Context) (int64, error) { return 0, nil })
	}
	return m.raise(ctx, alert)
}

// lock takes the cross-instance lock of a check (Redis); without Redis, or
// when Redis fails, the check runs anyway (it is idempotent).
func (m *BackupMonitor) lock(ctx context.Context) (release func(), ok bool) {
	if m.redis == nil {
		return func() {}, true
	}
	token, err := redislock.NewToken()
	if err != nil {
		return func() {}, true
	}
	got, err := redislock.TryAcquire(ctx, m.redis, backupMonitorLockKey, token, backupMonitorLockLease)
	if err != nil {
		logger.Warnf(ctx, "[BackupMonitor] lock: %v (checking anyway)", err)
		return func() {}, true
	}
	if !got {
		return nil, false
	}
	return func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = redislock.Release(rctx, m.redis, backupMonitorLockKey, token)
	}, true
}

// raise writes the digest of every SuperAdmin whose latest backup_alert is
// not this incident already (open with the same signature, or read less
// than a day ago and newer than the last healthy check).
func (m *BackupMonitor) raise(ctx context.Context, alert types.UserNotificationBackupAlertPayload) error {
	recipients, err := m.digests.reviewers(ctx, "")
	if err != nil {
		return fmt.Errorf("listing SuperAdmins: %w", err)
	}
	m.mu.Lock()
	resolvedAt := m.resolvedAt
	m.mu.Unlock()
	now := m.digests.now()
	var failed []string
	for _, userID := range recipients {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		prev, err := m.digests.notifications.LatestOfKind(ctx, userID, types.UserNotificationBackupAlert)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", userID, err))
			continue
		}
		if backupAlertCurrent(prev, alert.Signature, resolvedAt, now) {
			continue
		}
		n := backupAlertNotification(userID, alert, now)
		if _, err := m.digests.notifications.UpsertOpenDigest(ctx, n, func(_, next *types.UserNotification) {
			*next = *backupAlertNotification(next.UserID, alert, next.CreatedAt)
		}); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", userID, err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("backup alert of %d SuperAdmin(s) not written: %s", len(failed), strings.Join(failed, "; "))
	}
	return nil
}

// backupAlertCurrent reports whether prev (the person's latest backup
// alert) already tells the incident of signature.
func backupAlertCurrent(prev *types.UserNotification, signature string, resolvedAt, now time.Time) bool {
	if prev == nil {
		return false
	}
	var p types.UserNotificationBackupAlertPayload
	if len(prev.Payload) == 0 || json.Unmarshal(prev.Payload, &p) != nil || p.Signature != signature {
		return false
	}
	if prev.ReadAt == nil {
		return true
	}
	return now.Sub(*prev.ReadAt) < backupRemindAfter && prev.CreatedAt.After(resolvedAt)
}

// backupAlertPayload lists the jobs of rep with a problem.
func backupAlertPayload(rep BackupReport) types.UserNotificationBackupAlertPayload {
	p := types.UserNotificationBackupAlertPayload{Jobs: []types.UserNotificationBackupJob{}}
	var sig []string
	for _, r := range rep.Jobs {
		if !r.Problem() {
			continue
		}
		j := types.UserNotificationBackupJob{Job: r.Job, Problem: r.State, LastOKAt: r.LastOKAt}
		if r.Status != nil {
			at := r.Status.FinishedAt
			j.FinishedAt = &at
			j.Note = strings.TrimSpace(r.Status.Error)
			if j.Note == "" && r.State == types.BackupProblemFailed {
				j.Note = strings.TrimSpace(r.Status.Note)
			}
		} else {
			j.Note = r.ReadError
		}
		j.Note = queueEllipsis(j.Note, backupAlertNoteRunes)
		p.Jobs = append(p.Jobs, j)
		sig = append(sig, r.Job+":"+r.State)
	}
	sort.Strings(sig)
	p.Count, p.Signature = len(p.Jobs), strings.Join(sig, ",")
	return p
}

// backupAlertNotification builds the digest of userID. Title and body are
// the stored (Vietnamese) text; the clients word it from the payload in
// the reader's language.
func backupAlertNotification(userID string, p types.UserNotificationBackupAlertPayload, at time.Time) *types.UserNotification {
	payload, _ := json.Marshal(p)
	parts := make([]string, 0, len(p.Jobs))
	for _, j := range p.Jobs {
		parts = append(parts, backupAlertJobText(j))
	}
	return &types.UserNotification{
		TenantID:  types.PlatformTenantID,
		UserID:    userID,
		Kind:      types.UserNotificationBackupAlert,
		Title:     fmt.Sprintf("Sao lưu gặp sự cố: %d việc", p.Count),
		Body:      queueEllipsis(strings.Join(parts, "; "), backupAlertBodyRunes),
		Link:      BackupAlertLink,
		Payload:   types.JSON(payload),
		DedupeKey: types.UserNotificationOpenKey(types.UserNotificationBackupAlert, userID),
		CreatedAt: at,
	}
}

// backupAlertJobText: "files: quá hạn, lần thành công cuối 02h30 - 11/11/2026".
func backupAlertJobText(j types.UserNotificationBackupJob) string {
	var s string
	switch j.Problem {
	case types.BackupProblemFailed:
		s = j.Job + ": lỗi"
		if j.FinishedAt != nil {
			s += " lúc " + formatDateTimeVN(*j.FinishedAt)
		}
	case types.BackupProblemStale:
		s = j.Job + ": quá hạn"
	case types.BackupProblemMissing:
		s = j.Job + ": chưa có trạng thái"
	default:
		s = j.Job + ": không đọc được trạng thái"
	}
	if j.LastOKAt != nil {
		s += ", lần thành công cuối " + formatDateTimeVN(*j.LastOKAt)
	}
	if j.Note != "" {
		s += " — " + j.Note
	}
	return s
}

// Start runs Check after a minute, then every interval, until the
// returned stop is called (which waits for a running check).
func (m *BackupMonitor) Start(interval time.Duration) (stop func()) {
	if m == nil || interval <= 0 {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		wait := time.NewTimer(backupMonitorFirstDelay)
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

func (m *BackupMonitor) runCheck(ctx context.Context) {
	run, cancel := context.WithTimeout(ctx, backupMonitorTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf(run, "[BackupMonitor] check panicked: %v", r)
		}
	}()
	if err := m.Check(run); err != nil {
		logger.Warnf(run, "[BackupMonitor] %v", err)
	}
}
