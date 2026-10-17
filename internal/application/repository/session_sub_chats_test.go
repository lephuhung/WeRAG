package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// Sub-chats ("chat nhỏ") are found through their document chat, resolve to
// it for documents, stay out of the session list, and move it up the list
// when they are used.
func TestSessionRepositorySubChats(t *testing.T) {
	repo, db := newSessionRepositoryForTest(t)
	require.NoError(t, db.AutoMigrate(&testIMChannelSession{}))
	ctx := context.Background()

	root := createSessionForTest(t, db, 1, "alice")
	other := createSessionForTest(t, db, 1, "alice")
	sub := &types.Session{TenantID: 1, UserID: "alice", Title: "root (tiếp)", ChatRootID: root.ID}
	require.NoError(t, db.Create(sub).Error)
	foreign := &types.Session{TenantID: 1, UserID: "bob", Title: "bob's", ChatRootID: root.ID}
	require.NoError(t, db.Create(foreign).Error)

	got, err := repo.ChatRootID(ctx, 1, sub.ID)
	require.NoError(t, err)
	require.Equal(t, root.ID, got)
	got, err = repo.ChatRootID(ctx, 1, root.ID)
	require.NoError(t, err)
	require.Equal(t, root.ID, got, "a root resolves to itself")
	got, err = repo.ChatRootID(ctx, 2, sub.ID)
	require.NoError(t, err)
	require.Equal(t, sub.ID, got, "another tenant's row is not followed")

	family, err := repo.ListChatFamily(ctx, 1, "alice", root.ID)
	require.NoError(t, err)
	require.Len(t, family, 2, "only the owner's chats")
	require.Equal(t, root.ID, family[0].ID, "the root first")
	require.Equal(t, sub.ID, family[1].ID)

	items, _, err := repo.QueryPaged(ctx, &types.SessionListQuery{TenantID: 1, UserID: "alice", Page: 1, PageSize: 50})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{root.ID, other.ID}, listItemIDsForTest(items), "sub-chats are not listed")

	old := time.Now().Add(-time.Hour)
	require.NoError(t, db.Model(&types.Session{}).Where("id = ?", root.ID).Update("updated_at", old).Error)
	_, err = repo.UpdateLastRequestState(ctx, 1, "alice", sub.ID, nil)
	require.NoError(t, err)
	var after types.Session
	require.NoError(t, db.First(&after, "id = ?", root.ID).Error)
	require.True(t, after.UpdatedAt.After(old.Add(time.Minute)), "using a sub-chat moves its document chat up")
}
