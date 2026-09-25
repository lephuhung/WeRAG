package access

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// grantLookup is an in-memory KBGrantLookup: approved grants keyed by
// "kbID/granteeTenantID". err fails the lookup closed.
type grantLookup struct {
	grants    map[string]types.KBPermission
	err       error
	scope     *types.KBScope
	queriedKB string
	queriedT  uint64
}

func (s *grantLookup) GetKBScope(_ context.Context, _ string) (*types.KBScope, error) {
	return s.scope, nil
}

func (s *grantLookup) ApprovedKBPermission(
	_ context.Context, kbID string, granteeTenantID uint64,
) (types.KBPermission, bool, error) {
	s.queriedKB, s.queriedT = kbID, granteeTenantID
	if s.err != nil {
		return "", false, s.err
	}
	p, ok := s.grants[kbID+"/"+itoa(granteeTenantID)]
	return p, ok, nil
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func TestResolveKBPermissionMatrix(t *testing.T) {
	for _, tt := range []struct {
		name       string
		own        bool
		permission types.KBPermission
		required   types.KBPermission
		grantError bool
		want       types.KBPermission
		wantErr    error
	}{
		{name: "own KB", own: true, required: types.KBPermissionEditor, want: types.KBPermissionAdmin},
		{
			name:       "legacy tenant grant cannot read",
			permission: types.KBPermissionViewer,
			required:   types.KBPermissionViewer,
			wantErr:    ErrForbidden,
		},
		{
			name:       "legacy tenant grant cannot write",
			permission: types.KBPermissionEditor,
			required:   types.KBPermissionEditor,
			wantErr:    ErrForbidden,
		},
		{
			name:       "viewer grant cannot write",
			permission: types.KBPermissionViewer,
			required:   types.KBPermissionEditor,
			wantErr:    ErrForbidden,
		},
		{
			name:       "grant lookup fails closed",
			permission: types.KBPermissionEditor,
			grantError: true,
			required:   types.KBPermissionViewer,
			wantErr:    ErrForbidden,
		},
		{
			name:     "no grant denied",
			required: types.KBPermissionViewer,
			wantErr:  ErrForbidden,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			kb := &types.KnowledgeBase{
				ID: "kb", TenantID: 2,
				OwnerTenantID: 2, Visibility: types.KBVisibilityTenant,
			}
			if tt.own {
				kb.TenantID = 1
				kb.OwnerTenantID = 1
			}
			grants := &grantLookup{grants: map[string]types.KBPermission{}}
			if tt.permission != "" {
				grants.grants["kb/1"] = tt.permission
			}
			if tt.grantError {
				grants.err = errors.New("unavailable")
			}
			request := KBRequest{Caller: types.Caller{TenantID: 1, Role: types.TenantRoleMember}}
			grant, err := ResolveKB(context.Background(), request, kb, tt.required, grants)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, grant)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, grant.Permission)
			require.Equal(t, uint64(1), grant.Caller.TenantID)
			require.Equal(t, kb.TenantID, grant.EffectiveTenantID)
		})
	}
}

func TestResolveKBMissingIdentityResourceAndAPIKeyScope(t *testing.T) {
	kb := &types.KnowledgeBase{
		ID: "kb", TenantID: 1,
		OwnerTenantID: 1, Visibility: types.KBVisibilityTenant,
	}
	_, err := ResolveKB(context.Background(), KBRequest{}, kb, types.KBPermissionViewer, nil)
	require.ErrorIs(t, err, ErrUnauthorized)
	_, err = ResolveKB(
		context.Background(),
		KBRequest{Caller: types.Caller{TenantID: 1}},
		nil,
		types.KBPermissionViewer,
		nil,
	)
	require.ErrorIs(t, err, ErrNotFound)
	ctx := types.WithTenantAPIKeyScope(
		context.Background(),
		types.TenantAPIKeyScope{KnowledgeBaseIDs: types.StringArray{"other"}},
	)
	_, err = ResolveKB(ctx, KBRequest{Caller: types.Caller{TenantID: 1}}, kb, types.KBPermissionViewer, nil)
	require.Error(t, err, "API key scope must apply even to owned KBs")
}

func TestResolveKBVisibilityMatrix(t *testing.T) {
	for _, tt := range []struct {
		name       string
		visibility types.KBVisibility
		kbTenant   uint64
		kbOwner    uint64
		caller     types.Caller
		sysAdmin   bool
		granted    types.KBPermission
		required   types.KBPermission
		want       types.KBPermission
		wantErr    error
	}{
		// --- tenant-owned rows: owning-tenant members keep normal access ---
		{name: "tenant KB member reads in owning tenant",
			visibility: types.KBVisibilityTenant, kbTenant: 1, kbOwner: 1,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleMember},
			required: types.KBPermissionViewer, want: types.KBPermissionAdmin},
		{name: "tenant KB member writes in owning tenant",
			visibility: types.KBVisibilityTenant, kbTenant: 1, kbOwner: 1,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleMember},
			required: types.KBPermissionEditor, want: types.KBPermissionAdmin},
		{name: "tenant KB admin writes in owning tenant",
			visibility: types.KBVisibilityTenant, kbTenant: 1, kbOwner: 1,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleOwner},
			required: types.KBPermissionEditor, want: types.KBPermissionAdmin},
		// --- platform-owned public rows: every authenticated human reads ---
		{name: "platform public KB foreign member reads",
			visibility: types.KBVisibilityPublic, kbTenant: 9, kbOwner: 0,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleMember},
			required: types.KBPermissionViewer, want: types.KBPermissionViewer},
		{name: "platform public KB data scope is the execution tenant",
			visibility: types.KBVisibilityPublic, kbTenant: 9, kbOwner: 0,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleMember},
			required: types.KBPermissionViewer, want: types.KBPermissionViewer},
		{name: "platform public KB tenant admin cannot write",
			visibility: types.KBVisibilityPublic, kbTenant: 9, kbOwner: 0,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleOwner},
			required: types.KBPermissionEditor, wantErr: ErrForbidden},
		{name: "platform public KB system admin read resolves viewer, write stays closed",
			visibility: types.KBVisibilityPublic, kbTenant: 9, kbOwner: 0,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleMember},
			sysAdmin: true,
			required: types.KBPermissionViewer, want: types.KBPermissionViewer},
		// --- tenant-scoped ---
		{name: "tenant KB member gets admin in own tenant",
			visibility: types.KBVisibilityTenant, kbTenant: 1, kbOwner: 1,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleMember},
			required: types.KBPermissionEditor, want: types.KBPermissionAdmin},
		{name: "tenant KB foreign denied without grant",
			visibility: types.KBVisibilityTenant, kbTenant: 2, kbOwner: 2,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleMember},
			required: types.KBPermissionViewer, wantErr: ErrForbidden},
		{name: "legacy approved tenant grant does not authorize foreign read",
			visibility: types.KBVisibilityTenant, kbTenant: 2, kbOwner: 2,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleMember},
			granted:  types.KBPermissionViewer,
			required: types.KBPermissionViewer, wantErr: ErrForbidden},
		{name: "tenant KB foreign viewer grant cannot write",
			visibility: types.KBVisibilityTenant, kbTenant: 2, kbOwner: 2,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleMember},
			granted:  types.KBPermissionViewer,
			required: types.KBPermissionEditor, wantErr: ErrForbidden},
	} {
		t.Run(tt.name, func(t *testing.T) {
			kb := &types.KnowledgeBase{
				ID: "kb", TenantID: tt.kbTenant,
				OwnerTenantID: tt.kbOwner, Visibility: tt.visibility,
			}
			grants := &grantLookup{grants: map[string]types.KBPermission{}}
			if tt.granted != "" {
				grants.grants["kb/1"] = tt.granted
			}
			ctx := context.Background()
			if tt.sysAdmin {
				ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
			}
			grant, err := ResolveKB(ctx,
				KBRequest{Caller: tt.caller}, kb, tt.required, grants)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, grant)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, grant.Permission)
			require.Equal(t, tt.caller.TenantID, grant.Caller.TenantID)
			require.Equal(t, kb.TenantID, grant.EffectiveTenantID)
		})
	}
}
