package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

// fakeBackupStatuses serves status files from memory: a job absent from
// both maps has no status file.
type fakeBackupStatuses struct {
	mu   sync.Mutex
	raw  map[string][]byte
	errs map[string]error
}

func (f *fakeBackupStatuses) ReadStatus(_ context.Context, job string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errs[job]; err != nil {
		return nil, err
	}
	if raw, ok := f.raw[job]; ok {
		return raw, nil
	}
	return nil, ErrBackupStatusMissing
}

func (f *fakeBackupStatuses) set(job string, ok bool, finished time.Time, errText string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, _ := json.Marshal(map[string]any{"job": job, "ok": ok, "finished_at": finished.UTC().Format(time.RFC3339),
		"object": "werag/" + job + "/x", "bytes": 42, "note": "n", "error": errText})
	f.raw[job] = raw
	delete(f.errs, job)
}

func (f *fakeBackupStatuses) fail(job string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[job] = err
}

func spec(job string) BackupJobSpec {
	for _, s := range DefaultBackupJobs {
		if s.Job == job {
			return s
		}
	}
	panic(job)
}

func statusJSON(ok bool, finished time.Time) []byte {
	raw, _ := json.Marshal(map[string]any{"ok": ok, "finished_at": finished.UTC().Format(time.RFC3339)})
	return raw
}

func TestEvaluateBackupJobThresholds(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		job  string
		age  time.Duration
		ok   bool
		want string
	}{
		{"wal", 14 * time.Minute, true, BackupStateOK},
		{"wal", 16 * time.Minute, true, types.BackupProblemStale},
		{"postgres", 25 * time.Hour, true, BackupStateOK},
		{"postgres", 27 * time.Hour, true, types.BackupProblemStale},
		{"files", 27 * time.Hour, true, types.BackupProblemStale},
		{"objects", 25 * time.Hour, true, BackupStateOK},
		{"objects", 27 * time.Hour, true, types.BackupProblemStale},
		{"neo4j", 27 * time.Hour, true, types.BackupProblemStale},
		{"env", 7 * 24 * time.Hour, true, BackupStateOK},
		{"env", 9 * 24 * time.Hour, true, types.BackupProblemStale},
		{"pgbase", 7*24*time.Hour + 23*time.Hour, true, BackupStateOK},
		{"pgbase", 8*24*time.Hour + time.Hour, true, types.BackupProblemStale},
		{"restore-check", 9 * 24 * time.Hour, true, types.BackupProblemStale},
		{"wal", time.Minute, false, types.BackupProblemFailed},
		// a failed run that is also old is reported as failed
		{"postgres", 30 * time.Hour, false, types.BackupProblemFailed},
	}
	for _, c := range cases {
		r := evaluateBackupJob(spec(c.job), statusJSON(c.ok, now.Add(-c.age)), nil, now, time.Time{})
		require.Equal(t, c.want, r.State, "%s age %s ok=%v", c.job, c.age, c.ok)
		require.Equal(t, c.want != BackupStateOK, r.Problem())
		require.Equal(t, c.ok, r.LastOKAt != nil)
	}
}

func TestEvaluateBackupJobUnreadable(t *testing.T) {
	now := time.Now()
	r := evaluateBackupJob(spec("files"), nil, ErrBackupStatusMissing, now, time.Time{})
	require.Equal(t, types.BackupProblemMissing, r.State)

	r = evaluateBackupJob(spec("files"), []byte("not json"), nil, now, time.Time{})
	require.Equal(t, types.BackupProblemUnreadable, r.State)
	require.Contains(t, r.ReadError, "not JSON")

	r = evaluateBackupJob(spec("files"), []byte(`{"ok":true}`), nil, now, time.Time{})
	require.Equal(t, types.BackupProblemUnreadable, r.State, "no finished_at")

	netErr := errors.New("dial tcp: connection refused")
	r = evaluateBackupJob(spec("files"), nil, netErr, now, now)
	require.Equal(t, BackupStateUnknown, r.State, "a fresh read error waits")
	require.False(t, r.Problem())
	r = evaluateBackupJob(spec("files"), nil, netErr, now, now.Add(-29*time.Minute))
	require.Equal(t, BackupStateUnknown, r.State)
	r = evaluateBackupJob(spec("files"), nil, netErr, now, now.Add(-31*time.Minute))
	require.Equal(t, types.BackupProblemUnreadable, r.State)
	require.Equal(t, netErr.Error(), r.ReadError)
}

func TestBackupJobsFromList(t *testing.T) {
	require.Equal(t, DefaultBackupJobs, BackupJobsFromList(""))
	require.Equal(t, DefaultBackupJobs, BackupJobsFromList("nope"))
	got := BackupJobsFromList(" objects , wal,unknown")
	require.Len(t, got, 2)
	require.Equal(t, "wal", got[0].Job, "kept in the default order")
	require.Equal(t, "objects", got[1].Job)
}

func TestNewBackupMonitorFromEnv(t *testing.T) {
	for _, k := range []string{"BACKUP_S3_ENDPOINT", "BACKUP_S3_ACCESS_KEY", "BACKUP_S3_SECRET_KEY"} {
		t.Setenv(k, "")
	}
	require.Nil(t, NewBackupMonitorFromEnv(nil, nil, nil), "unset → off")
	var off *BackupMonitor
	require.NoError(t, off.Check(context.Background()))
	rep := off.Report(context.Background())
	require.False(t, rep.Enabled)
	require.NotNil(t, rep.Jobs)
	require.NotPanics(t, func() { off.Start(time.Minute)() })

	t.Setenv("BACKUP_S3_ENDPOINT", "http://127.0.0.1:1") // nothing listens: not contacted here
	t.Setenv("BACKUP_S3_ACCESS_KEY", "a")
	require.Nil(t, NewBackupMonitorFromEnv(nil, nil, nil), "secret missing → off")
	t.Setenv("BACKUP_S3_SECRET_KEY", "s")
	t.Setenv("BACKUP_MONITOR_JOBS", "wal,files")
	m := NewBackupMonitorFromEnv(nil, nil, nil)
	require.NotNil(t, m)
	require.Len(t, m.jobs, 2)

	t.Setenv("BACKUP_S3_ENDPOINT", "http://")
	require.Nil(t, NewBackupMonitorFromEnv(nil, nil, nil), "bad endpoint → off, no panic")
}

func TestMinioBackupStatusReaderUnreachable(t *testing.T) {
	r, err := NewMinioBackupStatusReader("127.0.0.1:1", "a", "s", "b", "werag")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = r.ReadStatus(ctx, "wal")
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrBackupStatusMissing), "unreachable is a read error, not a missing file")
	require.True(t, transientReadError(err))
}

type backupMonitorEnv struct {
	*queueNoticeEnv
	statuses *fakeBackupStatuses
	monitor  *BackupMonitor
	now      time.Time
}

func newBackupMonitorEnv(t *testing.T) *backupMonitorEnv {
	q := newQueueNoticeEnv(t, nil)
	e := &backupMonitorEnv{queueNoticeEnv: q, statuses: &fakeBackupStatuses{raw: map[string][]byte{}, errs: map[string]error{}},
		now: time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)}
	e.monitor = NewBackupMonitor(e.statuses, nil, repository.NewUserRepository(q.db), q.notices, nil)
	e.monitor.now = func() time.Time { return e.now }
	e.monitor.digests.now = func() time.Time { return e.now }
	e.healthy()
	return e
}

// healthy: every job finished well just now.
func (e *backupMonitorEnv) healthy() {
	for _, s := range DefaultBackupJobs {
		e.statuses.set(s.Job, true, e.now.Add(-time.Minute), "")
	}
}

func (e *backupMonitorEnv) check(t *testing.T) {
	t.Helper()
	require.NoError(t, e.monitor.Check(context.Background()))
}

func (e *backupMonitorEnv) alert(t *testing.T, user string) (*types.UserNotification, types.UserNotificationBackupAlertPayload) {
	t.Helper()
	n := e.unread(t, user, types.UserNotificationBackupAlert)
	var p types.UserNotificationBackupAlertPayload
	if n != nil {
		require.NoError(t, json.Unmarshal(n.Payload, &p))
	}
	return n, p
}

func TestBackupMonitorRaisesAndCloses(t *testing.T) {
	e := newBackupMonitorEnv(t)
	e.check(t)
	n, _ := e.alert(t, "sa1")
	require.Nil(t, n, "all healthy: nothing")
	require.EqualValues(t, 0, e.notices.upserts.Load())

	// files fails
	e.statuses.set("files", false, e.now.Add(-time.Hour), "tar: lỗi đọc")
	e.check(t)
	for _, sa := range []string{"sa1", "sa2"} {
		n, p := e.alert(t, sa)
		require.NotNil(t, n, sa)
		require.Equal(t, "Sao lưu gặp sự cố: 1 việc", n.Title)
		require.Equal(t, BackupAlertLink, n.Link)
		require.Equal(t, types.PlatformTenantID, n.TenantID)
		require.Equal(t, 1, p.Count)
		require.Equal(t, "files:failed", p.Signature)
		require.Equal(t, "files", p.Jobs[0].Job)
		require.Equal(t, "tar: lỗi đọc", p.Jobs[0].Note)
		require.NotNil(t, p.Jobs[0].FinishedAt)
		// the last success seen by the first check (a minute before it)
		require.NotNil(t, p.Jobs[0].LastOKAt)
		require.Contains(t, n.Body, "files: lỗi lúc 09h00 - 12/11/2026")
		require.Contains(t, n.Body, "lần thành công cuối 09h59 - 12/11/2026")
	}
	e.nobodyElse(t)
	require.EqualValues(t, 2, e.notices.upserts.Load())

	// the same incident ten minutes later: not written (nor toasted) again
	first, _ := e.alert(t, "sa1")
	e.now = e.now.Add(10 * time.Minute)
	e.healthyExcept("files")
	e.check(t)
	require.EqualValues(t, 2, e.notices.upserts.Load())
	again, _ := e.alert(t, "sa1")
	require.Equal(t, first.CreatedAt.Unix(), again.CreatedAt.Unix())

	// wal goes stale too: the digest is updated (one row, two jobs)
	e.statuses.set("wal", true, e.now.Add(-20*time.Minute), "")
	e.check(t)
	n, p := e.alert(t, "sa1")
	require.Equal(t, "Sao lưu gặp sự cố: 2 việc", n.Title)
	require.Equal(t, "files:failed,wal:stale", p.Signature)
	require.Equal(t, first.ID, n.ID, "same open digest")
	require.Equal(t, "wal", p.Jobs[0].Job, "job order")
	require.Equal(t, types.BackupProblemStale, p.Jobs[0].Problem)

	// everything healthy again: closed for everyone
	e.now = e.now.Add(10 * time.Minute)
	e.healthy()
	e.check(t)
	e.closed(t, types.UserNotificationBackupAlert)

	// the same failure later is a new incident: raised again
	e.now = e.now.Add(10 * time.Minute)
	e.statuses.set("files", false, e.now.Add(-time.Minute), "x")
	e.check(t)
	n, _ = e.alert(t, "sa2")
	require.NotNil(t, n)
	require.NotEqual(t, first.ID, n.ID)
}

// healthyExcept refreshes every job but skip.
func (e *backupMonitorEnv) healthyExcept(skip string) {
	for _, s := range DefaultBackupJobs {
		if s.Job != skip {
			e.statuses.set(s.Job, true, e.now.Add(-time.Minute), "")
		}
	}
}

func TestBackupMonitorReadDigestRemindsAfterADay(t *testing.T) {
	e := newBackupMonitorEnv(t)
	e.statuses.set("objects", false, e.now, "mc mirror lỗi")
	e.check(t)
	n, _ := e.alert(t, "sa1")
	require.NotNil(t, n)
	ok, err := e.notices.MarkRead(context.Background(), "sa1", n.ID, e.now)
	require.NoError(t, err)
	require.True(t, ok)

	// still failing: sa1 read it, so no new toast within a day
	e.now = e.now.Add(2 * time.Hour)
	e.healthyExcept("objects")
	e.check(t)
	n, _ = e.alert(t, "sa1")
	require.Nil(t, n)

	// a day later it is still broken: reminded
	e.now = e.now.Add(23 * time.Hour)
	e.healthyExcept("objects")
	e.check(t)
	n, _ = e.alert(t, "sa1")
	require.NotNil(t, n)
}

func TestBackupMonitorMissingAndReadErrors(t *testing.T) {
	e := newBackupMonitorEnv(t)
	e.statuses.mu.Lock()
	delete(e.statuses.raw, "neo4j")
	e.statuses.mu.Unlock()
	e.check(t)
	_, p := e.alert(t, "sa1")
	require.Equal(t, "neo4j:missing", p.Signature)

	// storage unreachable for env: unknown for 30 min, neo4j alert unchanged
	e.statuses.fail("env", errors.New("dial tcp 10.10.0.123:9000: i/o timeout"))
	upserts := e.notices.upserts.Load()
	for i := 0; i < 3; i++ {
		e.now = e.now.Add(10 * time.Minute)
		e.healthyExcept("neo4j")
		e.statuses.fail("env", errors.New("dial tcp 10.10.0.123:9000: i/o timeout"))
		delete(e.statuses.raw, "neo4j")
		e.check(t)
	}
	require.Equal(t, upserts, e.notices.upserts.Load(), "within the grace: no change")
	e.now = e.now.Add(10 * time.Minute)
	e.check(t)
	n, p := e.alert(t, "sa1")
	require.Equal(t, "env:unreadable,neo4j:missing", p.Signature)
	require.Contains(t, n.Body, "env: không đọc được trạng thái")
	require.Contains(t, n.Body, "i/o timeout")

	// the page shows the jobs without changing the monitor's state
	rep := e.monitor.Report(context.Background())
	require.True(t, rep.Enabled)
	require.Equal(t, 2, rep.Problems)
	require.Len(t, rep.Jobs, len(DefaultBackupJobs))
}

func TestBackupMonitorReadErrorKeepsPreviousState(t *testing.T) {
	e := newBackupMonitorEnv(t)
	e.statuses.set("files", false, e.now, "boom")
	e.check(t)
	_, p := e.alert(t, "sa1")
	require.Equal(t, "files:failed", p.Signature)
	// the status of files can no longer be read for a while: the alert
	// neither closes nor changes
	e.now = e.now.Add(10 * time.Minute)
	e.healthyExcept("files")
	e.statuses.fail("files", fmt.Errorf("connection reset"))
	upserts := e.notices.upserts.Load()
	e.check(t)
	n, p := e.alert(t, "sa1")
	require.NotNil(t, n)
	require.Equal(t, "files:failed", p.Signature)
	require.Equal(t, upserts, e.notices.upserts.Load())
}

func TestBackupAlertCurrent(t *testing.T) {
	now := time.Now()
	payload, _ := json.Marshal(types.UserNotificationBackupAlertPayload{Signature: "wal:stale"})
	open := &types.UserNotification{Payload: types.JSON(payload), CreatedAt: now.Add(-time.Hour)}
	require.False(t, backupAlertCurrent(nil, "wal:stale", time.Time{}, now))
	require.True(t, backupAlertCurrent(open, "wal:stale", time.Time{}, now))
	require.False(t, backupAlertCurrent(open, "wal:failed", time.Time{}, now))
	readAt := now.Add(-time.Hour)
	read := &types.UserNotification{Payload: types.JSON(payload), CreatedAt: now.Add(-2 * time.Hour), ReadAt: &readAt}
	require.True(t, backupAlertCurrent(read, "wal:stale", time.Time{}, now))
	require.False(t, backupAlertCurrent(read, "wal:stale", now.Add(-90*time.Minute), now), "closed by a healthy check")
	old := now.Add(-25 * time.Hour)
	read.ReadAt = &old
	require.False(t, backupAlertCurrent(read, "wal:stale", time.Time{}, now), "reminded after a day")
}
