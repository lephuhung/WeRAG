package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// Public visibility is retired: creation and visibility updates must fail
// for every caller, including Tenant Admins and system admins.
func TestKnowledgeBasePublicCreationDenied(t *testing.T) {
	svc := &knowledgeBaseService{}
	mkCtx := func(role types.TenantRole, sys bool) context.Context {
		ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "u1", Role: role})
		if sys {
			ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
		}
		return ctx
	}
	for _, tc := range []struct {
		name string
		role types.TenantRole
		sys  bool
	}{
		{"member", types.TenantRoleMember, false},
		{"admin", types.TenantRoleAdmin, false},
		{"legacy owner", types.TenantRoleOwner, false},
		{"system admin", types.TenantRoleMember, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kb := &types.KnowledgeBase{Name: "x", Visibility: types.KBVisibilityPublic}
			if err := svc.validateKBVisibility(mkCtx(tc.role, tc.sys), kb); err == nil {
				t.Fatal("public KB creation must be denied")
			}
			tenant := &types.KnowledgeBase{Name: "x", Visibility: types.KBVisibilityTenant}
			if err := svc.validateKBVisibility(mkCtx(tc.role, tc.sys), tenant); err != nil {
				t.Fatalf("tenant visibility must pass, got %v", err)
			}
		})
	}
}

// Scope changes (Task 3) require an explicit human SuperAdmin — for
// promotion and narrowing alike. Tenant Admins cannot promote a KB to
// public, and narrowing to tenant is no longer a Tenant Admin decision.
func TestSetKnowledgeBaseVisibilityPublicDenied(t *testing.T) {
	repo := newFakeKBRepo()
	repo.rows["kb-1"] = &types.KnowledgeBase{ID: "kb-1", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityTenant}
	svc := &knowledgeBaseService{repo: repo}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "admin", Role: types.TenantRoleAdmin})
	if _, err := svc.SetKnowledgeBaseVisibility(ctx, "kb-1", types.KBVisibilityPublic, 0); err == nil {
		t.Fatal("widening to public must be denied for a tenant admin")
	}
	// Narrowing to tenant is a privileged scope decision too: a tenant
	// admin without explicit SuperAdmin authority is denied.
	if _, err := svc.SetKnowledgeBaseVisibility(ctx, "kb-1", types.KBVisibilityTenant, 0); err == nil {
		t.Fatal("scope change must be denied without explicit human SuperAdmin authority")
	}
	// An explicit human SuperAdmin may keep tenant scope (no-op).
	sysCtx := context.WithValue(ctx, types.SystemAdminContextKey, true)
	if _, err := svc.SetKnowledgeBaseVisibility(sysCtx, "kb-1", types.KBVisibilityTenant, 0); err != nil {
		t.Fatalf("explicit human SuperAdmin no-op must succeed, got %v", err)
	}
}

// Tenant-wide grants are retired: request/review/revoke entry points must
// reject so no tenant-wide grant can ever authorize access again.
func TestKBGrantMutationsDisabled(t *testing.T) {
	svc := NewKBAccessGrantService(nil, nil, nil, nil)
	caller := types.Caller{TenantID: 2, UserID: "u1", Role: types.TenantRoleAdmin}
	if _, err := svc.RequestAccess(context.Background(), caller, "kb-1", nil); !errors.Is(err, ErrGrantDisabled) {
		t.Fatalf("RequestAccess must return ErrGrantDisabled, got %v", err)
	}
	ownerCaller := types.Caller{TenantID: 1, UserID: "admin", Role: types.TenantRoleAdmin}
	if _, err := svc.Review(context.Background(), ownerCaller, "g-1", nil); !errors.Is(err, ErrGrantDisabled) {
		t.Fatalf("Review must return ErrGrantDisabled, got %v", err)
	}
	if _, err := svc.Revoke(context.Background(), ownerCaller, "g-1"); !errors.Is(err, ErrGrantDisabled) {
		t.Fatalf("Revoke must return ErrGrantDisabled, got %v", err)
	}
	// Read paths stay available for audit visibility (nil repos would
	// panic, so only assert the mutation surface here).
}
