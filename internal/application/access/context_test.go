package access

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func callerContext() context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "user")
	return context.WithValue(ctx, types.TenantRoleContextKey, types.TenantRoleMember)
}

func TestKBGrantSurvivesExecutionSwitchAndDetachWithoutWidening(t *testing.T) {
	ctx := callerContext()
	kb := &types.KnowledgeBase{ID: "shared", TenantID: 2}
	grants := &grantLookup{grants: map[string]types.KBPermission{"shared/1": types.KBPermissionViewer}}
	grant, err := ResolveKB(
		ctx,
		KBRequest{Caller: types.CallerFromContext(ctx)},
		kb,
		types.KBPermissionViewer,
		grants,
	)
	require.NoError(t, err)
	ctx = grant.Context(ctx)
	require.Equal(t, uint64(2), types.MustTenantIDFromContext(ctx))
	ctx = logger.CloneContext(types.WithExecutionTenant(ctx, 3))
	require.Equal(
		t,
		types.Caller{TenantID: 1, UserID: "user", Role: types.TenantRoleMember},
		types.CallerFromContext(ctx),
	)
	require.True(t, HasKBGrant(ctx, "shared", 2, types.KBPermissionViewer))
	// A later mutation of the service model cannot mutate the captured grant.
	kb.ID = "private"
	require.False(t, HasKBGrant(ctx, "private", 2, types.KBPermissionViewer))
	require.False(t, HasKBGrant(ctx, "shared", 3, types.KBPermissionViewer))
	require.False(t, HasKBGrant(ctx, "shared", 2, types.KBPermissionEditor))
	for _, caller := range []types.Caller{
		{TenantID: 9, UserID: "user", Role: types.TenantRoleMember},
		{TenantID: 1, UserID: "other", Role: types.TenantRoleMember},
		{TenantID: 1, UserID: "user", Role: types.TenantRoleAdmin},
	} {
		otherCtx := types.WithCaller(ctx, caller)
		require.False(t, HasKBGrant(otherCtx, "shared", 2, types.KBPermissionViewer))
		require.Equal(
			t,
			caller,
			types.CallerFromContext(grant.Context(otherCtx)),
			"reusing a grant cannot impersonate its caller",
		)
		require.Equal(
			t,
			uint64(3),
			types.MustTenantIDFromContext(grant.Context(otherCtx)),
			"a caller mismatch must not change execution scope",
		)
	}
	narrowed := types.WithTenantAPIKeyScope(
		ctx,
		types.TenantAPIKeyScope{KnowledgeBaseIDs: types.StringArray{"another"}},
	)
	require.False(t, HasKBGrant(narrowed, "shared", 2, types.KBPermissionViewer))
}

func TestExecutionTenantDoesNotGrantOwnershipOrChangeGrantIdentity(t *testing.T) {
	ctx := types.WithExecutionTenant(callerContext(), 2)
	grants := &grantLookup{grants: map[string]types.KBPermission{}}
	allowed, err := NewKBPermissions(ctx, grants).Check("private", 2, types.KBPermissionViewer)
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, "private", grants.queriedKB)
	require.Equal(t, uint64(1), grants.queriedT, "grant lookup must use the caller tenant, not the execution tenant")
	ctx = types.WithExecutionTenant(context.Background(), 2)
	allowed, err = NewKBPermissions(ctx, nil).Check("private", 2, types.KBPermissionViewer)
	require.NoError(t, err)
	require.False(t, allowed, "an absent caller must not become the execution tenant")
}

func TestKBGrantBranchesRemainIndependent(t *testing.T) {
	base := callerContext()
	grant := &KBAccess{Caller: types.CallerFromContext(base), EffectiveTenantID: 2, Permission: types.KBPermissionViewer}
	grant.KnowledgeBase = &types.KnowledgeBase{ID: "first", TenantID: 2}
	first := grant.WithGrant(base)
	grant.KnowledgeBase = &types.KnowledgeBase{ID: "second", TenantID: 2}
	second := grant.WithGrant(base)
	require.True(t, HasKBGrant(first, "first", 2, types.KBPermissionViewer))
	require.False(t, HasKBGrant(first, "second", 2, types.KBPermissionViewer))
	require.True(t, HasKBGrant(second, "second", 2, types.KBPermissionViewer))
	require.False(t, HasKBGrant(second, "first", 2, types.KBPermissionViewer))
}

func TestKBPermissionsOwnerShortcutOnlyGrantsRead(t *testing.T) {
	roles := []types.TenantRole{types.TenantRoleMember, types.TenantRoleMember, types.TenantRoleAdmin}
	for _, role := range roles {
		ctx := context.WithValue(callerContext(), types.TenantRoleContextKey, role)
		permissions := NewKBPermissions(ctx, nil)
		for _, required := range []types.KBPermission{types.KBPermissionViewer, types.KBPermissionEditor, types.KBPermissionAdmin} {
			allowed, err := permissions.Check("kb", 1, required)
			require.NoError(t, err)
			require.Equal(t, required == types.KBPermissionViewer, allowed)
		}
		grant := &KBAccess{
			Caller:            types.CallerFromContext(ctx),
			KnowledgeBase:     &types.KnowledgeBase{ID: "kb", TenantID: 1},
			EffectiveTenantID: 1,
			Permission:        types.KBPermissionEditor,
		}
		permissions = NewKBPermissions(grant.Context(ctx), nil)
		allowed, err := permissions.Check("kb", 1, types.KBPermissionEditor)
		require.NoError(t, err)
		require.True(t, allowed)
		allowed, err = permissions.Check("kb", 1, types.KBPermissionAdmin)
		require.NoError(t, err)
		require.False(t, allowed)
	}
}
