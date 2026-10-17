package access

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// Task 6 fix round 1: platform API-key principals must never count as human
// for public access. Identity is Principal.StorageID() = "api_platform:<id>"
// (not synthetic), so IsHumanCaller alone admits them once checkAPIKeyScope
// passes (unrestricted or explicitly allowlisted scope + retrieve).

func platformKeyCtx(tenant uint64, kbIDs ...string) context.Context {
	ctx := types.WithTenantAPIKeyScope(context.Background(), types.TenantAPIKeyScope{
		KeyID: 9, Name: "platform-key", ScopeType: types.APIKeyScopePlatform,
		Capabilities:     types.StringArray{string(types.APIKeyCapabilityRetrieve)},
		KnowledgeBaseIDs: kbIDs,
	})
	ctx = types.WithCaller(ctx, types.Caller{
		TenantID: tenant, UserID: "api_platform:9", Role: types.TenantRoleMember,
	})
	return types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPIPlatform, ID: "9"})
}

func TestPlatformKeyPublicReadDenied(t *testing.T) {
	kb := platformPublicKB("pub", 9)
	for name, ctx := range map[string]context.Context{
		"with tenant unrestricted": platformKeyCtx(3),
		"with tenant allowlisted":  platformKeyCtx(3, "pub"),
		"tenantless unrestricted":  platformKeyCtx(0),
		"tenantless allowlisted":   platformKeyCtx(0, "pub"),
	} {
		_, err := ResolveKB(ctx,
			KBRequest{Caller: types.CallerFromContext(ctx)}, kb, types.KBPermissionViewer, nil)
		require.Error(t, err, "%s: platform key must not read public KBs", name)
		_, err = ResolveKBForDownload(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, kb)
		require.Error(t, err, "%s: platform key must not download public originals", name)
	}
}

type platformKeyScopeLookup struct{}

func (platformKeyScopeLookup) GetKBScope(context.Context, string) (*types.KBScope, error) {
	return &types.KBScope{TenantID: 9, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic}, nil
}

func (platformKeyScopeLookup) ApprovedKBPermission(context.Context, string, uint64) (types.KBPermission, bool, error) {
	return "", false, nil
}

func TestPlatformKeyScopeCheckDenied(t *testing.T) {
	ctx := platformKeyCtx(3, "pub")
	permissions := NewKBPermissions(ctx, platformKeyScopeLookup{})
	ok, _ := permissions.Check("pub", 0, types.KBPermissionViewer)
	require.False(t, ok, "platform key must fail the scope Check on public rows")
}

func TestAuthenticatedHumanHelper(t *testing.T) {
	kb := platformPublicKB("pub", 9)
	_ = kb
	human := types.WithCaller(context.Background(), types.Caller{
		TenantID: 3, UserID: "user-a", Role: types.TenantRoleMember,
	})
	require.True(t, IsAuthenticatedHuman(human, types.CallerFromContext(human)))
	require.True(t, IsAuthenticatedHuman(tenantlessHumanCtx(), types.CallerFromContext(tenantlessHumanCtx())))
	for _, ctx := range []context.Context{
		platformKeyCtx(3), platformKeyCtx(0), context.Background(),
		types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "system-7"}),
	} {
		require.False(t, IsAuthenticatedHuman(ctx, types.CallerFromContext(ctx)),
			"keys, anonymous, and synthetic identities are never human")
	}
	// API principal type without an explicit scope still fails closed.
	external := types.WithPrincipal(
		types.WithCaller(context.Background(), types.Caller{TenantID: 3, UserID: "ext-1"}),
		types.Principal{Type: types.PrincipalAPIExternalUser, ID: "ext-1"},
	)
	require.False(t, IsAuthenticatedHuman(external, types.CallerFromContext(external)))
}

// A machine principal of the platform tenant itself (the "SuperAdmin"
// workspace, PlatformTenantID) administers the platform-public KBs stored
// there: that is how the legal feed's MCP endpoint writes them. Any other
// tenant's key stays denied (TestPlatformKeyPublicReadDenied), and a public
// KB whose data lives elsewhere is not covered.
func TestPlatformTenantMachinePrincipalAdministersPublicKBs(t *testing.T) {
	kb := platformPublicKB("pub", types.PlatformTenantID)
	ctx := platformKeyCtx(types.PlatformTenantID, "pub")
	grant, err := ResolveKB(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, kb, types.KBPermissionEditor, nil)
	require.NoError(t, err)
	require.Equal(t, types.KBPermissionAdmin, grant.Permission)
	require.Equal(t, types.PlatformTenantID, grant.EffectiveTenantID)

	_, err = ResolveKB(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, platformPublicKB("pub", 9), types.KBPermissionViewer, nil)
	require.Error(t, err, "public KB stored in another tenant is not covered")

	other := platformKeyCtx(types.PlatformTenantID, "other-kb")
	_, err = ResolveKB(other, KBRequest{Caller: types.CallerFromContext(other)}, kb, types.KBPermissionViewer, nil)
	require.Error(t, err, "the key's KB allow-list still applies")
}
