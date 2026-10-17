package access

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// The decision table for DecideKB, settled 2026-10-17. Owner tenant is 1
// (data scope 1); the platform-public row is owner 0 with data scope 9.
// The "other" tenant is 2.

type decideRole string

const (
	roleSuperAdmin  decideRole = "superadmin"
	roleOwnerAdmin  decideRole = "owner_admin"
	roleOwnerMember decideRole = "owner_member"
	roleOtherMember decideRole = "other_member"
	roleGuest       decideRole = "guest"
	roleAPIKeyOwner decideRole = "api_key_owner_tenant"
	roleAPIKeyOther decideRole = "api_key_other_tenant"
	decideOwner                = uint64(1)
	decideOther                = uint64(2)
)

type decideVis string

const (
	visPublic           decideVis = "public"
	visPublished        decideVis = "published"
	visTenantView       decideVis = "tenant_view"
	visTenantContribute decideVis = "tenant_contribute"
	visRestricted       decideVis = "restricted"
)

type decideACL string

const (
	aclNone            decideACL = "none"
	aclLiveInvite      decideACL = "live_invite"
	aclStaleInvite     decideACL = "stale_owner_invite"
	aclSuspendedInvite decideACL = "invite_suspended_membership"
	aclTenantGrant     decideACL = "tenant_grant"
)

// fakeInvites mimics the repository rule: an accepted invite counts only
// when issued by the KB's current owner and the recipient's membership in
// recipient_tenant_id is active.
type fakeInvites struct {
	kbOwner map[string]uint64
	rows    []fakeInviteRow
	active  map[string]bool // userID + "/" + tenant
	calls   int
	err     error
}

type fakeInviteRow struct {
	kbID, userID     string
	owner, recipient uint64
}

func (f *fakeInvites) HasAcceptedInvite(_ context.Context, kbID, userID string) (bool, error) {
	for _, r := range f.rows {
		if r.kbID == kbID && r.userID == userID && r.owner == f.kbOwner[kbID] {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeInvites) HasLiveInvite(_ context.Context, kbID, userID string) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	for _, r := range f.rows {
		if r.kbID == kbID && r.userID == userID && r.owner == f.kbOwner[kbID] &&
			f.active[fmt.Sprintf("%s/%d", userID, r.recipient)] {
			return true, nil
		}
	}
	return false, nil
}

func decideKB(vis decideVis) *types.KnowledgeBase {
	kb := &types.KnowledgeBase{ID: "kb", TenantID: decideOwner, OwnerTenantID: decideOwner,
		MemberDefault: types.KBMemberDefaultContribute}
	switch vis {
	case visPublic:
		kb.TenantID, kb.OwnerTenantID, kb.Visibility = 9, 0, types.KBVisibilityPublic
	case visPublished:
		kb.Visibility = types.KBVisibilityPublished
	case visTenantView:
		kb.Visibility, kb.MemberDefault = types.KBVisibilityTenant, types.KBMemberDefaultView
	case visTenantContribute:
		kb.Visibility = types.KBVisibilityTenant
	case visRestricted:
		kb.Visibility = types.KBVisibilityRestricted
	}
	return kb
}

func decideCaller(role decideRole) (context.Context, types.Caller) {
	ctx := context.Background()
	var caller types.Caller
	switch role {
	case roleSuperAdmin:
		// A SuperAdmin visiting another tenant with no membership there.
		caller = types.Caller{TenantID: 5, UserID: "super"}
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
	case roleOwnerAdmin:
		caller = types.Caller{TenantID: decideOwner, UserID: "owner-admin", Role: types.TenantRoleAdmin}
	case roleOwnerMember:
		caller = types.Caller{TenantID: decideOwner, UserID: "owner-member", Role: types.TenantRoleMember}
	case roleOtherMember:
		caller = types.Caller{TenantID: decideOther, UserID: "other-member", Role: types.TenantRoleMember}
	case roleGuest:
		caller = types.Caller{UserID: "guest"}
	case roleAPIKeyOwner, roleAPIKeyOther:
		tenant := decideOwner
		if role == roleAPIKeyOther {
			tenant = decideOther
		}
		caller = types.Caller{TenantID: tenant, UserID: fmt.Sprintf("system-%d", tenant), Role: types.TenantRoleMember}
		ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{FullAccess: true})
		ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPITenant, ID: "7"})
	}
	return types.WithCaller(ctx, caller), caller
}

// decideLookups builds the ACL sources for one scenario. The invite or
// grant is always addressed to the caller (user and current tenant).
func decideLookups(caller types.Caller, kb *types.KnowledgeBase, acl decideACL) (KBLookups, *fakeInvites, *grantLookup) {
	invites := &fakeInvites{kbOwner: map[string]uint64{kb.ID: kb.OwnerTenantID}, active: map[string]bool{}}
	grants := &grantLookup{grants: map[string]types.KBPermission{}}
	// Every non-guest human is an active member of its current tenant.
	if caller.HasTenantRole() {
		invites.active[fmt.Sprintf("%s/%d", caller.UserID, caller.TenantID)] = true
	}
	recipientTenant := caller.TenantID
	if recipientTenant == 0 {
		recipientTenant = decideOther // a guest's former tenant
	}
	row := fakeInviteRow{kbID: kb.ID, userID: caller.UserID, owner: kb.OwnerTenantID, recipient: recipientTenant}
	switch acl {
	case aclLiveInvite:
		invites.rows = append(invites.rows, row)
	case aclStaleInvite:
		row.owner = 77 // issued by a previous owner
		invites.rows = append(invites.rows, row)
	case aclSuspendedInvite:
		invites.rows = append(invites.rows, row)
		invites.active[fmt.Sprintf("%s/%d", caller.UserID, recipientTenant)] = false
	case aclTenantGrant:
		grants.grants[fmt.Sprintf("%s/%d", kb.ID, caller.TenantID)] = types.KBPermissionViewer
	}
	return KBLookups{Grants: grants, Invites: invites}, invites, grants
}

type decideWant struct {
	permission types.KBPermission // "" = no access
	download   bool
	source     AccessSource
}

// expectedDecision states the settled rules per scenario.
func expectedDecision(role decideRole, vis decideVis, acl decideACL) decideWant {
	none := decideWant{}
	tenantOwned := vis != visPublic
	open := vis == visPublic || vis == visPublished
	readerSource := SourcePublic
	if vis == visPublished {
		readerSource = SourcePublished
	}
	switch role {
	case roleSuperAdmin:
		return decideWant{types.KBPermissionAdmin, true, SourceSuperAdmin}
	case roleAPIKeyOwner:
		if tenantOwned {
			return decideWant{types.KBPermissionAdmin, true, SourceTenantScope}
		}
		return none
	case roleAPIKeyOther:
		return none // keys never read public/published or ACL rows
	case roleOwnerAdmin:
		if tenantOwned {
			return decideWant{types.KBPermissionAdmin, true, SourceTenantAdmin}
		}
		return decideWant{types.KBPermissionViewer, true, SourcePublic}
	case roleOwnerMember:
		switch vis {
		case visPublic:
			return decideWant{types.KBPermissionViewer, true, SourcePublic}
		case visPublished, visTenantContribute:
			return decideWant{types.KBPermissionEditor, true, SourceTenantScope}
		case visTenantView:
			return decideWant{types.KBPermissionViewer, true, SourceTenantScope}
		case visRestricted:
			if acl == aclLiveInvite {
				return decideWant{types.KBPermissionViewer, false, SourceACLUser}
			}
			return none // a same-tenant grant is meaningless
		}
	case roleOtherMember:
		if open {
			return decideWant{types.KBPermissionViewer, true, readerSource}
		}
		switch acl {
		case aclLiveInvite:
			return decideWant{types.KBPermissionViewer, false, SourceACLUser}
		case aclTenantGrant:
			return decideWant{types.KBPermissionViewer, false, SourceACLTenant}
		}
		return none
	case roleGuest:
		if open {
			return decideWant{types.KBPermissionViewer, true, readerSource}
		}
		return none // a Guest gets no ACL
	}
	return none
}

func actionAllowed(want decideWant, action KBAction) bool {
	if want.permission == "" || !want.permission.HasPermission(action.RequiredPermission()) {
		return false
	}
	return action != ActionDownloadOriginal || want.download
}

func TestDecideKBMatrix(t *testing.T) {
	roles := []decideRole{roleSuperAdmin, roleOwnerAdmin, roleOwnerMember, roleOtherMember, roleGuest,
		roleAPIKeyOwner, roleAPIKeyOther}
	visibilities := []decideVis{visPublic, visPublished, visTenantView, visTenantContribute, visRestricted}
	acls := []decideACL{aclNone, aclLiveInvite, aclStaleInvite, aclSuspendedInvite, aclTenantGrant}
	actions := []KBAction{ActionView, ActionDownloadOriginal, ActionContribute, ActionManage}
	cases := 0
	for _, role := range roles {
		for _, vis := range visibilities {
			for _, acl := range acls {
				for _, action := range actions {
					cases++
					name := fmt.Sprintf("%s/%s/%s/%s", role, vis, acl, action)
					t.Run(name, func(t *testing.T) {
						ctx, caller := decideCaller(role)
						kb := decideKB(vis)
						lookups, invites, _ := decideLookups(caller, kb, acl)
						want := expectedDecision(role, vis, acl)
						got, err := DecideKB(ctx, caller, kb, action, lookups)
						if !actionAllowed(want, action) {
							require.Nil(t, got)
							if role == roleGuest {
								require.ErrorIs(t, err, ErrUnauthorized)
							} else {
								require.ErrorIs(t, err, ErrForbidden)
							}
							if action != ActionView {
								require.Zero(t, invites.calls, "writes and downloads never consult the ACL")
							}
							return
						}
						require.NoError(t, err)
						require.Equal(t, want.permission, got.Permission)
						require.Equal(t, want.download, got.CanDownloadOriginal)
						require.Equal(t, want.source, got.Source)
						require.Equal(t, kb.TenantID, got.EffectiveTenantID)
					})
				}
			}
		}
	}
	require.Equal(t, 700, cases)
}

func TestDecideKBNotFoundAndCallerMismatch(t *testing.T) {
	ctx, caller := decideCaller(roleOwnerAdmin)
	_, err := DecideKB(ctx, caller, nil, ActionView, KBLookups{})
	require.ErrorIs(t, err, ErrNotFound)
	_, err = DecideKB(ctx, caller, &types.KnowledgeBase{}, ActionView, KBLookups{})
	require.ErrorIs(t, err, ErrNotFound)

	other := types.Caller{TenantID: decideOwner, UserID: "someone-else", Role: types.TenantRoleAdmin}
	_, err = DecideKB(ctx, other, decideKB(visTenantContribute), ActionView, KBLookups{})
	require.ErrorIs(t, err, ErrForbidden, "a captured caller cannot be replaced")
}

func TestDecideKBAPIKeyScopeStillApplies(t *testing.T) {
	ctx := types.WithTenantAPIKeyScope(context.Background(), types.TenantAPIKeyScope{
		Capabilities: types.StringArray{string(types.APIKeyCapabilityRetrieve)}, KnowledgeBaseIDs: types.StringArray{"other-kb"},
	})
	caller := types.Caller{TenantID: decideOwner, UserID: "system-1", Role: types.TenantRoleMember}
	ctx = types.WithCaller(ctx, caller)
	_, err := DecideKB(ctx, caller, decideKB(visTenantContribute), ActionView, KBLookups{})
	require.Error(t, err, "a KB outside the key's allow-list is refused before any rule")
}

func TestDecideKBLookupErrorsFailClosed(t *testing.T) {
	ctx, caller := decideCaller(roleOtherMember)
	kb := decideKB(visRestricted)
	invites := &fakeInvites{err: errors.New("db down")}
	grants := &grantLookup{err: errors.New("db down")}
	_, err := DecideKB(ctx, caller, kb, ActionView, KBLookups{Grants: grants, Invites: invites})
	require.ErrorIs(t, err, ErrForbidden)
}

type fakeMembers struct{ active map[string]bool }

func (f fakeMembers) IsActiveMember(_ context.Context, tenantID uint64, userID string) (bool, error) {
	return f.active[fmt.Sprintf("%s/%d", userID, tenantID)], nil
}

func TestDecideKBMembershipRecheck(t *testing.T) {
	kb := decideKB(visTenantContribute)
	for _, role := range []decideRole{roleOwnerAdmin, roleOwnerMember} {
		ctx, caller := decideCaller(role)
		// The request role is stale: the membership was removed since.
		_, err := DecideKB(ctx, caller, kb, ActionView, KBLookups{Members: fakeMembers{}})
		require.ErrorIs(t, err, ErrForbidden, string(role))
		live := fakeMembers{active: map[string]bool{fmt.Sprintf("%s/%d", caller.UserID, decideOwner): true}}
		_, err = DecideKB(ctx, caller, kb, ActionView, KBLookups{Members: live})
		require.NoError(t, err, string(role))
	}
}

func TestDecideKBMemberDefaultEmptyMeansContribute(t *testing.T) {
	ctx, caller := decideCaller(roleOwnerMember)
	kb := decideKB(visTenantContribute)
	kb.MemberDefault = "" // rows read before EnsureDefaults
	got, err := DecideKB(ctx, caller, kb, ActionContribute, KBLookups{})
	require.NoError(t, err)
	require.Equal(t, types.KBPermissionEditor, got.Permission)
}

func TestWrappersCarryDecisionSource(t *testing.T) {
	ctx, caller := decideCaller(roleOtherMember)
	kb := decideKB(visRestricted)
	lookups, _, _ := decideLookups(caller, kb, aclLiveInvite)
	grant, err := ResolveKBWithInvite(ctx, KBRequest{Caller: caller}, kb, types.KBPermissionViewer,
		lookups.Grants, lookups.Invites)
	require.NoError(t, err)
	require.Equal(t, SourceACLUser, grant.Source)
	require.False(t, grant.CanDownloadOriginal)
	_, err = ResolveKBForDownload(ctx, KBRequest{Caller: caller}, kb)
	require.ErrorIs(t, err, ErrForbidden, "an invite never downloads originals")
	_, err = ResolveKB(ctx, KBRequest{Caller: caller}, kb, types.KBPermissionViewer, lookups.Grants)
	require.ErrorIs(t, err, ErrForbidden, "ResolveKB alone does not consult invites")

	ctx, caller = decideCaller(roleOwnerMember)
	kb = decideKB(visTenantView)
	_, err = ResolveKBForManage(ctx, KBRequest{Caller: caller}, kb)
	require.ErrorIs(t, err, ErrForbidden, "member_default=view refuses member uploads")
	kb.MemberDefault = types.KBMemberDefaultContribute
	grant, err = ResolveKBForManage(ctx, KBRequest{Caller: caller}, kb)
	require.NoError(t, err)
	require.Equal(t, SourceTenantScope, grant.Source)
	require.NoError(t, RequireKBManage(grant.Context(ctx), kb), "the contribute grant satisfies the write check")
	_, err = ResolveKB(ctx, KBRequest{Caller: caller}, kb, types.KBPermissionAdmin, nil)
	require.ErrorIs(t, err, ErrForbidden, "a member never manages the KB")
}
