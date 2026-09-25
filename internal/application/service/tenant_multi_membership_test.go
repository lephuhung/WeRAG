package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// Regression: accepting a tenant-B invitation must create a second,
// independent membership while preserving the tenant-A row, its role,
// and without touching any other state. Tenant join invitations and KB
// read invitations are distinct flows — this test pins the tenant side.
func TestTenantInviteAccept_PreservesMultiTenantMemberships(t *testing.T) {
	ctx := context.Background()
	svc, invRepo, memberSvc := newInvitationSvc()

	// Existing membership: user is an Admin of tenant A.
	if _, err := memberSvc.AddMember(ctx, "u-multi", 100, types.TenantRoleAdmin, nil); err != nil {
		t.Fatalf("seed tenant-A membership: %v", err)
	}

	// Tenant-B admin invites the same user as a Member.
	inv, err := svc.Create(ctx, 200, "u-multi", types.TenantRoleMember, nil, "admin-b")
	if err != nil {
		t.Fatalf("create tenant-B invite: %v", err)
	}
	_ = invRepo

	// Accept as the invitee.
	got, err := svc.Accept(ctx, inv.ID, "u-multi")
	if err != nil {
		t.Fatalf("accept tenant-B invite: %v", err)
	}
	if got.TenantID != 200 || got.Role != types.TenantRoleMember {
		t.Errorf("new membership = tenant %d role %q, want tenant 200 role member", got.TenantID, got.Role)
	}

	// Tenant-A membership survives with its independent role.
	a, err := memberSvc.GetMembership(ctx, "u-multi", 100)
	if err != nil || a == nil {
		t.Fatalf("tenant-A membership must survive: %v", err)
	}
	if a.Role != types.TenantRoleAdmin || a.Status != types.TenantMemberStatusActive {
		t.Errorf("tenant-A membership mutated: role=%q status=%q", a.Role, a.Status)
	}
	b, err := memberSvc.GetMembership(ctx, "u-multi", 200)
	if err != nil || b == nil {
		t.Fatalf("tenant-B membership must exist: %v", err)
	}
	if b.Role != types.TenantRoleMember {
		t.Errorf("tenant-B role = %q, want member (independent of tenant-A admin)", b.Role)
	}
}

// Accepting a KB read invitation must NOT create or alter any tenant
// membership: the two invitation flows share no state.
func TestKBInviteAccept_DoesNotTouchTenantMemberships(t *testing.T) {
	ctx := context.Background()
	_, _, memberSvc := newInvitationSvc()
	if _, err := memberSvc.AddMember(ctx, "u-kb", 100, types.TenantRoleMember, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}

	inviteRepo := newFakeKBInviteRepo()
	kbRepo := &fakeKBRepoForInvite{kb: &types.KnowledgeBase{ID: "kb-9", TenantID: 100, OwnerTenantID: 100, Visibility: types.KBVisibilityTenant, Name: "KB"}}
	members := &fakeMemberSvcForInvite{active: map[string]bool{"u-kb": true}}
	kbSvc := NewKBInvitationService(inviteRepo, kbRepo, members, nil, nil)

	admin := types.Caller{TenantID: 100, UserID: "admin-1", Role: types.TenantRoleAdmin}
	created, err := kbSvc.Issue(ctx, admin, "kb-9", &types.CreateKBInviteRequest{
		RecipientUserID: "u-kb", RecipientTenantID: 200,
	})
	if err != nil {
		t.Fatalf("issue KB invite: %v", err)
	}
	recipient := types.Caller{TenantID: 200, UserID: "u-kb", Role: types.TenantRoleMember}
	if _, err := kbSvc.Accept(ctx, recipient, created.Token); err != nil {
		t.Fatalf("accept KB invite: %v", err)
	}

	// Tenant memberships unchanged: still exactly the tenant-100 row.
	m, err := memberSvc.GetMembership(ctx, "u-kb", 100)
	if err != nil || m == nil || m.Role != types.TenantRoleMember {
		t.Errorf("tenant-100 membership must be untouched: %+v %v", m, err)
	}
	if other, _ := memberSvc.GetMembership(ctx, "u-kb", 200); other != nil {
		t.Error("KB invite accept must NOT create a tenant-200 membership")
	}
}
