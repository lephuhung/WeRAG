package access

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// platformPublicKB builds a platform-owned public KB whose documents live in
// the given data-scope tenant. Authorization must use OwnerTenantID (0) +
// Visibility while content operations keep TenantID as execution scope.
func platformPublicKB(id string, dataTenant uint64) *types.KnowledgeBase {
	return &types.KnowledgeBase{
		ID: id, TenantID: dataTenant,
		OwnerTenantID: 0, Visibility: types.KBVisibilityPublic,
	}
}

// tenantOwnedKB builds a normal tenant-owned KB (owner == data scope).
func tenantOwnedKB(id string, owner uint64) *types.KnowledgeBase {
	return &types.KnowledgeBase{
		ID: id, TenantID: owner,
		OwnerTenantID: owner, Visibility: types.KBVisibilityTenant,
	}
}

func humanCallerCtx(tenant uint64, user string) context.Context {
	return types.WithCaller(context.Background(), types.Caller{
		TenantID: tenant, UserID: user, Role: types.TenantRoleMember,
	})
}

func sysAdminCallerCtx(tenant uint64, user string) context.Context {
	return context.WithValue(humanCallerCtx(tenant, user), types.SystemAdminContextKey, true)
}

func TestResolveKBPublicReadGrantsViewerWithDataScope(t *testing.T) {
	kb := platformPublicKB("pub", 9)
	ctx := humanCallerCtx(3, "user-a")
	grant, err := ResolveKB(ctx,
		KBRequest{Caller: types.CallerFromContext(ctx)}, kb, types.KBPermissionViewer, nil)
	require.NoError(t, err)
	require.Equal(t, types.KBPermissionViewer, grant.Permission)
	require.Equal(t, uint64(9), grant.EffectiveTenantID,
		"successful reads retain kb.TenantID as the effective content tenant")
	require.True(t, HasKBGrant(grant.WithGrant(ctx), "pub", 9, types.KBPermissionViewer))
	require.False(t, HasKBGrant(grant.WithGrant(ctx), "pub", 9, types.KBPermissionEditor),
		"a public read must never mint a write grant")
}

func TestResolveKBPublicReadDeniesUnauthenticated(t *testing.T) {
	kb := platformPublicKB("pub", 9)
	_, err := ResolveKB(context.Background(),
		KBRequest{}, kb, types.KBPermissionViewer, nil)
	require.ErrorIs(t, err, ErrUnauthorized)
	_, err = ResolveKB(humanCallerCtx(0, ""), KBRequest{Caller: types.Caller{TenantID: 0}}, kb,
		types.KBPermissionViewer, nil)
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestResolveKBPublicReadDeniesSyntheticAPIKeyIdentity(t *testing.T) {
	kb := platformPublicKB("pub", 9)
	ctx := humanCallerCtx(7, "system-7")
	_, err := ResolveKB(ctx,
		KBRequest{Caller: types.CallerFromContext(ctx)}, kb, types.KBPermissionViewer, nil)
	require.ErrorIs(t, err, ErrForbidden,
		"public visibility must not expand machine/API-key identities")
}

func TestResolveKBPublicReadKeepsAPIKeyAllowlist(t *testing.T) {
	kb := platformPublicKB("pub", 9)
	ctx := types.WithTenantAPIKeyScope(humanCallerCtx(3, "user-a"),
		types.TenantAPIKeyScope{KnowledgeBaseIDs: types.StringArray{"other"}})
	_, err := ResolveKB(ctx,
		KBRequest{Caller: types.CallerFromContext(ctx)}, kb, types.KBPermissionViewer, nil)
	require.Error(t, err, "allowlisted-out keys stay denied even on public KBs")
}

func TestResolveKBCrossTenantWritesDeniedOnPublic(t *testing.T) {
	kb := platformPublicKB("pub", 9)
	for _, role := range []types.TenantRole{types.TenantRoleMember, types.TenantRoleAdmin} {
		ctx := types.WithCaller(context.Background(), types.Caller{
			TenantID: 3, UserID: "user-a", Role: role,
		})
		_, err := ResolveKB(ctx,
			KBRequest{Caller: types.CallerFromContext(ctx)}, kb, types.KBPermissionEditor, nil)
		require.ErrorIs(t, err, ErrForbidden, "role=%s", role)
	}
}

func TestResolveKBExplicitSuperAdminWriteOnlyViaManagePath(t *testing.T) {
	kb := platformPublicKB("pub", 9)
	ctx := sysAdminCallerCtx(3, "super-1")
	_, err := ResolveKB(ctx,
		KBRequest{Caller: types.CallerFromContext(ctx)}, kb, types.KBPermissionEditor, nil)
	require.ErrorIs(t, err, ErrForbidden,
		"ResolveKB must not imply write from public Viewer; SuperAdmin writes go through ResolveKBForManage")
}

func TestResolveKBTenantIDZeroSuperAdminReadsPublic(t *testing.T) {
	kb := platformPublicKB("pub", 0)
	ctx := sysAdminCallerCtx(0, "super-1")
	grant, err := ResolveKB(ctx,
		KBRequest{Caller: types.CallerFromContext(ctx)}, kb, types.KBPermissionViewer, nil)
	require.NoError(t, err, "explicit human SuperAdmin with no active tenant resolves public KBs")
	require.Equal(t, uint64(0), grant.EffectiveTenantID)
}

// Task 6 integration finding: a tenantless real human (no active tenant
// but a genuine user identity) reads platform-public rows as Viewer —
// discovery without a workspace is meaningless if consumption 401s.
// Anonymous callers (no identity at all) stay unauthorized.
func TestResolveKBTenantIDZeroOrdinaryHumanReadsPublic(t *testing.T) {
	kb := platformPublicKB("pub", 9)
	ctx := humanCallerCtx(0, "user-a")
	grant, err := ResolveKB(ctx,
		KBRequest{Caller: types.CallerFromContext(ctx)}, kb, types.KBPermissionViewer, nil)
	require.NoError(t, err, "tenantless real human must read platform-public KBs")
	require.Equal(t, uint64(9), grant.EffectiveTenantID)

	_, err = ResolveKB(context.Background(),
		KBRequest{}, kb, types.KBPermissionViewer, nil)
	require.ErrorIs(t, err, ErrUnauthorized, "anonymous stays unauthorized")
}

func TestResolveKBCanAccessAllTenantsAloneDenied(t *testing.T) {
	kb := platformPublicKB("pub", 9)
	ctx := context.WithValue(humanCallerCtx(3, "user-a"),
		types.UserContextKey, &types.User{ID: "user-a", TenantID: 3, CanAccessAllTenants: true})
	_, err := ResolveKB(ctx,
		KBRequest{Caller: types.CallerFromContext(ctx)}, kb, types.KBPermissionEditor, nil)
	require.ErrorIs(t, err, ErrForbidden,
		"CanAccessAllTenants without explicit is_system_admin grants no public management")
}

func TestResolveKBForDownloadMatrix(t *testing.T) {
	pub := platformPublicKB("pub", 9)
	priv := tenantOwnedKB("priv", 2)

	t.Run("public authenticated human may download through data scope", func(t *testing.T) {
		ctx := humanCallerCtx(3, "user-a")
		grant, err := ResolveKBForDownload(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, pub)
		require.NoError(t, err)
		require.Equal(t, uint64(9), grant.EffectiveTenantID)
		require.True(t, HasKBGrant(grant.WithGrant(ctx), "pub", 9, types.KBPermissionViewer))
		require.False(t, HasKBGrant(grant.WithGrant(ctx), "pub", 9, types.KBPermissionEditor),
			"a download grant must not satisfy writes")
	})

	t.Run("anonymous cannot download public originals", func(t *testing.T) {
		_, err := ResolveKBForDownload(context.Background(), KBRequest{}, pub)
		require.ErrorIs(t, err, ErrUnauthorized)
	})

	t.Run("uninvited cross-tenant private denied", func(t *testing.T) {
		ctx := humanCallerCtx(3, "user-a")
		_, err := ResolveKBForDownload(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, priv)
		require.ErrorIs(t, err, ErrForbidden)
	})

	t.Run("invitation viewer keeps no-download behavior", func(t *testing.T) {
		ctx := humanCallerCtx(3, "user-b")
		invites := &stubInvites{accepted: map[string]bool{"priv\x00user-b": true}}
		read, err := ResolveKBWithInvite(ctx,
			KBRequest{Caller: types.CallerFromContext(ctx)}, priv, types.KBPermissionViewer, nil, invites)
		require.NoError(t, err, "invite still grants read")
		require.Equal(t, types.KBPermissionViewer, read.Permission)
		_, err = ResolveKBForDownload(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, priv)
		require.ErrorIs(t, err, ErrForbidden, "invite read must not confer original download")
		_ = read
	})

	t.Run("same-owner-tenant keeps tenant policy", func(t *testing.T) {
		ctx := humanCallerCtx(2, "user-a")
		grant, err := ResolveKBForDownload(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, priv)
		require.NoError(t, err)
		require.Equal(t, uint64(2), grant.EffectiveTenantID)
	})

	t.Run("synthetic identity denied", func(t *testing.T) {
		ctx := humanCallerCtx(7, "system-7")
		_, err := ResolveKBForDownload(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, pub)
		require.ErrorIs(t, err, ErrForbidden)
	})
}

func TestResolveKBForManageMatrix(t *testing.T) {
	pub := platformPublicKB("pub", 9)

	t.Run("explicit human SuperAdmin manages platform-owned", func(t *testing.T) {
		for _, tenant := range []uint64{3, 0} {
			ctx := sysAdminCallerCtx(tenant, "super-1")
			grant, err := ResolveKBForManage(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, pub)
			require.NoError(t, err, "tenant=%d", tenant)
			require.Equal(t, uint64(9), grant.EffectiveTenantID)
		}
	})

	t.Run("tenant admin and member denied on platform-owned", func(t *testing.T) {
		for _, role := range []types.TenantRole{types.TenantRoleAdmin, types.TenantRoleMember} {
			ctx := types.WithCaller(context.Background(), types.Caller{
				TenantID: 9, UserID: "user-a", Role: role,
			})
			_, err := ResolveKBForManage(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, pub)
			require.ErrorIs(t, err, ErrForbidden, "role=%s", role)
		}
	})

	t.Run("CanAccessAllTenants alone denied", func(t *testing.T) {
		ctx := context.WithValue(humanCallerCtx(3, "user-a"),
			types.UserContextKey, &types.User{ID: "user-a", TenantID: 3, CanAccessAllTenants: true})
		_, err := ResolveKBForManage(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, pub)
		require.ErrorIs(t, err, ErrForbidden)
	})

	t.Run("API keys never manage public", func(t *testing.T) {
		ctx := types.WithTenantAPIKeyScope(sysAdminCallerCtx(3, "super-1"),
			types.TenantAPIKeyScope{FullAccess: true})
		_, err := ResolveKBForManage(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, pub)
		require.ErrorIs(t, err, ErrForbidden)
	})

	t.Run("owning tenant keeps existing policy", func(t *testing.T) {
		owned := tenantOwnedKB("own", 3)
		ctx := humanCallerCtx(3, "user-a")
		grant, err := ResolveKBForManage(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, owned)
		require.NoError(t, err)
		require.Equal(t, uint64(3), grant.EffectiveTenantID)
		foreign := humanCallerCtx(4, "user-b")
		_, err = ResolveKBForManage(foreign, KBRequest{Caller: types.CallerFromContext(foreign)}, owned)
		require.ErrorIs(t, err, ErrForbidden)
	})
}

func TestRequireKBManageMatrix(t *testing.T) {
	pub := platformPublicKB("pub", 9)

	t.Run("platform-owned allows explicit human SuperAdmin only", func(t *testing.T) {
		require.NoError(t, RequireKBManage(sysAdminCallerCtx(3, "super-1"), pub))
		require.NoError(t, RequireKBManage(sysAdminCallerCtx(0, "super-1"), pub))
		denied, err := ResolveKB(sysAdminCallerCtx(3, "super-1"),
			KBRequest{Caller: types.CallerFromContext(sysAdminCallerCtx(3, "super-1"))},
			pub, types.KBPermissionViewer, nil)
		require.NoError(t, err)
		_ = denied
		ctx := humanCallerCtx(3, "user-a")
		require.ErrorIs(t, RequireKBManage(ctx, pub), ErrForbidden)
		adminCtx := types.WithCaller(context.Background(), types.Caller{
			TenantID: 3, UserID: "user-a", Role: types.TenantRoleAdmin,
		})
		require.ErrorIs(t, RequireKBManage(adminCtx, pub), ErrForbidden)
		_, isKey := types.TenantAPIKeyScopeFromContext(types.WithTenantAPIKeyScope(ctx,
			types.TenantAPIKeyScope{FullAccess: true}))
		require.True(t, isKey)
		require.ErrorIs(t, RequireKBManage(types.WithTenantAPIKeyScope(ctx,
			types.TenantAPIKeyScope{FullAccess: true}), pub), ErrForbidden)
	})

	t.Run("tenant-owned still needs an editor grant", func(t *testing.T) {
		owned := tenantOwnedKB("own", 3)
		ctx := humanCallerCtx(3, "user-a")
		require.ErrorIs(t, RequireKBManage(ctx, owned), ErrForbidden)
		write, err := ResolveKB(ctx, KBRequest{Caller: types.CallerFromContext(ctx)},
			owned, types.KBPermissionEditor, nil)
		require.NoError(t, err)
		require.NoError(t, RequireKBManage(write.Context(ctx), owned))
	})

	t.Run("nil KB is not found", func(t *testing.T) {
		require.ErrorIs(t, RequireKBManage(humanCallerCtx(3, "user-a"), nil), ErrNotFound)
	})
}
