package access

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// Task 6 integration finding: a tenantless real human (TenantID 0, genuine
// user ID, no API-key scope) reads platform-public rows as Viewer — for
// detail/read AND original download — while every other tenantless access
// stays denied exactly as before.

// tenantlessHumanCtx carries a real human identity with no active tenant.
func tenantlessHumanCtx() context.Context {
	return types.WithCaller(context.Background(), types.Caller{
		TenantID: 0, UserID: "plain-user", Role: types.TenantRoleMember,
	})
}

func TestResolveKBTenantlessHumanPublicRead(t *testing.T) {
	kb := platformPublicKB("pub", 9)
	ctx := tenantlessHumanCtx()
	grant, err := ResolveKB(ctx,
		KBRequest{Caller: types.CallerFromContext(ctx)}, kb, types.KBPermissionViewer, nil)
	require.NoError(t, err, "tenantless real human must read platform-public KBs")
	require.Equal(t, types.KBPermissionViewer, grant.Permission)
	require.Equal(t, uint64(9), grant.EffectiveTenantID,
		"content retrieval keeps the KB data-scope tenant")
}

func TestResolveKBTenantlessHumanPublicDownload(t *testing.T) {
	kb := platformPublicKB("pub", 9)
	ctx := tenantlessHumanCtx()
	grant, err := ResolveKBForDownload(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, kb)
	require.NoError(t, err, "tenantless real human must download platform-public originals")
	require.Equal(t, uint64(9), grant.EffectiveTenantID)
}

func TestResolveKBTenantlessPrivateDenied(t *testing.T) {
	priv := tenantOwnedKB("priv", 2)
	ctx := tenantlessHumanCtx()
	_, err := ResolveKB(ctx,
		KBRequest{Caller: types.CallerFromContext(ctx)}, priv, types.KBPermissionViewer, nil)
	require.ErrorIs(t, err, ErrUnauthorized, "tenantless private reads stay denied")
	_, err = ResolveKBForDownload(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, priv)
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestResolveKBTenantlessNonHumanDenied(t *testing.T) {
	kb := platformPublicKB("pub", 9)
	// Anonymous: no identity at all.
	_, err := ResolveKB(context.Background(), KBRequest{}, kb, types.KBPermissionViewer, nil)
	require.ErrorIs(t, err, ErrUnauthorized)
	_, err = ResolveKBForDownload(context.Background(), KBRequest{}, kb)
	require.ErrorIs(t, err, ErrUnauthorized)
	// Synthetic machine identity without tenant.
	synth := types.WithCaller(context.Background(), types.Caller{TenantID: 0, UserID: "system-0"})
	_, err = ResolveKB(synth, KBRequest{Caller: types.CallerFromContext(synth)}, kb, types.KBPermissionViewer, nil)
	require.ErrorIs(t, err, ErrUnauthorized)
	// Synthetic machine identity WITH tenant stays forbidden (not widened).
	synthT := types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "system-7"})
	_, err = ResolveKB(synthT, KBRequest{Caller: types.CallerFromContext(synthT)}, kb, types.KBPermissionViewer, nil)
	require.ErrorIs(t, err, ErrForbidden)
}

func TestResolveKBTenantlessManageDeniedNonSuperAdmin(t *testing.T) {
	kb := platformPublicKB("pub", 9)
	ctx := tenantlessHumanCtx()
	_, err := ResolveKBForManage(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, kb)
	require.ErrorIs(t, err, ErrUnauthorized,
		"tenantless mutation stays denied for non-SuperAdmins (existing rule)")
}
