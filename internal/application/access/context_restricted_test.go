package access

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// errScopeLookup fails every scope load.
type errScopeLookup struct{ *grantLookup }

func (errScopeLookup) GetKBScope(context.Context, string) (*types.KBScope, error) {
	return nil, errors.New("db down")
}

// TestKBPermissionsCheckFollowsDecideKB is the read check used by search,
// chat Q&A target selection, custom agents and FAQ/tag reads. It must
// follow DecideKB(ActionView): restricted KBs are readable only by the
// owner tenant's Admins, SuperAdmins and live ACL holders.
func TestKBPermissionsCheckFollowsDecideKB(t *testing.T) {
	const owner, other = uint64(1), uint64(2)
	scope := func(v types.KBVisibility, def types.KBMemberDefault) *types.KBScope {
		if v == types.KBVisibilityPublic {
			return &types.KBScope{TenantID: 9, OwnerTenantID: 0, Visibility: v, MemberDefault: def}
		}
		return &types.KBScope{TenantID: owner, OwnerTenantID: owner, Visibility: v, MemberDefault: def}
	}
	member := types.Caller{TenantID: owner, UserID: "m", Role: types.TenantRoleMember}
	admin := types.Caller{TenantID: owner, UserID: "a", Role: types.TenantRoleAdmin}
	outsider := types.Caller{TenantID: other, UserID: "o", Role: types.TenantRoleMember}
	guest := types.Caller{TenantID: other, UserID: "g"}
	key := types.Caller{TenantID: owner, UserID: "system-1", Role: types.TenantRoleAdmin}

	type tc struct {
		name       string
		caller     types.Caller
		superAdmin bool
		apiKey     bool
		scope      *types.KBScope
		invite     bool
		grant      bool
		required   types.KBPermission
		want       bool
	}
	cases := []tc{
		{"member reads tenant KB", member, false, false, scope(types.KBVisibilityTenant, types.KBMemberDefaultView), false, false, types.KBPermissionViewer, true},
		{"member reads own published KB", member, false, false, scope(types.KBVisibilityPublished, ""), false, false, types.KBPermissionViewer, true},
		{"member without ACL denied on restricted KB", member, false, false, scope(types.KBVisibilityRestricted, ""), false, false, types.KBPermissionViewer, false},
		{"member with live invite reads restricted KB", member, false, false, scope(types.KBVisibilityRestricted, ""), true, false, types.KBPermissionViewer, true},
		{"invite never confers writes", member, false, false, scope(types.KBVisibilityRestricted, ""), true, false, types.KBPermissionEditor, false},
		{"owner admin reads restricted KB", admin, false, false, scope(types.KBVisibilityRestricted, ""), false, false, types.KBPermissionViewer, true},
		{"superadmin reads restricted KB of any tenant", outsider, true, false, scope(types.KBVisibilityRestricted, ""), false, false, types.KBPermissionViewer, true},
		{"outsider denied on tenant KB", outsider, false, false, scope(types.KBVisibilityTenant, ""), false, false, types.KBPermissionViewer, false},
		{"outsider with tenant grant reads restricted KB", outsider, false, false, scope(types.KBVisibilityRestricted, ""), false, true, types.KBPermissionViewer, true},
		{"outsider with invite reads tenant KB", outsider, false, false, scope(types.KBVisibilityTenant, ""), true, false, types.KBPermissionViewer, true},
		{"outsider reads published KB", outsider, false, false, scope(types.KBVisibilityPublished, ""), false, false, types.KBPermissionViewer, true},
		{"outsider reads platform public KB", outsider, false, false, scope(types.KBVisibilityPublic, ""), false, false, types.KBPermissionViewer, true},
		{"guest reads published KB", guest, false, false, scope(types.KBVisibilityPublished, ""), false, false, types.KBPermissionViewer, true},
		{"guest gets no tenant grant", guest, false, false, scope(types.KBVisibilityRestricted, ""), false, true, types.KBPermissionViewer, false},
		{"api key of owner tenant keeps own-tenant reads on restricted KB", key, false, true, scope(types.KBVisibilityRestricted, ""), false, false, types.KBPermissionViewer, true},
		{"api key never reads published KB of another tenant", types.Caller{TenantID: other, UserID: "system-2", Role: types.TenantRoleAdmin}, false, true, scope(types.KBVisibilityPublished, ""), false, false, types.KBPermissionViewer, false},
		{"owner reads never mint writes without a grant", admin, false, false, scope(types.KBVisibilityTenant, ""), false, false, types.KBPermissionEditor, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ctx := types.WithCaller(context.Background(), tt.caller)
			if tt.superAdmin {
				ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
			}
			if tt.apiKey {
				ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 1, FullAccess: true})
			}
			lookup := &grantLookup{scope: tt.scope, grants: map[string]types.KBPermission{}}
			if tt.grant {
				lookup.grants["kb/"+itoa(tt.caller.TenantID)] = types.KBPermissionViewer
			}
			invites := &stubInvites{accepted: map[string]bool{}}
			if tt.invite {
				invites.accepted["kb\x00"+tt.caller.UserID] = true
			}
			got, err := NewKBPermissions(ctx, lookup).WithInvites(invites).Check("kb", tt.scope.TenantID, tt.required)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestKBPermissionsCheckFailsClosedWhenScopeUnknown(t *testing.T) {
	member := types.Caller{TenantID: 1, UserID: "m", Role: types.TenantRoleMember}
	admin := types.Caller{TenantID: 1, UserID: "a", Role: types.TenantRoleAdmin}
	for _, lookup := range []KBGrantLookup{&grantLookup{}, errScopeLookup{&grantLookup{}}} {
		ok, err := NewKBPermissions(types.WithCaller(context.Background(), member), lookup).
			Check("kb", 1, types.KBPermissionViewer)
		require.NoError(t, err)
		require.False(t, ok, "an unloadable scope is treated as restricted for members")
		ok, err = NewKBPermissions(types.WithCaller(context.Background(), admin), lookup).
			Check("kb", 1, types.KBPermissionViewer)
		require.NoError(t, err)
		require.True(t, ok, "owner-tenant admins keep reads")
	}
}

func TestKBPermissionsUsesLookupInvitesAndSeesRevocation(t *testing.T) {
	member := types.Caller{TenantID: 1, UserID: "m", Role: types.TenantRoleMember}
	lookup := &inviteGrantLookup{
		grantLookup: grantLookup{scope: &types.KBScope{TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityRestricted}},
		stubInvites: stubInvites{accepted: map[string]bool{"kb\x00m": true}},
	}
	ctx := types.WithCaller(context.Background(), member)
	ok, err := NewKBPermissions(ctx, lookup).Check("kb", 1, types.KBPermissionViewer)
	require.NoError(t, err)
	require.True(t, ok, "a lookup that also resolves invites is used as the invite ACL source")
	delete(lookup.accepted, "kb\x00m")
	ok, err = NewKBPermissions(ctx, lookup).Check("kb", 1, types.KBPermissionViewer)
	require.NoError(t, err)
	require.False(t, ok, "a revoked invite stops reads on the next request")
}

type inviteGrantLookup struct {
	grantLookup
	stubInvites
}
