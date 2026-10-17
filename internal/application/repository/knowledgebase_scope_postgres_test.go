package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestUpdateKnowledgeBaseScopeRevokesAndCountsPostgres: an owner-changing
// scope write revokes live invitations (stamping revoked_reason) and live
// tenant grants in the same transaction and reports how many. Skipped
// unless WEKNORA_MIGRATION_TEST_POSTGRES_DSN points at a disposable
// database.
func TestUpdateKnowledgeBaseScopeRevokesAndCountsPostgres(t *testing.T) {
	dsn := os.Getenv("WEKNORA_MIGRATION_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set WEKNORA_MIGRATION_TEST_POSTGRES_DSN to run the disposable PostgreSQL scope-revocation test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	schema := fmt.Sprintf("kb_scope_%d", time.Now().UnixNano())
	require.NoError(t, db.Exec("CREATE SCHEMA "+schema).Error)
	t.Cleanup(func() { db.Exec("DROP SCHEMA " + schema + " CASCADE") })
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.Exec("SET search_path TO "+schema).Error)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}, &types.KBInvitation{}, &types.KBAccessGrant{}))

	kb := &types.KnowledgeBase{ID: "kb-1", Name: "kb", TenantID: 1, OwnerTenantID: 1,
		Visibility: types.KBVisibilityRestricted}
	require.NoError(t, db.Create(kb).Error)
	for i, st := range []types.KBInvitationStatus{
		types.KBInvitationStatusPending, types.KBInvitationStatusAccepted, types.KBInvitationStatusRevoked,
	} {
		require.NoError(t, db.Create(&types.KBInvitation{
			ID: fmt.Sprintf("inv-%d", i), KBID: "kb-1", OwnerTenantID: 1, RecipientTenantID: 1,
			RecipientUserID: fmt.Sprintf("u-%d", i), Status: st, TokenHash: fmt.Sprintf("h-%d", i),
		}).Error)
	}
	require.NoError(t, db.Create(&types.KBAccessGrant{
		KBID: "kb-1", OwnerTenantID: 1, GranteeTenantID: 2, Status: types.GrantStatusApproved,
		Permission: types.KBPermissionViewer,
	}).Error)

	repo := NewKnowledgeBaseRepository(db)
	kb.OwnerTenantID = 2
	kb.Visibility = types.KBVisibilityTenant
	revoked, err := repo.UpdateKnowledgeBaseScope(context.Background(), kb, types.KBInviteRevokedReasonOwnerChanged)
	require.NoError(t, err)
	require.Equal(t, types.KBShareRevocation{Invitations: 2, Grants: 1}, revoked)

	var live int64
	require.NoError(t, db.Model(&types.KBInvitation{}).
		Where("kb_id = ? AND status <> ?", "kb-1", types.KBInvitationStatusRevoked).Count(&live).Error)
	require.Zero(t, live)
	var stamped int64
	require.NoError(t, db.Model(&types.KBInvitation{}).
		Where("kb_id = ? AND revoked_reason = ?", "kb-1", types.KBInviteRevokedReasonOwnerChanged).
		Count(&stamped).Error)
	require.Equal(t, int64(2), stamped)

	none, err := repo.UpdateKnowledgeBaseScope(context.Background(), kb, "")
	require.NoError(t, err)
	require.Equal(t, types.KBShareRevocation{}, none)
}
