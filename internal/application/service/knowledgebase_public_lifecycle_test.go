package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Task 3 (platform public KBs): lifecycle, scope changes, auditing.
//
// RED-first coverage:
//   - Tenant Admin can create tenant KBs but cannot create/promote to public.
//   - Explicit human SuperAdmin can create public KBs and change scope with
//     or without an active tenant; CanAccessAllTenants alone cannot.
//   - API-key manage_kbs is not system-admin authority.
//   - public→tenant rejects missing/invalid targets without mutating the row.
//   - Scope transitions preserve KB ID, data-scope tenant_id, children and
//     indexes (only owner/visibility metadata changes).
//   - Audit records actor, old/new owner+visibility, target tenant, KB ID,
//     and outcome.
// ---------------------------------------------------------------------------

type lifecycleTenantRepo struct {
	tenants map[uint64]*types.Tenant
}

func newLifecycleTenantRepo(ids ...uint64) *lifecycleTenantRepo {
	r := &lifecycleTenantRepo{tenants: map[uint64]*types.Tenant{}}
	for _, id := range ids {
		r.tenants[id] = &types.Tenant{ID: id, Name: "t"}
	}
	return r
}

func (r *lifecycleTenantRepo) CreateTenant(_ context.Context, _ *types.Tenant) error {
	return nil
}
func (r *lifecycleTenantRepo) GetTenantByID(_ context.Context, id uint64) (*types.Tenant, error) {
	if t, ok := r.tenants[id]; ok {
		return t, nil
	}
	// Mirror the production repository contract so tests exercise the
	// service's ErrTenantNotFound → 404 mapping branch.
	return nil, repository.ErrTenantNotFound
}
func (r *lifecycleTenantRepo) GetTenantsByIDs(_ context.Context, _ []uint64) (map[uint64]*types.Tenant, error) {
	return map[uint64]*types.Tenant{}, nil
}
func (r *lifecycleTenantRepo) ListTenants(_ context.Context) ([]*types.Tenant, error) {
	return nil, nil
}
func (r *lifecycleTenantRepo) SearchTenants(
	_ context.Context, _ string, _ uint64, _, _ int,
) ([]*types.Tenant, int64, error) {
	return nil, 0, nil
}
func (r *lifecycleTenantRepo) UpdateTenant(_ context.Context, _ *types.Tenant) error { return nil }
func (r *lifecycleTenantRepo) DeleteTenant(_ context.Context, _ uint64) error        { return nil }
func (r *lifecycleTenantRepo) AdjustStorageUsed(_ context.Context, _ uint64, _ int64) error {
	return nil
}
func (r *lifecycleTenantRepo) BulkSetStorageQuota(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}

var _ interfaces.TenantRepository = (*lifecycleTenantRepo)(nil)

type lifecycleAudit struct {
	entries []*types.AuditLog
}

func (a *lifecycleAudit) Log(_ context.Context, e *types.AuditLog) error {
	a.entries = append(a.entries, e)
	return nil
}
func (a *lifecycleAudit) LogDenied(
	_ context.Context, _ *gin.Context, _ uint64, _, _ string, _ types.TenantRole,
) error {
	return nil
}
func (a *lifecycleAudit) List(_ context.Context, _ uint64, _ *interfaces.AuditLogQuery) ([]*types.AuditLog, error) {
	return nil, nil
}
func (a *lifecycleAudit) Purge(_ context.Context, _ int) (int64, error) { return 0, nil }

func tenantAdminLifecycleCtx(tenant uint64) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenant)
	return types.WithCaller(ctx, types.Caller{TenantID: tenant, UserID: "admin-1", Role: types.TenantRoleAdmin})
}

func superAdminLifecycleCtx(tenant uint64) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenant)
	ctx = types.WithCaller(ctx, types.Caller{TenantID: tenant, UserID: "super-1", Role: types.TenantRoleAdmin})
	return context.WithValue(ctx, types.SystemAdminContextKey, true)
}

func superAdminNoTenantLifecycleCtx() context.Context {
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 0, UserID: "super-1", Role: types.TenantRoleMember})
	return context.WithValue(ctx, types.SystemAdminContextKey, true)
}

func crossTenantOnlyLifecycleCtx() context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(3))
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 3, UserID: "user-a", Role: types.TenantRoleAdmin})
	return context.WithValue(ctx, types.UserContextKey,
		&types.User{ID: "user-a", TenantID: 3, CanAccessAllTenants: true})
}

func apiKeyManageLifecycleCtx() context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "system-1", Role: types.TenantRoleAdmin})
	ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
	return types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{
		KeyID: 7, Name: "k", Capabilities: types.StringArray{string(types.APIKeyCapabilityManageKnowledgeBases)},
	})
}

func newLifecycleService(repo *fakeKBRepo, tenants *lifecycleTenantRepo, audit *lifecycleAudit) *knowledgeBaseService {
	return &knowledgeBaseService{repo: repo, tenantRepo: tenants, audit: audit, asynqClient: &lifecycleEnqueuer{}}
}

type lifecycleEnqueuer struct{}

func (e *lifecycleEnqueuer) Enqueue(_ *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	return &asynq.TaskInfo{}, nil
}

func TestPublicLifecycle_TenantAdminCannotCreatePublic(t *testing.T) {
	repo := newFakeKBRepo()
	audit := &lifecycleAudit{}
	svc := newLifecycleService(repo, newLifecycleTenantRepo(1), audit)
	_, err := svc.CreatePublicKnowledgeBase(tenantAdminLifecycleCtx(1), &types.KnowledgeBase{Name: "pub"})
	require.Error(t, err, "tenant admin must not create platform-owned public KBs")
	assert.Empty(t, repo.rows, "rejected create must not persist")
}

func TestPublicLifecycle_SuperAdminCanCreatePublic(t *testing.T) {
	for _, ctx := range []context.Context{superAdminLifecycleCtx(1), superAdminNoTenantLifecycleCtx()} {
		repo := newFakeKBRepo()
		audit := &lifecycleAudit{}
		svc := newLifecycleService(repo, newLifecycleTenantRepo(1), audit)
		kb, err := svc.CreatePublicKnowledgeBase(ctx, &types.KnowledgeBase{Name: "pub"})
		require.NoError(t, err)
		require.NotNil(t, kb)
		assert.Equal(t, uint64(0), kb.OwnerTenantID)
		assert.Equal(t, uint64(0), kb.TenantID, "public create uses platform data scope, never the active tenant")
		assert.Equal(t, types.KBVisibilityPublic, kb.Visibility)
		require.NoError(t, kb.ValidateOwnership())
	}
}

func TestPublicLifecycle_PublicCreateIgnoresTenantDefaults(t *testing.T) {
	repo := newFakeKBRepo()
	audit := &lifecycleAudit{}
	svc := newLifecycleService(repo, newLifecycleTenantRepo(1), audit)
	ctx := superAdminLifecycleCtx(1)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, &types.Tenant{
		ID: 1, StorageEngineConfig: &types.StorageEngineConfig{DefaultProvider: "minio"},
	})
	kb, err := svc.CreatePublicKnowledgeBase(ctx, &types.KnowledgeBase{Name: "pub"})
	require.NoError(t, err)
	assert.NotEqual(t, "minio", kb.GetStorageProvider(),
		"public create must not inherit the selected tenant's storage default")
	assert.Nil(t, kb.StorageBackendID, "no tenant-scoped backend binding on platform rows")
}

func TestPublicLifecycle_CrossTenantOnlyAndAPIKeyDenied(t *testing.T) {
	for name, ctx := range map[string]context.Context{
		"can-access-all-tenants alone": crossTenantOnlyLifecycleCtx(),
		"api-key manage_kbs":           apiKeyManageLifecycleCtx(),
	} {
		repo := newFakeKBRepo()
		audit := &lifecycleAudit{}
		svc := newLifecycleService(repo, newLifecycleTenantRepo(1), audit)
		_, err := svc.CreatePublicKnowledgeBase(ctx, &types.KnowledgeBase{Name: "pub"})
		require.Error(t, err, name)
		assert.Empty(t, repo.rows, "%s: rejected create must not persist", name)
	}
}

func TestPublicLifecycle_TenantCreateSetsOwner(t *testing.T) {
	repo := newFakeKBRepo()
	audit := &lifecycleAudit{}
	svc := newLifecycleService(repo, newLifecycleTenantRepo(1), audit)
	kb, err := svc.CreateKnowledgeBase(ctxWithTenant(1), &types.KnowledgeBase{Name: "kb"})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), kb.OwnerTenantID)
	assert.Equal(t, uint64(1), kb.TenantID)
	require.NoError(t, kb.ValidateOwnership())
}

func TestPublicLifecycle_TenantCreateRejectsPublicVisibility(t *testing.T) {
	repo := newFakeKBRepo()
	audit := &lifecycleAudit{}
	svc := newLifecycleService(repo, newLifecycleTenantRepo(1), audit)
	_, err := svc.CreateKnowledgeBase(ctxWithTenant(1),
		&types.KnowledgeBase{Name: "kb", Visibility: types.KBVisibilityPublic})
	require.Error(t, err, "tenant create flow must not mint public KBs")
}

func TestPublicLifecycle_TenantAdminCannotPromote(t *testing.T) {
	repo := newFakeKBRepo()
	repo.rows["kb-1"] = &types.KnowledgeBase{
		ID: "kb-1", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityTenant,
	}
	audit := &lifecycleAudit{}
	svc := newLifecycleService(repo, newLifecycleTenantRepo(1), audit)
	_, err := svc.SetKnowledgeBaseVisibility(tenantAdminLifecycleCtx(1), "kb-1", types.KBVisibilityPublic, 0)
	require.Error(t, err)
	stored := repo.rows["kb-1"]
	assert.Equal(t, types.KBVisibilityTenant, stored.Visibility)
	assert.Equal(t, uint64(1), stored.OwnerTenantID)
}

func TestPublicLifecycle_SuperAdminTenantToPublic(t *testing.T) {
	repo := newFakeKBRepo()
	repo.rows["kb-1"] = &types.KnowledgeBase{
		ID: "kb-1", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityTenant,
	}
	audit := &lifecycleAudit{}
	svc := newLifecycleService(repo, newLifecycleTenantRepo(1), audit)
	kb, err := svc.SetKnowledgeBaseVisibility(superAdminLifecycleCtx(1), "kb-1", types.KBVisibilityPublic, 0)
	require.NoError(t, err)
	assert.Equal(t, "kb-1", kb.ID)
	assert.Equal(t, uint64(1), kb.TenantID, "transition preserves the data scope")
	assert.Equal(t, uint64(0), kb.OwnerTenantID)
	assert.Equal(t, types.KBVisibilityPublic, kb.Visibility)
	require.NoError(t, kb.ValidateOwnership())
}

func TestPublicLifecycle_PublicToTenantRequiresValidTarget(t *testing.T) {
	setup := func() (*fakeKBRepo, *lifecycleAudit, *knowledgeBaseService) {
		repo := newFakeKBRepo()
		repo.rows["pub-1"] = &types.KnowledgeBase{
			ID: "pub-1", TenantID: 0, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic,
		}
		audit := &lifecycleAudit{}
		return repo, audit, newLifecycleService(repo, newLifecycleTenantRepo(2), audit)
	}
	ctx := superAdminNoTenantLifecycleCtx()

	repo, _, svc := setup()
	_, err := svc.SetKnowledgeBaseVisibility(ctx, "pub-1", types.KBVisibilityTenant, 0)
	require.Error(t, err, "missing target must be rejected")
	assert.Equal(t, uint64(0), repo.rows["pub-1"].OwnerTenantID, "rejected transition must not mutate")

	repo, _, svc = setup()
	_, err = svc.SetKnowledgeBaseVisibility(ctx, "pub-1", types.KBVisibilityTenant, 99)
	require.Error(t, err, "unknown target tenant must be rejected")
	// The production mapping for a missing destination is 404, not merely
	// any error: the fake returns repository.ErrTenantNotFound exactly so
	// this branch is proven.
	appErr, ok := apperrors.IsAppError(err)
	require.True(t, ok, "unknown target must surface a typed AppError, got %T", err)
	assert.Equal(t, http.StatusNotFound, appErr.HTTPCode)
	assert.Equal(t, uint64(0), repo.rows["pub-1"].OwnerTenantID, "rejected transition must not mutate")
	assert.Equal(t, types.KBVisibilityPublic, repo.rows["pub-1"].Visibility)

	repo, _, svc = setup()
	kb, err := svc.SetKnowledgeBaseVisibility(ctx, "pub-1", types.KBVisibilityTenant, 2)
	require.NoError(t, err)
	assert.Equal(t, "pub-1", kb.ID)
	assert.Equal(t, uint64(0), kb.TenantID, "transition preserves the platform data scope")
	assert.Equal(t, uint64(2), kb.OwnerTenantID)
	assert.Equal(t, types.KBVisibilityTenant, kb.Visibility)
	require.NoError(t, kb.ValidateOwnership())
}

func TestPublicLifecycle_TenantToTenantTransferRejected(t *testing.T) {
	repo := newFakeKBRepo()
	repo.rows["kb-1"] = &types.KnowledgeBase{
		ID: "kb-1", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityTenant,
	}
	audit := &lifecycleAudit{}
	svc := newLifecycleService(repo, newLifecycleTenantRepo(1, 2), audit)
	_, err := svc.SetKnowledgeBaseVisibility(superAdminLifecycleCtx(1), "kb-1", types.KBVisibilityTenant, 2)
	require.Error(t, err, "tenant-to-tenant transfer is not a supported transition")
	assert.Equal(t, uint64(1), repo.rows["kb-1"].OwnerTenantID)
}

// decodeScopeDetails parses the kb.scope_changed details payload back
// into a map so tests assert exact keys instead of substrings.
func decodeScopeDetails(t *testing.T, entry *types.AuditLog) map[string]any {
	t.Helper()
	require.NotEmpty(t, entry.Details, "scope-change audit must carry details")
	var details map[string]any
	require.NoError(t, json.Unmarshal(entry.Details, &details))
	return details
}

func TestPublicLifecycle_ScopeChangeAuditedTenantToPublic(t *testing.T) {
	repo := newFakeKBRepo()
	repo.rows["kb-1"] = &types.KnowledgeBase{
		ID: "kb-1", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityTenant,
	}
	audit := &lifecycleAudit{}
	svc := newLifecycleService(repo, newLifecycleTenantRepo(1), audit)
	_, err := svc.SetKnowledgeBaseVisibility(superAdminLifecycleCtx(1), "kb-1", types.KBVisibilityPublic, 0)
	require.NoError(t, err)
	require.Len(t, audit.entries, 1)
	entry := audit.entries[0]
	assert.Equal(t, types.AuditActionKBScopeChanged, entry.Action)
	assert.Equal(t, "kb-1", entry.ScopeID)
	assert.Equal(t, "knowledge_base", entry.ScopeType)
	assert.Equal(t, "kb-1", entry.TargetID)
	assert.Equal(t, "super-1", entry.ActorUserID)
	assert.Equal(t, types.AuditOutcomeSuccess, entry.Outcome)
	// Tenant attribution follows the new owner, falling back to the old
	// owner when the new scope is platform-owned.
	assert.Equal(t, uint64(1), entry.TenantID)
	details := decodeScopeDetails(t, entry)
	assert.Equal(t, "kb-1", details["kb_id"])
	assert.Equal(t, "super-1", details["actor_user_id"])
	assert.Equal(t, float64(1), details["old_owner_tenant"])
	assert.Equal(t, "tenant", details["old_visibility"])
	assert.Equal(t, float64(0), details["new_owner_tenant"])
	assert.Equal(t, "public", details["new_visibility"])
	assert.Equal(t, "success", details["outcome"])
	_, hasTarget := details["target_tenant_id"]
	assert.False(t, hasTarget, "no destination tenant on tenant→public")
}

func TestPublicLifecycle_ScopeChangeAuditedPublicToTenant(t *testing.T) {
	repo := newFakeKBRepo()
	repo.rows["pub-1"] = &types.KnowledgeBase{
		ID: "pub-1", TenantID: 0, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic,
	}
	audit := &lifecycleAudit{}
	svc := newLifecycleService(repo, newLifecycleTenantRepo(2), audit)
	_, err := svc.SetKnowledgeBaseVisibility(superAdminNoTenantLifecycleCtx(), "pub-1", types.KBVisibilityTenant, 2)
	require.NoError(t, err)
	require.Len(t, audit.entries, 1)
	entry := audit.entries[0]
	assert.Equal(t, types.AuditActionKBScopeChanged, entry.Action)
	assert.Equal(t, "pub-1", entry.ScopeID)
	assert.Equal(t, "pub-1", entry.TargetID)
	assert.Equal(t, "super-1", entry.ActorUserID)
	assert.Equal(t, types.AuditOutcomeSuccess, entry.Outcome)
	assert.Equal(t, uint64(2), entry.TenantID, "audit attributed to the destination tenant")
	details := decodeScopeDetails(t, entry)
	assert.Equal(t, "pub-1", details["kb_id"])
	assert.Equal(t, "super-1", details["actor_user_id"])
	assert.Equal(t, float64(0), details["old_owner_tenant"])
	assert.Equal(t, "public", details["old_visibility"])
	assert.Equal(t, float64(2), details["new_owner_tenant"])
	assert.Equal(t, "tenant", details["new_visibility"])
	assert.Equal(t, float64(2), details["target_tenant_id"])
	assert.Equal(t, "success", details["outcome"])
}

func TestPublicLifecycle_PublicCreateAuditedAtTenantZero(t *testing.T) {
	repo := newFakeKBRepo()
	audit := &lifecycleAudit{}
	svc := newLifecycleService(repo, newLifecycleTenantRepo(1), audit)
	kb, err := svc.CreatePublicKnowledgeBase(superAdminNoTenantLifecycleCtx(), &types.KnowledgeBase{Name: "pub"})
	require.NoError(t, err)
	require.Len(t, audit.entries, 1, "tenant-zero events must be observed, not dropped")
	entry := audit.entries[0]
	assert.Equal(t, types.AuditActionKBScopeChanged, entry.Action)
	assert.Equal(t, uint64(0), entry.TenantID, "platform create is attributed to tenant 0")
	assert.Equal(t, kb.ID, entry.ScopeID)
	assert.Equal(t, kb.ID, entry.TargetID)
	assert.Equal(t, "super-1", entry.ActorUserID)
	assert.Equal(t, types.AuditOutcomeSuccess, entry.Outcome)
	details := decodeScopeDetails(t, entry)
	assert.Equal(t, kb.ID, details["kb_id"])
	assert.Equal(t, float64(0), details["new_owner_tenant"])
	assert.Equal(t, "public", details["new_visibility"])
	_, hasOld := details["old_owner_tenant"]
	assert.False(t, hasOld, "creation has no previous scope")
}

func TestPublicLifecycle_ScopeChangeFailedOutcomeAudited(t *testing.T) {
	repo := newFakeKBRepo()
	repo.rows["kb-1"] = &types.KnowledgeBase{
		ID: "kb-1", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityTenant,
	}
	repo.updateErr = assert.AnError
	audit := &lifecycleAudit{}
	svc := newLifecycleService(repo, newLifecycleTenantRepo(1), audit)
	_, err := svc.SetKnowledgeBaseVisibility(superAdminLifecycleCtx(1), "kb-1", types.KBVisibilityPublic, 0)
	require.Error(t, err, "persistence failure must propagate")
	require.Len(t, audit.entries, 1, "failed transitions must still emit audit")
	entry := audit.entries[0]
	assert.Equal(t, types.AuditActionKBScopeChanged, entry.Action)
	assert.Equal(t, "kb-1", entry.ScopeID)
	assert.Equal(t, "kb-1", entry.TargetID)
	assert.Equal(t, "super-1", entry.ActorUserID)
	assert.Equal(t, types.AuditOutcomeFailed, entry.Outcome)
	details := decodeScopeDetails(t, entry)
	assert.Equal(t, float64(1), details["old_owner_tenant"])
	assert.Equal(t, "tenant", details["old_visibility"])
	assert.Equal(t, float64(0), details["new_owner_tenant"])
	assert.Equal(t, "public", details["new_visibility"])
	assert.Equal(t, "failed", details["outcome"])
}

func TestPublicLifecycle_SuperAdminOutsideTenantCannotMutateTenantKB(t *testing.T) {
	repo := newFakeKBRepo()
	repo.rows["kb-1"] = &types.KnowledgeBase{
		ID: "kb-1", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityTenant, Name: "kb",
	}
	audit := &lifecycleAudit{}
	svc := newLifecycleService(repo, newLifecycleTenantRepo(1), audit)
	// Explicit SuperAdmin acting in another tenant (or none) is denied on
	// tenant-owned rows; only the owning tenant's policy grants mutation.
	_, err := svc.UpdateKnowledgeBase(superAdminLifecycleCtx(9), "kb-1", "new", "", nil)
	require.Error(t, err)
	assert.Equal(t, "kb", repo.rows["kb-1"].Name)
	require.Error(t, svc.DeleteKnowledgeBase(superAdminNoTenantLifecycleCtx(), "kb-1"))
	assert.Contains(t, repo.rows, "kb-1")
}

func TestPublicLifecycle_SuperAdminManagesPublicKB(t *testing.T) {
	repo := newFakeKBRepo()
	repo.rows["pub-1"] = &types.KnowledgeBase{
		ID: "pub-1", TenantID: 0, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic, Name: "pub",
	}
	audit := &lifecycleAudit{}
	svc := newLifecycleService(repo, newLifecycleTenantRepo(1), audit)
	kb, err := svc.UpdateKnowledgeBase(superAdminNoTenantLifecycleCtx(), "pub-1", "pub2", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "pub2", kb.Name)
	require.NoError(t, svc.DeleteKnowledgeBase(superAdminNoTenantLifecycleCtx(), "pub-1"))
}
