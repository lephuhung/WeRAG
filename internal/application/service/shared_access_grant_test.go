package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// fakeKBGrantService is an in-memory KBAccessGrantService for tests. Only
// the read-side lookup methods carry behaviour; the lifecycle methods are
// inherited from the embedded interface and panic if a test reaches them.
type fakeKBGrantService struct {
	interfaces.KBAccessGrantService
	// allowedKBs maps kbID → grant permission for the caller's tenant.
	allowedKBs map[string]types.KBPermission
	// callers records every grantee tenant queried, to assert that grant
	// checks always run against the original caller — never the execution
	// tenant of a shared execution context.
	callers []uint64
}

func (f *fakeKBGrantService) ApprovedKBPermission(
	_ context.Context, kbID string, granteeTenantID uint64,
) (types.KBPermission, bool, error) {
	f.callers = append(f.callers, granteeTenantID)
	permission, ok := f.allowedKBs[kbID]
	if !ok {
		return "", false, nil
	}
	return permission, true, nil
}

func (f *fakeKBGrantService) GetKBScope(_ context.Context, _ string) (*types.KBScope, error) {
	return nil, nil
}

func (f *fakeKBGrantService) GrantedKBIDs(_ context.Context, _ uint64) ([]string, error) {
	var ids []string
	for kbID := range f.allowedKBs {
		ids = append(ids, kbID)
	}
	return ids, nil
}

func setupKnowledgeSharedAccessDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}))
	return db
}

func newKnowledgeSharedAccessService(t *testing.T, grants interfaces.KBAccessGrantService) (*knowledgeService, *gorm.DB) {
	t.Helper()

	db := setupKnowledgeSharedAccessDB(t)
	repo := repository.NewKnowledgeRepository(db)
	return &knowledgeService{
		repo:                 repo,
		kbAccessGrantService: grants,
	}, db
}

func newSharedAccessContext() context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "user-1")
	return ctx
}

func seedKnowledge(t *testing.T, db *gorm.DB, knowledge *types.Knowledge) {
	t.Helper()
	require.NoError(t, db.Create(knowledge).Error)
}

func TestGetKnowledgeBatchWithSharedAccessRejectsLegacyTenantGrant(t *testing.T) {
	service, db := newKnowledgeSharedAccessService(t, &fakeKBGrantService{
		allowedKBs: map[string]types.KBPermission{"kb-shared": types.KBPermissionViewer},
	})
	seedKnowledge(t, db, &types.Knowledge{ID: "k1", TenantID: 2, KnowledgeBaseID: "kb-shared", Type: "file"})
	seedKnowledge(t, db, &types.Knowledge{ID: "k2", TenantID: 2, KnowledgeBaseID: "kb-private", Type: "file"})

	ctx := newSharedAccessContext()
	rows, err := service.GetKnowledgeBatchWithSharedAccess(ctx, 2, []string{"k1", "k2"})
	require.NoError(t, err)
	require.Empty(t, rows, "legacy tenant-wide grant must not reveal foreign knowledge")
}

// emptyWebSearchProviderRepo is a no-op WebSearchProviderRepository for
// session-service tests that never reach provider resolution.
type emptyWebSearchProviderRepo struct {
	interfaces.WebSearchProviderRepository
}

func (emptyWebSearchProviderRepo) GetDefault(
	context.Context, uint64,
) (*types.WebSearchProviderEntity, error) {
	return nil, nil
}
