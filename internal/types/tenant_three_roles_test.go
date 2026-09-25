package types

import "testing"

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

func TestRoleAbsenceDefaultsToMember(t *testing.T) {
	var c Caller
	if got := c.Normalize().Role; got != TenantRoleMember {
		t.Errorf("zero Caller.Normalize().Role = %q, want member", got)
	}
}
