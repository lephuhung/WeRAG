package access

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// Phase 2 (F7): an explicit human SuperAdmin holds every right on every
// KB, tenant-owned included, without a membership in the owner tenant.
// The grant keeps the caller identity and runs in kb.TenantID.
func TestSuperAdminHoldsEveryRightOnForeignTenantKB(t *testing.T) {
	owned := tenantOwnedKB("own", 3)
	for _, callerTenant := range []uint64{0, 4} { // tenantless and another tenant
		ctx := sysAdminCallerCtx(callerTenant, "super-1")
		caller := types.CallerFromContext(ctx)

		manage, err := ResolveKBForManage(ctx, KBRequest{Caller: caller}, owned)
		require.NoError(t, err, "manage, caller tenant=%d", callerTenant)
		require.Equal(t, types.KBPermissionAdmin, manage.Permission)
		require.Equal(t, uint64(3), manage.EffectiveTenantID)
		require.Equal(t, caller, manage.Caller, "the grant never changes the caller")
		require.NoError(t, RequireKBWrite(manage.Context(ctx), owned),
			"the manage grant satisfies the write check")
		require.NoError(t, RequireKBManage(ctx, owned),
			"RequireKBManage admits the SuperAdmin without a grant")

		download, err := ResolveKBForDownload(ctx, KBRequest{Caller: caller}, owned)
		require.NoError(t, err, "download, caller tenant=%d", callerTenant)
		require.Equal(t, uint64(3), download.EffectiveTenantID)

		for _, required := range []types.KBPermission{
			types.KBPermissionViewer, types.KBPermissionEditor, types.KBPermissionAdmin,
		} {
			grant, err := ResolveKB(ctx, KBRequest{Caller: caller}, owned, required, nil)
			require.NoError(t, err, "ResolveKB %s, caller tenant=%d", required, callerTenant)
			require.Equal(t, types.KBPermissionAdmin, grant.Permission)
			require.Equal(t, uint64(3), grant.EffectiveTenantID)
		}
	}
}

// Platform-owned rows: a SuperAdmin writes through either resolver
// (settled 2026-10-17: every right on every KB).
func TestSuperAdminPlatformKBWritesViaEitherPath(t *testing.T) {
	pub := platformPublicKB("pub", 9)
	ctx := sysAdminCallerCtx(4, "super-1")
	caller := types.CallerFromContext(ctx)
	_, err := ResolveKB(ctx, KBRequest{Caller: caller}, pub, types.KBPermissionEditor, nil)
	require.NoError(t, err)
	_, err = ResolveKBForManage(ctx, KBRequest{Caller: caller}, pub)
	require.NoError(t, err)
}

// API keys never gain anything from the SuperAdmin branch, even with the
// system-admin flag in context and a full-access scope.
func TestAPIKeyNeverGainsSuperAdminKBRights(t *testing.T) {
	owned := tenantOwnedKB("own", 3)
	ctx := types.WithTenantAPIKeyScope(sysAdminCallerCtx(4, "super-1"),
		types.TenantAPIKeyScope{FullAccess: true})
	caller := types.CallerFromContext(ctx)
	_, err := ResolveKBForManage(ctx, KBRequest{Caller: caller}, owned)
	require.ErrorIs(t, err, ErrForbidden)
	_, err = ResolveKBForDownload(ctx, KBRequest{Caller: caller}, owned)
	require.ErrorIs(t, err, ErrForbidden)
	_, err = ResolveKB(ctx, KBRequest{Caller: caller}, owned, types.KBPermissionViewer, nil)
	require.ErrorIs(t, err, ErrForbidden)
	require.ErrorIs(t, RequireKBManage(ctx, owned), ErrForbidden)
}

// Neither the legacy flag nor a synthetic identity with the system-admin
// flag counts.
func TestNonHumanOrFlagOnlyGetsNoForeignKBRights(t *testing.T) {
	owned := tenantOwnedKB("own", 3)
	synthetic := sysAdminCallerCtx(4, "system-4")
	_, err := ResolveKBForManage(synthetic, KBRequest{Caller: types.CallerFromContext(synthetic)}, owned)
	require.ErrorIs(t, err, ErrForbidden)

	flagged := humanCallerCtx(4, "user-a")
	flagged = withUser(flagged, &types.User{ID: "user-a", TenantID: 4, CanAccessAllTenants: true})
	_, err = ResolveKBForManage(flagged, KBRequest{Caller: types.CallerFromContext(flagged)}, owned)
	require.ErrorIs(t, err, ErrForbidden)
	_, err = ResolveKBForDownload(flagged, KBRequest{Caller: types.CallerFromContext(flagged)}, owned)
	require.ErrorIs(t, err, ErrForbidden)
}

func withUser(ctx context.Context, u *types.User) context.Context {
	return context.WithValue(ctx, types.UserContextKey, u)
}
