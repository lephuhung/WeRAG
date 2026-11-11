package repository

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// countDigest is a test digest: the payload holds how many events it sums.
type countDigest struct {
	Count int `json:"count"`
}

func digestFor(user string, at time.Time) *types.UserNotification {
	return &types.UserNotification{
		UserID: user, Kind: "digest_kind", Title: "1", Payload: types.JSON(`{"count":1}`),
		Link: "/platform/x", DedupeKey: types.UserNotificationOpenKey("digest_kind", user), CreatedAt: at,
	}
}

func addOne(prev, next *types.UserNotification) {
	var p countDigest
	_ = json.Unmarshal(prev.Payload, &p)
	p.Count++
	b, _ := json.Marshal(p)
	next.Payload = types.JSON(b)
	next.Title = string(rune('0' + p.Count))
}

func digestCount(t *testing.T, n *types.UserNotification) int {
	t.Helper()
	var p countDigest
	require.NoError(t, json.Unmarshal(n.Payload, &p))
	return p.Count
}

func TestUserNotificationOpenDigestLifecycle(t *testing.T) {
	repo := newUserNotificationRepo(t)
	ctx := context.Background()
	t0 := time.Now().Add(-time.Hour)

	first := digestFor("a", t0)
	created, err := repo.UpsertOpenDigest(ctx, first, addOne)
	require.NoError(t, err)
	require.True(t, created)
	_, err = repo.MarkSeen(ctx, "a", []string{first.ID}, t0)
	require.NoError(t, err)

	second := digestFor("a", t0.Add(time.Minute))
	created, err = repo.UpsertOpenDigest(ctx, second, addOne)
	require.NoError(t, err)
	require.False(t, created, "the unread digest is updated")
	require.Equal(t, first.ID, second.ID)

	rows, err := repo.List(ctx, "a", false, nil, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 2, digestCount(t, rows[0]))
	require.Equal(t, "2", rows[0].Title)
	require.Nil(t, rows[0].SeenAt, "updated digest is toasted again")
	require.WithinDuration(t, t0.Add(time.Minute), rows[0].CreatedAt, time.Second, "moved to the top")
	_, unseen, err := repo.CountUnread(ctx, "a")
	require.NoError(t, err)
	require.EqualValues(t, 1, unseen)

	// another user's digest is separate
	other := digestFor("b", t0)
	created, err = repo.UpsertOpenDigest(ctx, other, addOne)
	require.NoError(t, err)
	require.True(t, created)

	// reading frees the key: the next event opens a new digest
	ok, err := repo.MarkRead(ctx, "a", first.ID, time.Now())
	require.NoError(t, err)
	require.True(t, ok)
	third := digestFor("a", time.Now())
	created, err = repo.UpsertOpenDigest(ctx, third, addOne)
	require.NoError(t, err)
	require.True(t, created)
	require.NotEqual(t, first.ID, third.ID)
	rows, err = repo.List(ctx, "a", true, nil, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 1, digestCount(t, rows[0]))
	var old types.UserNotification
	require.NoError(t, repo.db.Where("id = ?", first.ID).Take(&old).Error)
	require.Equal(t, first.DedupeKey+":read:"+first.ID, old.DedupeKey)

	// mark all read frees it too; a plain notice keeps its key
	plain := &types.UserNotification{UserID: "a", Kind: "k", DedupeKey: "plain:1"}
	_, err = repo.Create(ctx, plain)
	require.NoError(t, err)
	_, err = repo.MarkAllRead(ctx, "a", time.Now())
	require.NoError(t, err)
	var kept types.UserNotification
	require.NoError(t, repo.db.Where("id = ?", plain.ID).Take(&kept).Error)
	require.Equal(t, "plain:1", kept.DedupeKey)
	fourth := digestFor("a", time.Now())
	created, err = repo.UpsertOpenDigest(ctx, fourth, addOne)
	require.NoError(t, err)
	require.True(t, created)

	// a non-open key is refused
	bad := digestFor("a", time.Now())
	bad.DedupeKey = "digest_kind:a"
	_, err = repo.UpsertOpenDigest(ctx, bad, addOne)
	require.Error(t, err)
}

func TestUserNotificationCloseOpenDigests(t *testing.T) {
	repo := newUserNotificationRepo(t)
	ctx := context.Background()
	now := time.Now()
	old := digestFor("a", now.Add(-time.Hour))
	_, err := repo.UpsertOpenDigest(ctx, old, addOne)
	require.NoError(t, err)
	fresh := digestFor("b", now.Add(time.Minute)) // updated after the queue was counted
	_, err = repo.UpsertOpenDigest(ctx, fresh, addOne)
	require.NoError(t, err)
	otherKind := &types.UserNotification{UserID: "a", Kind: "other", DedupeKey: types.UserNotificationOpenKey("other", "a"),
		CreatedAt: now.Add(-time.Hour)}
	_, err = repo.UpsertOpenDigest(ctx, otherKind, addOne)
	require.NoError(t, err)

	n, err := repo.CloseOpenDigests(ctx, "digest_kind", now, now)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	unread, _, err := repo.CountUnread(ctx, "a")
	require.NoError(t, err)
	require.EqualValues(t, 1, unread, "only the other kind stays")
	unread, _, err = repo.CountUnread(ctx, "b")
	require.NoError(t, err)
	require.EqualValues(t, 1, unread, "a digest newer than the count stays open")

	// the closed key is free again
	again := digestFor("a", now)
	created, err := repo.UpsertOpenDigest(ctx, again, addOne)
	require.NoError(t, err)
	require.True(t, created)
}

func TestUserNotificationOpenDigestConcurrent(t *testing.T) {
	repo := newUserNotificationRepo(t)
	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = repo.UpsertOpenDigest(context.Background(), digestFor("a", time.Now()), addOne)
		}(i)
	}
	close(start)
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	rows, err := repo.List(context.Background(), "a", true, nil, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1, "one unread digest")
	require.Equal(t, workers, digestCount(t, rows[0]))
}

// A read row still holding an open key (written before reads rotated keys)
// must not block the digest for good: the key is freed and a new one opens.
func TestUserNotificationOpenDigestFreesKeyOfReadRow(t *testing.T) {
	repo := newUserNotificationRepo(t)
	ctx := context.Background()
	readAt := time.Now().Add(-time.Hour)
	stale := digestFor("a", time.Now().Add(-2*time.Hour))
	stale.ID = "stale"
	stale.ReadAt = &readAt
	require.NoError(t, repo.db.Create(stale).Error)

	n := digestFor("a", time.Now())
	created, err := repo.UpsertOpenDigest(ctx, n, addOne)
	require.NoError(t, err)
	require.True(t, created)
	require.NotEqual(t, "stale", n.ID)
	var old types.UserNotification
	require.NoError(t, repo.db.Where("id = ?", "stale").Take(&old).Error)
	require.Equal(t, stale.DedupeKey+":read:stale", old.DedupeKey)
	rows, err := repo.List(ctx, "a", true, nil, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 1, digestCount(t, rows[0]))
}
