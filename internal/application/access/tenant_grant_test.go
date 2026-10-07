package access

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestHasTenantGrant(t *testing.T) {
	member := types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleMember}
	live := &grantLookup{grants: map[string]types.KBPermission{"kb/1": types.KBPermissionViewer}}
	bg := context.Background()

	require.True(t, HasTenantGrant(bg, member, "kb", 2, live), "live grant to the caller's tenant")
	require.False(t, HasTenantGrant(bg, member, "kb", 2, nil), "nil lookup fails closed")
	require.False(t, HasTenantGrant(bg, member, "kb", 0, live), "platform-owned rows never use tenant grants")
	require.False(t, HasTenantGrant(bg, member, "kb", 1, live), "owner tenant needs no grant")
	require.False(t, HasTenantGrant(bg, types.Caller{UserID: "u1"}, "kb", 2, live), "tenantless caller")
	require.False(t, HasTenantGrant(bg, types.Caller{TenantID: 3, UserID: "u3"}, "kb", 2, live),
		"grant to tenant 1 does not reach tenant 3")
	require.False(t, HasTenantGrant(bg, member, "other", 2, live), "grant is per KB")

	keyCtx := types.WithTenantAPIKeyScope(bg, types.TenantAPIKeyScope{FullAccess: true})
	require.False(t, HasTenantGrant(keyCtx, member, "kb", 2, live), "API-key principals never use tenant grants")

	failing := &grantLookup{err: errors.New("db down")}
	require.False(t, HasTenantGrant(bg, member, "kb", 2, failing), "lookup errors fail closed")
}

func TestPublishedKBReadOnlyForHumans(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb-pub", TenantID: 2, OwnerTenantID: 2, Visibility: types.KBVisibilityPublished}
	bg := context.Background()
	reader := types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleMember}

	got, err := ResolveKB(bg, KBRequest{Caller: reader}, kb, types.KBPermissionViewer, nil)
	require.NoError(t, err)
	require.Equal(t, types.KBPermissionViewer, got.Permission)
	require.Equal(t, uint64(2), got.EffectiveTenantID, "content is read from the owner's data scope")

	_, err = ResolveKB(bg, KBRequest{Caller: reader}, kb, types.KBPermissionEditor, nil)
	require.ErrorIs(t, err, ErrForbidden, "published never grants writes to other tenants")
	_, err = ResolveKBForManage(bg, KBRequest{Caller: reader}, kb)
	require.ErrorIs(t, err, ErrForbidden)

	owner := types.Caller{TenantID: 2, UserID: "u2", Role: types.TenantRoleMember}
	got, err = ResolveKBForManage(bg, KBRequest{Caller: owner}, kb)
	require.NoError(t, err, "the owning tenant keeps writes")
	require.Equal(t, types.KBPermissionAdmin, got.Permission)

	tenantless := types.Caller{UserID: "u3"}
	_, err = ResolveKB(bg, KBRequest{Caller: tenantless}, kb, types.KBPermissionViewer, nil)
	require.NoError(t, err)

	keyCtx := types.WithTenantAPIKeyScope(bg, types.TenantAPIKeyScope{FullAccess: true})
	_, err = ResolveKB(keyCtx, KBRequest{Caller: reader}, kb, types.KBPermissionViewer, nil)
	require.Error(t, err, "API keys never read published KBs of other tenants")

	_, err = ResolveKB(bg, KBRequest{Caller: types.Caller{TenantID: 1, UserID: "system-1"}}, kb, types.KBPermissionViewer, nil)
	require.ErrorIs(t, err, ErrForbidden, "synthetic identities never read published KBs")

	scope := &grantLookup{scope: &types.KBScope{TenantID: 2, OwnerTenantID: 2, Visibility: types.KBVisibilityPublished}}
	ctx := types.WithCaller(bg, reader)
	ok, err := NewKBPermissions(ctx, scope).Check("kb-pub", 2, types.KBPermissionViewer)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = NewKBPermissions(ctx, scope).Check("kb-pub", 2, types.KBPermissionEditor)
	require.NoError(t, err)
	require.False(t, ok)
}
