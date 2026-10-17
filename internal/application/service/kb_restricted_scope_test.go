package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func memberLifecycleCtx(tenant uint64) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenant)
	return types.WithCaller(ctx, types.Caller{TenantID: tenant, UserID: "member-1", Role: types.TenantRoleMember})
}

// Phase 3: tenant <-> restricted <-> published keep the owner and are the
// owner Tenant Admin's (or a SuperAdmin's) decision.
func TestSetVisibilityRestrictedTransitions(t *testing.T) {
	repo := newFakeKBRepo()
	repo.rows["kb-1"] = &types.KnowledgeBase{ID: "kb-1", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityTenant}
	svc := newLifecycleService(repo, newLifecycleTenantRepo(1, 2), &lifecycleAudit{})
	admin := tenantAdminLifecycleCtx(1)

	for _, next := range []types.KBVisibility{
		types.KBVisibilityRestricted, types.KBVisibilityPublished, types.KBVisibilityRestricted, types.KBVisibilityTenant,
	} {
		kb, err := svc.SetKnowledgeBaseVisibility(admin, "kb-1", next, 0)
		require.NoError(t, err, "-> %s", next)
		assert.Equal(t, next, kb.Visibility)
		assert.Equal(t, uint64(1), kb.OwnerTenantID, "the owner never changes")
	}
	assert.Empty(t, repo.scopeRevokeReasons[len(repo.scopeRevokeReasons)-1], "same owner revokes nothing")

	_, err := svc.SetKnowledgeBaseVisibility(memberLifecycleCtx(1), "kb-1", types.KBVisibilityRestricted, 0)
	require.Error(t, err, "a member cannot restrict the KB")
	_, err = svc.SetKnowledgeBaseVisibility(tenantAdminLifecycleCtx(2), "kb-1", types.KBVisibilityRestricted, 0)
	require.Error(t, err, "another tenant's admin cannot restrict the KB")
	_, err = svc.SetKnowledgeBaseVisibility(admin, "kb-1", types.KBVisibilityRestricted, 2)
	require.Error(t, err, "a different target tenant is an owner change, not a scope change")

	kb, err := svc.SetKnowledgeBaseVisibility(superAdminNoTenantLifecycleCtx(), "kb-1", types.KBVisibilityRestricted, 0)
	require.NoError(t, err, "a SuperAdmin restricts any tenant KB")
	assert.Equal(t, types.KBVisibilityRestricted, kb.Visibility)
	_, err = svc.SetKnowledgeBaseVisibility(superAdminNoTenantLifecycleCtx(), "kb-1", types.KBVisibilityRestricted, 0)
	require.NoError(t, err, "same scope is a no-op")
	_, err = svc.SetKnowledgeBaseVisibility(superAdminNoTenantLifecycleCtx(), "kb-1", types.KBVisibilityPublic, 0)
	require.Error(t, err, "restricted goes back to tenant before platform promotion")
}

func TestSetKnowledgeBaseMemberDefault(t *testing.T) {
	repo := newFakeKBRepo()
	repo.rows["kb-1"] = &types.KnowledgeBase{ID: "kb-1", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityTenant}
	repo.rows["pub"] = &types.KnowledgeBase{ID: "pub", TenantID: 9, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic}
	svc := newLifecycleService(repo, newLifecycleTenantRepo(1), &lifecycleAudit{})

	kb, err := svc.SetKnowledgeBaseMemberDefault(tenantAdminLifecycleCtx(1), "kb-1", types.KBMemberDefaultView)
	require.NoError(t, err)
	assert.Equal(t, types.KBMemberDefaultView, kb.MemberDefault)

	_, err = svc.SetKnowledgeBaseMemberDefault(memberLifecycleCtx(1), "kb-1", types.KBMemberDefaultContribute)
	require.Error(t, err, "members do not decide their own rights")
	_, err = svc.SetKnowledgeBaseMemberDefault(apiKeyManageLifecycleCtx(), "kb-1", types.KBMemberDefaultContribute)
	require.Error(t, err, "API keys never change access")
	_, err = svc.SetKnowledgeBaseMemberDefault(tenantAdminLifecycleCtx(1), "kb-1", "edit")
	require.Error(t, err, "unknown values are rejected")
	_, err = svc.SetKnowledgeBaseMemberDefault(superAdminNoTenantLifecycleCtx(), "pub", types.KBMemberDefaultView)
	require.Error(t, err, "platform KBs have no member tenant")

	kb, err = svc.SetKnowledgeBaseMemberDefault(superAdminNoTenantLifecycleCtx(), "kb-1", types.KBMemberDefaultContribute)
	require.NoError(t, err)
	assert.Equal(t, types.KBMemberDefaultContribute, kb.MemberDefault)
}

// Attaching a KB to a tenant's MCP endpoint asks whether an ordinary member
// of that tenant could read it — never the editor's own rights.
func TestEndpointTenantCanRead(t *testing.T) {
	grants := scopeLookup{} // ApprovedKBPermission: viewer for every pair
	noGrants := writableGrantLookup{}
	kb := func(owner uint64, vis types.KBVisibility) *types.KnowledgeBase {
		return &types.KnowledgeBase{ID: "kb", TenantID: owner, OwnerTenantID: owner, Visibility: vis}
	}
	assert.True(t, endpointTenantCanRead(7, kb(7, types.KBVisibilityTenant), noGrants), "own tenant KB")
	assert.False(t, endpointTenantCanRead(7, kb(7, types.KBVisibilityRestricted), noGrants),
		"a restricted KB needs a per-user ACL, which an endpoint never has")
	assert.False(t, endpointTenantCanRead(7, kb(8, types.KBVisibilityTenant), noGrants),
		"another tenant's private KB, even when a SuperAdmin edits the endpoint")
	assert.True(t, endpointTenantCanRead(7, kb(8, types.KBVisibilityTenant), grants), "tenant grant")
	assert.True(t, endpointTenantCanRead(7, kb(8, types.KBVisibilityPublished), noGrants), "published")
	assert.True(t, endpointTenantCanRead(7, &types.KnowledgeBase{ID: "kb", TenantID: 9,
		Visibility: types.KBVisibilityPublic}, noGrants), "platform public")
	assert.False(t, endpointTenantCanRead(0, kb(7, types.KBVisibilityTenant), noGrants))
}

func TestCallerIsTenantAdminOrSuperAdmin(t *testing.T) {
	assert.True(t, callerIsTenantAdminOrSuperAdmin(
		context.WithValue(tenantAdminLifecycleCtx(1), types.TenantRoleContextKey, types.TenantRoleAdmin)))
	assert.False(t, callerIsTenantAdminOrSuperAdmin(
		context.WithValue(memberLifecycleCtx(1), types.TenantRoleContextKey, types.TenantRoleMember)))
	assert.True(t, callerIsTenantAdminOrSuperAdmin(superAdminNoTenantLifecycleCtx()),
		"a SuperAdmin without a membership reads channel sessions like an admin")
	assert.False(t, callerIsTenantAdminOrSuperAdmin(apiKeyManageLifecycleCtx()),
		"an API key never counts as SuperAdmin")
}

// Phase 5: the create form picks the scope — restricted is accepted on the
// tenant create flow and member_default is kept; public stays forbidden.
func TestCreateKnowledgeBaseScopeChoice(t *testing.T) {
	repo := newFakeKBRepo()
	svc := newLifecycleService(repo, newLifecycleTenantRepo(1), &lifecycleAudit{})
	kb, err := svc.CreateKnowledgeBase(ctxWithTenant(1),
		&types.KnowledgeBase{Name: "r", Visibility: types.KBVisibilityRestricted})
	require.NoError(t, err)
	assert.Equal(t, types.KBVisibilityRestricted, kb.Visibility)
	assert.Equal(t, uint64(1), kb.OwnerTenantID)

	kb, err = svc.CreateKnowledgeBase(ctxWithTenant(1),
		&types.KnowledgeBase{Name: "t", MemberDefault: types.KBMemberDefaultView})
	require.NoError(t, err)
	assert.Equal(t, types.KBVisibilityTenant, kb.Visibility, "empty scope defaults to tenant")
	assert.Equal(t, types.KBMemberDefaultView, kb.MemberDefault)

	kb, err = svc.CreateKnowledgeBase(ctxWithTenant(1),
		&types.KnowledgeBase{Name: "p", Visibility: types.KBVisibilityPublished})
	require.NoError(t, err)
	assert.Equal(t, types.KBVisibilityTenant, kb.Visibility, "publishing happens after creation")
}
