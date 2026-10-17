package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// fakeMemberService is a hand-rolled stand-in for
// interfaces.TenantMemberService. It backs Get/HasAnyMembers/AddMember
// with two in-memory maps and lets each test seed exactly the rows it
// cares about. Other interface methods are stubbed because resolveTenantRole
// never touches them.
type fakeMemberService struct {
	members map[string]*types.TenantMember // key = userID + "|" + tenantID
	// addCalls records every AddMember invocation so tests can assert
	// that resolveTenantRole did (or didn't) attempt to auto-promote.
	addCalls []struct {
		UserID   string
		TenantID uint64
		Role     types.TenantRole
	}
	failGet    error
	failHasAny error
	failAdd    error
	// 阻止 auto-promote 把 hasAny 翻面：默认 AddMember 成功也会写入 members map。
}

func newFakeMemberService() *fakeMemberService {
	return &fakeMemberService{members: map[string]*types.TenantMember{}}
}

func memberKey(u string, t uint64) string {
	return u + "|" + uintToStr(t)
}

func uintToStr(t uint64) string {
	// 简单数字转字符串，避免引入额外依赖。
	if t == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for t > 0 {
		i--
		buf[i] = byte('0' + t%10)
		t /= 10
	}
	return string(buf[i:])
}

func (f *fakeMemberService) seedActive(userID string, tenantID uint64, role types.TenantRole) {
	f.members[memberKey(userID, tenantID)] = &types.TenantMember{
		UserID:   userID,
		TenantID: tenantID,
		Role:     role,
		Status:   types.TenantMemberStatusActive,
	}
}

func (f *fakeMemberService) AddMember(
	ctx context.Context, userID string, tenantID uint64, role types.TenantRole, invitedBy *string,
) (*types.TenantMember, error) {
	f.addCalls = append(f.addCalls, struct {
		UserID   string
		TenantID uint64
		Role     types.TenantRole
	}{userID, tenantID, role})
	if f.failAdd != nil {
		return nil, f.failAdd
	}
	m := &types.TenantMember{UserID: userID, TenantID: tenantID, Role: role, Status: types.TenantMemberStatusActive}
	f.members[memberKey(userID, tenantID)] = m
	return m, nil
}

func (f *fakeMemberService) EnsureAdmin(
	ctx context.Context, userID string, tenantID uint64,
) (*types.TenantMember, error) {
	if existing, ok := f.members[memberKey(userID, tenantID)]; ok {
		return existing, nil
	}
	return f.AddMember(ctx, userID, tenantID, types.TenantRoleOwner, nil)
}

func (f *fakeMemberService) GetMembership(
	ctx context.Context, userID string, tenantID uint64,
) (*types.TenantMember, error) {
	if f.failGet != nil {
		return nil, f.failGet
	}
	m, ok := f.members[memberKey(userID, tenantID)]
	if !ok {
		return nil, nil
	}
	cp := *m
	return &cp, nil
}

func (f *fakeMemberService) ListByUser(ctx context.Context, userID string) ([]*types.TenantMember, error) {
	var out []*types.TenantMember
	for _, member := range f.members {
		if member.UserID == userID && member.Status == types.TenantMemberStatusActive {
			copy := *member
			out = append(out, &copy)
		}
	}
	return out, nil
}
func (f *fakeMemberService) ListByTenant(ctx context.Context, tenantID uint64) ([]*types.TenantMember, error) {
	return nil, nil
}
func (f *fakeMemberService) ListMembersPage(
	ctx context.Context, tenantID uint64, query string, page, pageSize int,
) ([]*types.TenantMember, int64, error) {
	return nil, 0, nil
}
func (f *fakeMemberService) HasAnyMembers(ctx context.Context, tenantID uint64) (bool, error) {
	if f.failHasAny != nil {
		return false, f.failHasAny
	}
	for _, m := range f.members {
		if m.TenantID == tenantID && m.Status == types.TenantMemberStatusActive {
			return true, nil
		}
	}
	return false, nil
}
func (f *fakeMemberService) UpdateRole(
	ctx context.Context, userID string, tenantID uint64, newRole types.TenantRole,
) error {
	return nil
}
func (f *fakeMemberService) RemoveMember(ctx context.Context, userID string, tenantID uint64) error {
	return nil
}

var _ interfaces.TenantMemberService = (*fakeMemberService)(nil)

func cfgWithRBAC(enabled bool) *config.Config {
	return &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}
}

func TestResolveTenantRole_ActiveMembershipWins(t *testing.T) {
	svc := newFakeMemberService()
	svc.seedActive("u1", 10, types.TenantRoleMember)

	got, ok := resolveTenantRole(context.Background(), svc,
		&types.User{ID: "u1", TenantID: 10}, 10, false, cfgWithRBAC(true))
	if !ok || got != types.TenantRoleMember {
		t.Fatalf("got (%v, %v), want (contributor, true)", got, ok)
	}
	if len(svc.addCalls) != 0 {
		t.Fatalf("must not auto-promote when membership exists, got %d AddMember calls", len(svc.addCalls))
	}
}

func TestResolveTenantRole_SuperAdminSwitchGetsNoRole_NoAutoPromote(t *testing.T) {
	// A SuperAdmin selecting a foreign tenant chooses a scope; it gets no
	// tenant role from that and never writes tenant_members.
	svc := newFakeMemberService()
	user := &types.User{ID: "super", TenantID: 1, IsSystemAdmin: true, CanAccessAllTenants: true}

	got, ok := resolveTenantRole(context.Background(), svc, user, 99, true, cfgWithRBAC(true))
	if ok {
		t.Fatalf("got (%v, %v), want no role", got, ok)
	}
	if len(svc.addCalls) != 0 {
		t.Fatalf("cross-tenant switch must not trigger auto-promote, got %+v", svc.addCalls)
	}
}

func TestResolveTenantRole_OrphanForeignTenantNotClaimed(t *testing.T) {
	// An orphan tenant that is not the caller's home is never claimed.
	svc := newFakeMemberService() // 空 — 任何空间都是孤儿
	user := &types.User{ID: "u1", TenantID: 1, CanAccessAllTenants: true}

	got, ok := resolveTenantRole(context.Background(), svc, user, 42, true, cfgWithRBAC(true))
	if ok {
		t.Fatalf("got (%v, %v), want no role", got, ok)
	}
	if len(svc.addCalls) != 0 {
		t.Fatalf("auto-promote must skip cross-tenant target, got %+v", svc.addCalls)
	}
}

func TestResolveTenantRole_AutoPromoteSkippedIfTenantHasMembers(t *testing.T) {
	svc := newFakeMemberService()
	// 同一 home tenant 已经有其它成员 — 不应自动晋升新登录者。
	svc.seedActive("other", 7, types.TenantRoleOwner)
	user := &types.User{ID: "u1", TenantID: 7}

	got, ok := resolveTenantRole(context.Background(), svc, user, 7, false, cfgWithRBAC(true))
	if ok {
		t.Fatalf("RBAC enabled + no membership for u1 should be rejected, got role=%v", got)
	}
	if len(svc.addCalls) != 0 {
		t.Fatalf("must not auto-promote into a tenant that already has members, got %+v", svc.addCalls)
	}
}

func TestResolveTenantRole_NoFailOpenWhenRBACDisabled(t *testing.T) {
	// enable_rbac=false relaxes role gates only; it never invents a role.
	svc := newFakeMemberService()
	user := &types.User{ID: "u1", TenantID: 7}
	got, ok := resolveTenantRole(context.Background(), svc, user, 8, false, cfgWithRBAC(false))
	if ok {
		t.Fatalf("EnableRBAC=false must not fail open, got (%v, %v)", got, ok)
	}
}

func TestResolveTenantRole_FailClosedWhenRBACEnabled(t *testing.T) {
	svc := newFakeMemberService()
	// 已有其它成员，自动晋升路径关闭；RBAC 启用 → 必须 403。
	svc.seedActive("other", 8, types.TenantRoleOwner)
	user := &types.User{ID: "u1", TenantID: 7}
	if _, ok := resolveTenantRole(context.Background(), svc, user, 8, false, cfgWithRBAC(true)); ok {
		t.Fatalf("EnableRBAC=true + no membership should be rejected")
	}
}

func TestResolveTenantRole_LookupErrorFailsClosedWhenRBACDisabled(t *testing.T) {
	// A membership lookup error grants nothing, whatever enable_rbac says.
	svc := newFakeMemberService()
	svc.failGet = errors.New("transient db failure")
	user := &types.User{ID: "u1", TenantID: 7}

	got, ok := resolveTenantRole(context.Background(), svc, user, 8, false, cfgWithRBAC(false))
	if ok {
		t.Fatalf("lookup error must fail closed, got (%v, %v)", got, ok)
	}
	if len(svc.addCalls) != 0 {
		t.Fatalf("must not AddMember, got %+v", svc.addCalls)
	}
}

func TestResolveTenantRole_DemotedUserCannotReclaimViaOrphan(t *testing.T) {
	// 边界场景：管理员人为软删全部成员后，被踢出的用户登录自己 home tenant 时
	// 不能因 HasAnyMembers=false 而自动重新拿到 Admin（孤儿自愈已移除）。
	svc := newFakeMemberService()
	user := &types.User{ID: "demoted", TenantID: 5}
	got, ok := resolveTenantRole(context.Background(), svc, user, 5, false, cfgWithRBAC(true))
	if ok {
		t.Fatalf("orphan home tenant must not be reclaimed, got (%v, %v)", got, ok)
	}
	if len(svc.addCalls) != 0 {
		t.Fatalf("must not AddMember, got %+v", svc.addCalls)
	}
}

// ---------- Phase 0 regression: identity must not come from legacy flags ----------

func TestResolveTenantRole_LegacyCrossTenantFlagGrantsNothing(t *testing.T) {
	// F1: CanAccessAllTenants on a non-SuperAdmin must not turn a JWT or
	// X-Tenant-ID that names a foreign tenant into Admin of that tenant.
	svc := newFakeMemberService()
	svc.seedActive("someone-else", 99, types.TenantRoleAdmin)
	user := &types.User{ID: "legacy", TenantID: 1, CanAccessAllTenants: true}

	got, ok := resolveTenantRole(context.Background(), svc, user, 99, true, cfgWithRBAC(true))
	if ok {
		t.Fatalf("legacy cross-tenant flag must not grant a role, got (%v, %v)", got, ok)
	}
	if len(svc.addCalls) != 0 {
		t.Fatalf("must not write memberships, got %+v", svc.addCalls)
	}
}

func TestResolveTenantRole_SuspendedMembershipRejectedWhenRBACDisabled(t *testing.T) {
	// F2: turning RBAC enforcement off must not turn a suspended
	// membership into Admin.
	svc := newFakeMemberService()
	svc.seedActive("other", 8, types.TenantRoleAdmin)
	svc.members[memberKey("u1", 8)] = &types.TenantMember{
		UserID: "u1", TenantID: 8, Role: types.TenantRoleMember,
		Status: types.TenantMemberStatusSuspended,
	}
	user := &types.User{ID: "u1", TenantID: 7}

	got, ok := resolveTenantRole(context.Background(), svc, user, 8, false, cfgWithRBAC(false))
	if ok {
		t.Fatalf("suspended membership must be rejected even with RBAC off, got (%v, %v)", got, ok)
	}
}

func TestResolveTenantRole_EmptyHomeTenantIsNotSelfClaimed(t *testing.T) {
	// R3: a home tenant with no active members must not auto-promote the
	// caller; SuperAdmins assign the first Admin explicitly.
	svc := newFakeMemberService()
	user := &types.User{ID: "u1", TenantID: 7}

	got, ok := resolveTenantRole(context.Background(), svc, user, 7, false, cfgWithRBAC(true))
	if ok {
		t.Fatalf("orphan home tenant must not grant a role, got (%v, %v)", got, ok)
	}
	if len(svc.addCalls) != 0 {
		t.Fatalf("must not AddMember, got %+v", svc.addCalls)
	}
}
