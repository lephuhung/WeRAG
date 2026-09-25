package access

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestKBPermissionsPublicAnonymousAndSuperAdmin(t *testing.T) {
	scope := &types.KBScope{TenantID: 9, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic}
	lookup := &grantLookup{scope: scope, grants: map[string]types.KBPermission{}}

	t.Run("anonymous stays denied on public rows", func(t *testing.T) {
		allowed, err := NewKBPermissions(context.Background(), lookup).Check("kb", 9, types.KBPermissionViewer)
		require.NoError(t, err)
		require.False(t, allowed)
	})

	t.Run("tenantID-less explicit SuperAdmin resolves public rows", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), types.SystemAdminContextKey, true)
		ctx = types.WithCaller(ctx, types.Caller{TenantID: 0, UserID: "super-1", Role: types.TenantRoleMember})
		allowed, err := NewKBPermissions(ctx, lookup).Check("kb", 9, types.KBPermissionViewer)
		require.NoError(t, err)
		require.True(t, allowed)
	})

	t.Run("synthetic identity stays denied on public rows", func(t *testing.T) {
		ctx := types.WithCaller(context.Background(), types.Caller{
			TenantID: 9, UserID: "system-9", Role: types.TenantRoleMember,
		})
		allowed, err := NewKBPermissions(ctx, lookup).Check("kb", 9, types.KBPermissionViewer)
		require.NoError(t, err)
		require.False(t, allowed)
	})
}
