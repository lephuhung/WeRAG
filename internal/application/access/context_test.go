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

func TestKBInviteSurvivesExecutionSwitchAndDetachWithoutWidening(t *testing.T) {
	ctx := callerContext()
	kb := &types.KnowledgeBase{ID: "shared", TenantID: 2}
	grants := &grantLookup{grants: map[string]types.KBPermission{"shared/1": types.KBPermissionViewer}}
	invites := &stubInvites{accepted: map[string]bool{"shared\x00user": true}}
	grant, err := ResolveKBWithInvite(
		ctx,
		KBRequest{Caller: types.CallerFromContext(ctx)},
		kb,
		types.KBPermissionViewer,
		grants,
		invites,
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
	require.Empty(t, grants.queriedKB, "legacy tenant grants are not consulted")
	require.Zero(t, grants.queriedT, "legacy tenant grants are not consulted")
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

func TestKBPermissionsPublicOwnerSemantics(t *testing.T) {
	for _, tt := range []struct {
		name       string
		scope      *types.KBScope
		required   types.KBPermission
		want       bool
		wantLegacy bool
	}{
		{
			name:     "platform public row grants cross-tenant human read",
			scope:    &types.KBScope{TenantID: 2, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic},
			required: types.KBPermissionViewer, want: true,
		},
		{
			name:     "platform public row never grants writes",
			scope:    &types.KBScope{TenantID: 2, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic},
			required: types.KBPermissionEditor, want: false,
		},
		{
			name:     "tenant row with legacy grant map stays denied cross-tenant",
			scope:    &types.KBScope{TenantID: 2, OwnerTenantID: 2, Visibility: types.KBVisibilityTenant},
			required: types.KBPermissionViewer, want: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := types.WithCaller(callerContext(), types.Caller{TenantID: 1, UserID: "user", Role: types.TenantRoleMember})
			lookup := &grantLookup{
				scope:  tt.scope,
				grants: map[string]types.KBPermission{"kb/1": types.KBPermissionViewer},
			}
			allowed, err := NewKBPermissions(ctx, lookup).Check("kb", 2, tt.required)
			require.NoError(t, err)
			require.Equal(t, tt.want, allowed)
			legacyAllowed, err := NewKBGrantPermissions(ctx, lookup, 1).Check("kb", types.KBPermissionViewer)
			require.NoError(t, err)
			require.False(t, legacyAllowed, "tenant-wide grant helper must fail closed")
		})
	}
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
