package types

import (
	"context"
	"testing"
)

func TestHumanTenantRolesOnlyAdminMember(t *testing.T) {
	if !TenantRoleAdmin.IsHumanTenantRole() {
		t.Error("admin must be a human tenant role")
	}
	if !TenantRoleMember.IsHumanTenantRole() {
		t.Error("member must be a human tenant role")
	}
	if TenantRoleOwner.IsHumanTenantRole() {
		t.Error("owner must NOT be a human tenant role (legacy alias only)")
	}
	if TenantRole("contributor").IsHumanTenantRole() {
		t.Error("contributor must NOT be a human tenant role")
	}
	if TenantRole("viewer").IsHumanTenantRole() {
		t.Error("viewer must NOT be a human tenant role")
	}
	if TenantRole("").IsHumanTenantRole() {
		t.Error("empty role must NOT be a human tenant role")
	}
}

func TestTenantAdminThreshold(t *testing.T) {
	if !TenantRoleAdmin.IsTenantAdmin() {
		t.Error("admin must satisfy IsTenantAdmin")
	}
	if !TenantRoleOwner.IsTenantAdmin() {
		t.Error("legacy owner must satisfy IsTenantAdmin for backward compat")
	}
	if TenantRoleMember.IsTenantAdmin() {
		t.Error("member must NOT satisfy IsTenantAdmin")
	}
	if TenantRole("viewer").IsTenantAdmin() {
		t.Error("viewer must NOT satisfy IsTenantAdmin")
	}
}

func TestNormalizeTenantRole(t *testing.T) {
	cases := []struct {
		in   TenantRole
		want TenantRole
	}{
		{TenantRoleAdmin, TenantRoleAdmin},
		{TenantRoleOwner, TenantRoleAdmin},
		{TenantRoleMember, TenantRoleMember},
		{TenantRole("contributor"), TenantRoleMember},
		{TenantRole("viewer"), TenantRoleMember},
		{TenantRole(""), TenantRoleMember},
		{TenantRole("bogus"), TenantRoleMember},
	}
	for _, tc := range cases {
		if got := NormalizeTenantRole(tc.in); got != tc.want {
			t.Errorf("NormalizeTenantRole(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Phase 2: an absent or invalid role means "no tenant role", never Member.
func TestRoleAbsenceMeansNoTenantRole(t *testing.T) {
	var c Caller
	if got := c.Normalize().Role; got != "" {
		t.Errorf("zero Caller.Normalize().Role = %q, want empty", got)
	}
	if c.HasTenantRole() {
		t.Errorf("zero Caller must not have a tenant role")
	}
	if got := (Caller{Role: "bogus"}).Normalize().Role; got != "" {
		t.Errorf("invalid role normalized to %q, want empty", got)
	}
	if !(Caller{Role: TenantRoleMember}).Normalize().HasTenantRole() {
		t.Errorf("member must keep its tenant role")
	}
	if TenantRole("").HasPermission(TenantRoleMember) {
		t.Errorf("empty role must not satisfy Member")
	}
	if got := TenantRoleFromContext(context.Background()); got != "" {
		t.Errorf("TenantRoleFromContext without a role = %q, want empty", got)
	}
	ctx := context.WithValue(context.Background(), TenantRoleContextKey, TenantRole("bogus"))
	if got := TenantRoleFromContext(ctx); got != "" {
		t.Errorf("TenantRoleFromContext with an invalid role = %q, want empty", got)
	}
	ctx = context.WithValue(context.Background(), TenantRoleContextKey, TenantRoleAdmin)
	if got := TenantRoleFromContext(ctx); got != TenantRoleAdmin {
		t.Errorf("TenantRoleFromContext = %q, want admin", got)
	}
	if got := CallerFromContext(context.Background()).Role; got != "" {
		t.Errorf("CallerFromContext without a role = %q, want empty", got)
	}
}

func TestResolveMembershipState(t *testing.T) {
	member := []Membership{{TenantID: 3, Role: TenantRoleMember}}
	if got := ResolveMembershipState(true, nil); got != MembershipStateSuperAdmin {
		t.Errorf("superadmin without membership = %q", got)
	}
	if got := ResolveMembershipState(true, member); got != MembershipStateSuperAdmin {
		t.Errorf("superadmin with membership = %q", got)
	}
	if got := ResolveMembershipState(false, member); got != MembershipStateMember {
		t.Errorf("member = %q", got)
	}
	if got := ResolveMembershipState(false, nil); got != MembershipStateGuest {
		t.Errorf("guest = %q", got)
	}
	if got := ResolveMembershipState(false, []Membership{{TenantID: 0}}); got != MembershipStateGuest {
		t.Errorf("zero-tenant row = %q, want guest", got)
	}
}
