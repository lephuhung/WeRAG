package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newKBInviteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.KBInvitation{}))
	return db
}

func seedKBInvite(t *testing.T, db *gorm.DB, id, kbID, userID string, status types.KBInvitationStatus, expiresAt *time.Time) {
	t.Helper()
	require.NoError(t, db.Create(&types.KBInvitation{
		ID: id, KBID: kbID, OwnerTenantID: 100,
		RecipientUserID: userID, RecipientTenantID: 200,
		InviterUserID: "admin-1", TokenHash: types.HashKBInviteToken("token-" + id),
		Status: status, ExpiresAt: expiresAt,
	}).Error)
}

func TestKBInvitationRepoMarkRevoked(t *testing.T) {
	ctx := context.Background()
	db := newKBInviteTestDB(t)
	repo := NewKBInvitationRepository(db)

	seedKBInvite(t, db, "pending-1", "kb-1", "user-b", types.KBInvitationStatusPending, nil)
	seedKBInvite(t, db, "accepted-1", "kb-1", "user-c", types.KBInvitationStatusAccepted, nil)
	seedKBInvite(t, db, "revoked-1", "kb-1", "user-d", types.KBInvitationStatusRevoked, nil)

	// Pending revoke works.
	require.NoError(t, repo.MarkRevoked(ctx, "pending-1"))
	// Accepted revoke works (access stops immediately).
	require.NoError(t, repo.MarkRevoked(ctx, "accepted-1"))
	ok, err := repo.HasAcceptedInvite(ctx, "kb-1", "user-c")
	require.NoError(t, err)
	require.False(t, ok, "revoked accepted invite must stop authorizing reads")
	// Already-terminal revoke fails.
	require.ErrorIs(t, repo.MarkRevoked(ctx, "revoked-1"), gorm.ErrRecordNotFound)
	require.ErrorIs(t, repo.MarkRevoked(ctx, "pending-1"), gorm.ErrRecordNotFound)
	require.ErrorIs(t, repo.MarkRevoked(ctx, "missing"), gorm.ErrRecordNotFound)
}

func TestKBInvitationRepoListAcceptedKBIDsByUser(t *testing.T) {
	ctx := context.Background()
	db := newKBInviteTestDB(t)
	repo := NewKBInvitationRepository(db)

	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)
	seedKBInvite(t, db, "live-1", "kb-1", "user-b", types.KBInvitationStatusAccepted, nil)
	seedKBInvite(t, db, "live-2", "kb-2", "user-b", types.KBInvitationStatusAccepted, &future)
	seedKBInvite(t, db, "exp-1", "kb-3", "user-b", types.KBInvitationStatusAccepted, &past)
	seedKBInvite(t, db, "pend-1", "kb-4", "user-b", types.KBInvitationStatusPending, nil)
	seedKBInvite(t, db, "other-1", "kb-5", "user-c", types.KBInvitationStatusAccepted, nil)

	ids, err := repo.ListAcceptedKBIDsByUser(ctx, "user-b")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"kb-1", "kb-2"}, ids)

	ids, err = repo.ListAcceptedKBIDsByUser(ctx, "")
	require.NoError(t, err)
	require.Empty(t, ids)
}
