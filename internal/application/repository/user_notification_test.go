package repository

import (
	"context"
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
