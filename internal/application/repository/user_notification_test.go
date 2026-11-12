package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
)

func newUserNotificationRepo(t *testing.T) *userNotificationRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.UserNotification{}))
	return &userNotificationRepository{db: db}
}

func TestUserNotificationsOwnRowsOnly(t *testing.T) {
	repo := newUserNotificationRepo(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour)
	add := func(user, key string, at time.Time) *types.UserNotification {
		t.Helper()
		n := &types.UserNotification{UserID: user, Kind: "k", Title: key, DedupeKey: key, CreatedAt: at}
		ok, err := repo.Create(ctx, n)
		require.NoError(t, err)
		require.True(t, ok)
		return n
	}
	a1 := add("a", "a1", base)
	a2 := add("a", "a2", base.Add(time.Minute))
	b1 := add("b", "b1", base.Add(2*time.Minute))
	ok, err := repo.Create(ctx, &types.UserNotification{UserID: "a", Kind: "k", DedupeKey: "a1"})
	require.NoError(t, err)
	require.False(t, ok, "the dedupe key keeps one row")

	rows, err := repo.List(ctx, "a", false, nil, 10)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, a2.ID, rows[0].ID, "newest first")
	before := a2.CreatedAt
	rows, err = repo.List(ctx, "a", false, &before, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, a1.ID, rows[0].ID)

	unread, unseen, err := repo.CountUnread(ctx, "a")
	require.NoError(t, err)
	require.EqualValues(t, 2, unread)
	require.EqualValues(t, 2, unseen)

	// b's id in a's call changes nothing
	n, err := repo.MarkSeen(ctx, "a", []string{a1.ID, b1.ID}, time.Now())
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	found, err := repo.MarkRead(ctx, "a", b1.ID, time.Now())
	require.NoError(t, err)
	require.False(t, found)
	_, bUnseen, _ := repo.CountUnread(ctx, "b")
	require.EqualValues(t, 1, bUnseen)

	unread, unseen, _ = repo.CountUnread(ctx, "a")
	require.EqualValues(t, 2, unread)
	require.EqualValues(t, 1, unseen)

	found, err = repo.MarkRead(ctx, "a", a2.ID, time.Now())
	require.NoError(t, err)
	require.True(t, found)
	rows, _ = repo.List(ctx, "a", true, nil, 10)
	require.Len(t, rows, 1)
	require.Equal(t, a1.ID, rows[0].ID)
	var read types.UserNotification
	require.NoError(t, repo.db.Where("id = ?", a2.ID).Take(&read).Error)
	require.NotNil(t, read.SeenAt, "reading also marks seen")

	n, err = repo.MarkAllRead(ctx, "a", time.Now())
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	unread, _, _ = repo.CountUnread(ctx, "a")
	require.Zero(t, unread)
	bUnread, _, _ := repo.CountUnread(ctx, "b")
	require.EqualValues(t, 1, bUnread)
}

func TestUserNotificationLinkValidated(t *testing.T) {
	repo := newUserNotificationRepo(t)
	ctx := context.Background()
	for _, link := range []string{"https://evil.example", "//evil.example", "/platform//evil.example", "/platform/\\evil",
		"/platform/\tx", "/platform/a\nb", "/platform/a\x7fb", "/other/page", "platform/chat/x"} {
		_, err := repo.Create(ctx, &types.UserNotification{UserID: "a", Kind: "k", DedupeKey: link, Link: link})
		require.Error(t, err, link)
	}
	for _, link := range []string{"", "/platform/chat/s1", "/platform/legal-lookup?q=53%2F2022%2FN%C4%90-CP"} {
		_, err := repo.Create(ctx, &types.UserNotification{UserID: "a", Kind: "k", DedupeKey: "ok" + link, Link: link})
		require.NoError(t, err, link)
	}
}

func TestUserNotificationPurgeRead(t *testing.T) {
	repo := newUserNotificationRepo(t)
	ctx := context.Background()
	old := time.Now().Add(-types.UserNotificationRetention - time.Hour)
	recent := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		require.NoError(t, repo.db.Create(&types.UserNotification{ID: fmt.Sprintf("old%d", i), UserID: "a", Kind: "k",
			DedupeKey: fmt.Sprintf("old%d", i), ReadAt: &old}).Error)
	}
	require.NoError(t, repo.db.Create(&types.UserNotification{ID: "recent", UserID: "a", Kind: "k", DedupeKey: "recent", ReadAt: &recent}).Error)
	require.NoError(t, repo.db.Create(&types.UserNotification{ID: "unread", UserID: "a", Kind: "k", DedupeKey: "unread",
		CreatedAt: old}).Error)
	n, err := repo.PurgeRead(ctx, time.Now().Add(-types.UserNotificationRetention), 2)
	require.NoError(t, err)
	require.EqualValues(t, 5, n, "deleted in batches of 2")
	var left []string
	require.NoError(t, repo.db.Model(&types.UserNotification{}).Order("id").Pluck("id", &left).Error)
	require.Equal(t, []string{"recent", "unread"}, left, "unread ones are never purged")
}

func TestUserNotificationLatestOfKind(t *testing.T) {
	repo := newUserNotificationRepo(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour)
	n, err := repo.LatestOfKind(ctx, "a", "backup_alert")
	require.NoError(t, err)
	require.Nil(t, n, "none yet")
	for i, row := range []struct{ user, kind string }{{"a", "backup_alert"}, {"a", "backup_alert"}, {"a", "other"}, {"b", "backup_alert"}} {
		_, err := repo.Create(ctx, &types.UserNotification{UserID: row.user, Kind: row.kind, Title: fmt.Sprint(i),
			DedupeKey: fmt.Sprint("k", i), CreatedAt: base.Add(time.Duration(i) * time.Minute)})
		require.NoError(t, err)
	}
	n, err = repo.LatestOfKind(ctx, "a", "backup_alert")
	require.NoError(t, err)
	require.Equal(t, "1", n.Title, "newest of the kind, the user's own")
	_, err = repo.MarkRead(ctx, "a", n.ID, time.Now())
	require.NoError(t, err)
	n, err = repo.LatestOfKind(ctx, "a", "backup_alert")
	require.NoError(t, err)
	require.Equal(t, "1", n.Title, "read rows count too")
	require.NotNil(t, n.ReadAt)
}

func TestUserNotificationSetPayload(t *testing.T) {
	repo := newUserNotificationRepo(t)
	ctx := context.Background()
	n := &types.UserNotification{UserID: "a", Kind: "backup_alert", DedupeKey: "k", Payload: types.JSON(`{"x":1}`)}
	_, err := repo.Create(ctx, n)
	require.NoError(t, err)
	require.NoError(t, repo.SetPayload(ctx, "b", n.ID, types.JSON(`{"x":2}`)), "another user's id: no-op")
	got, err := repo.LatestOfKind(ctx, "a", "backup_alert")
	require.NoError(t, err)
	require.JSONEq(t, `{"x":1}`, string(got.Payload))
	require.NoError(t, repo.SetPayload(ctx, "a", n.ID, types.JSON(`{"x":2}`)))
	got, err = repo.LatestOfKind(ctx, "a", "backup_alert")
	require.NoError(t, err)
	require.JSONEq(t, `{"x":2}`, string(got.Payload))
	require.Nil(t, got.SeenAt, "not marked seen")
}
