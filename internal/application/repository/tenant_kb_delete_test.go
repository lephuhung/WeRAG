package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDeleteTenant_BlockedByActiveKBDataScope(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := NewTenantRepository(db)

	tenant := &types.Tenant{Name: "blocked", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	blocker := &types.KnowledgeBase{
		ID: "kb-blocker", Name: "blocker",
		TenantID: tenant.ID, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic,
	}
	require.NoError(t, db.Create(blocker).Error)

	err := repo.DeleteTenant(ctx, tenant.ID)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTenantHasKnowledgeBase)
	var depErr *TenantKBDependencyError
	require.True(t, errors.As(err, &depErr), "rejection must carry the blocking KB ID")
	assert.Equal(t, "kb-blocker", depErr.KnowledgeBaseID)

	// Failed delete leaves the tenant intact.
	var tenantCount int64
	require.NoError(t, db.Model(&types.Tenant{}).Count(&tenantCount).Error)
	assert.Equal(t, int64(1), tenantCount)
}

func TestDeleteTenant_UnrelatedKBsDoNotBlock(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := NewTenantRepository(db)

	tenant := &types.Tenant{Name: "gone", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	other := &types.Tenant{Name: "other", Status: "active"}
	require.NoError(t, db.Create(other).Error)
	unrelated := &types.KnowledgeBase{
		ID: "kb-other", Name: "unrelated",
		TenantID: other.ID, OwnerTenantID: other.ID, Visibility: types.KBVisibilityTenant,
	}
	require.NoError(t, db.Create(unrelated).Error)
	// Soft-deleted KBs in the same data scope do not block either.
	deleted := &types.KnowledgeBase{
		ID: "kb-deleted", Name: "deleted",
		TenantID: tenant.ID, OwnerTenantID: tenant.ID, Visibility: types.KBVisibilityTenant,
	}
	require.NoError(t, db.Create(deleted).Error)
	require.NoError(t, db.Delete(deleted).Error)

	require.NoError(t, repo.DeleteTenant(ctx, tenant.ID))
	var tenantCount int64
	require.NoError(t, db.Model(&types.Tenant{}).Where("id = ?", tenant.ID).Count(&tenantCount).Error)
	assert.Equal(t, int64(0), tenantCount)
}

// Task 3 fix round 1: KB creation and tenant deletion serialize on the
// live tenant row. Creation through the repository locks and verifies the
// tenant before inserting; a deleted or missing tenant fails instead of
// minting an orphaned data-scope row. Platform scope (tenant_id 0) stays
// independent of any tenant row.
func TestCreateKnowledgeBase_RequiresLiveTenant(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	kbRepo := NewKnowledgeBaseRepository(db)
	tenantRepo := NewTenantRepository(db)

	mkKB := func(id string, tenantID uint64) *types.KnowledgeBase {
		return &types.KnowledgeBase{
			ID: id, Name: id,
			TenantID: tenantID, OwnerTenantID: tenantID, Visibility: types.KBVisibilityTenant,
		}
	}

	// Missing tenant: creation fails instead of orphaning the row.
	require.ErrorIs(t, kbRepo.CreateKnowledgeBase(ctx, mkKB("kb-missing", 4242)), ErrTenantNotFound)

	// Soft-deleted tenant: creation fails after the delete commits.
	dead := &types.Tenant{Name: "dead", Status: "active"}
	require.NoError(t, db.Create(dead).Error)
	require.NoError(t, tenantRepo.DeleteTenant(ctx, dead.ID))
	require.ErrorIs(t, kbRepo.CreateKnowledgeBase(ctx, mkKB("kb-dead", dead.ID)), ErrTenantNotFound)

	// Live tenant: creation succeeds.
	live := &types.Tenant{Name: "live", Status: "active"}
	require.NoError(t, db.Create(live).Error)
	require.NoError(t, kbRepo.CreateKnowledgeBase(ctx, mkKB("kb-live", live.ID)))

	// Platform scope needs no tenant row and never touches one.
	require.NoError(t, kbRepo.CreateKnowledgeBase(ctx, &types.KnowledgeBase{
		ID: "kb-pub", Name: "kb-pub",
		TenantID: 0, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic,
	}))
}

func TestDeleteTenant_MissingTenantNotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewTenantRepository(db)
	require.ErrorIs(t, repo.DeleteTenant(context.Background(), 4242), ErrTenantNotFound)
}

// Task 3 fix round 1: concurrent create-vs-delete serialization. The
// assertion is order-independent: whatever interleaving wins, the final
// state must never hold a live KB whose data-scope tenant is soft-deleted
// (either the delete observed the KB and rejected, or the create observed
// the deleted tenant and failed).
//
// Dialect note: SQLite ignores SELECT ... FOR UPDATE (verified: GORM emits
// no locking clause) and serializes writers at the database level instead;
// Postgres takes a real tenant row lock in both paths. This test runs the
// race on a shared file-backed SQLite database with a busy timeout so both
// orders are exercised across repetitions.
func TestDeleteTenantCreateKnowledgeBase_Serialized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "race.db")
	dsn := fmt.Sprintf("file:%s?cache=shared&mode=rwc&_journal_mode=WAL&_busy_timeout=10000", path)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.TenantMember{}))
	require.NoError(t, db.Exec(knowledgeBasesTestDDL).Error)

	ctx := context.Background()
	tenantRepo := NewTenantRepository(db)
	kbRepo := NewKnowledgeBaseRepository(db)
	tenant := &types.Tenant{Name: "raced", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)

	for i := 0; i < 10; i++ {
		kbID := fmt.Sprintf("kb-race-%d", i)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var delErr, createErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			delErr = tenantRepo.DeleteTenant(ctx, tenant.ID)
		}()
		go func() {
			defer wg.Done()
			<-start
			createErr = kbRepo.CreateKnowledgeBase(ctx, &types.KnowledgeBase{
				ID: kbID, Name: kbID,
				TenantID: tenant.ID, OwnerTenantID: tenant.ID, Visibility: types.KBVisibilityTenant,
			})
		}()
		close(start)
		wg.Wait()
		_ = delErr
		_ = createErr
		// The tenant may be gone (delete won) or alive with the KB
		// (create won and delete rejected). If the tenant is gone, the
		// racing create must have failed rather than orphaning the row.
		var tenantAlive int64
		require.NoError(t, db.Model(&types.Tenant{}).Where("id = ?", tenant.ID).Count(&tenantAlive).Error)
		var kbAlive int64
		require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id = ?", kbID).Count(&kbAlive).Error)
		require.False(t, tenantAlive == 0 && kbAlive == 1,
			"iter %d: orphaned live KB %q under soft-deleted tenant %d (delErr=%v createErr=%v)",
			i, kbID, tenant.ID, delErr, createErr)
		if tenantAlive == 0 {
			// Delete won this round: recreate the tenant so later
			// iterations keep racing instead of short-circuiting.
			tenant = &types.Tenant{Name: fmt.Sprintf("raced-%d", i), Status: "active"}
			require.NoError(t, db.Create(tenant).Error)
		}
	}
	_ = os.Remove(path)
}
