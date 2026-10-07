package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type fakeGrantRepo struct {
	interfaces.KBAccessGrantRepository
	rows map[string]*types.KBAccessGrant
}

func newFakeGrantRepo() *fakeGrantRepo {
	return &fakeGrantRepo{rows: map[string]*types.KBAccessGrant{}}
}

func (r *fakeGrantRepo) Create(_ context.Context, g *types.KBAccessGrant) error {
	r.rows[g.ID] = g
	return nil
}

func (r *fakeGrantRepo) GetByID(_ context.Context, id string) (*types.KBAccessGrant, error) {
	return r.rows[id], nil
}

func (r *fakeGrantRepo) GetLiveByPair(_ context.Context, kbID string, grantee uint64) (*types.KBAccessGrant, error) {
	for _, g := range r.rows {
		if g.KBID == kbID && g.GranteeTenantID == grantee &&
			(g.Status == types.GrantStatusPending || g.IsLive()) {
			return g, nil
		}
	}
	return nil, nil
}

func (r *fakeGrantRepo) ListByOwnerTenant(_ context.Context, owner uint64, statuses []types.GrantStatus) ([]*types.KBAccessGrant, error) {
	var out []*types.KBAccessGrant
	for _, g := range r.rows {
		if g.OwnerTenantID != owner {
			continue
		}
		if len(statuses) == 0 {
			out = append(out, g)
			continue
		}
		for _, st := range statuses {
			if g.Status == st {
				out = append(out, g)
			}
		}
	}
	return out, nil
}

func (r *fakeGrantRepo) UpdateStatus(_ context.Context, g *types.KBAccessGrant) error {
	r.rows[g.ID] = g
	return nil
}

type fakeGrantTenants struct {
	interfaces.TenantRepository
}

func (fakeGrantTenants) GetTenantByID(_ context.Context, id uint64) (*types.Tenant, error) {
	if id >= 1 && id <= 3 {
		return &types.Tenant{ID: id}, nil
	}
	return nil, errors.New("not found")
}

func (fakeGrantTenants) GetTenantsByIDs(context.Context, []uint64) (map[uint64]*types.Tenant, error) {
	return nil, nil
}

// scopedKBRepo answers GetKBScopeByID from the fake rows so grant checks see
// the KB's current owner.
type scopedKBRepo struct{ *fakeKBRepo }

func (r scopedKBRepo) GetKBScopeByID(_ context.Context, id string) (*types.KBScope, error) {
	kb := r.rows[id]
	if kb == nil {
		return nil, nil
	}
	return &types.KBScope{TenantID: kb.TenantID, OwnerTenantID: kb.OwnerTenantID, Visibility: kb.Visibility}, nil
}

func grantFixture() (*kbAccessGrantService, *fakeGrantRepo, *fakeKBRepo) {
	kbs := newFakeKBRepo()
	kbs.rows["kb-tinh"] = &types.KnowledgeBase{ID: "kb-tinh", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityTenant}
	kbs.rows["kb-luat"] = &types.KnowledgeBase{ID: "kb-luat", TenantID: 9, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic}
	grants := newFakeGrantRepo()
	svc := NewKBAccessGrantService(grants, scopedKBRepo{kbs}, fakeGrantTenants{}, nil).(*kbAccessGrantService)
	return svc, grants, kbs
}

func humanCtx(caller types.Caller, superAdmin bool) context.Context {
	ctx := types.WithCaller(context.Background(), caller)
	if superAdmin {
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
	}
	return ctx
}

func TestGrantTenantAccessByOwnerAdmin(t *testing.T) {
	svc, _, _ := grantFixture()
	admin := types.Caller{TenantID: 1, UserID: "admin-tinh", Role: types.TenantRoleAdmin}
	ctx := humanCtx(admin, false)

	g, err := svc.GrantTenantAccess(ctx, admin, "kb-tinh", &types.GrantKBAccessRequest{GranteeTenantID: 2})
	require.NoError(t, err)
	require.Equal(t, types.GrantStatusApproved, g.Status)
	require.Equal(t, types.KBPermissionViewer, g.Permission)

	perm, ok, err := svc.ApprovedKBPermission(ctx, "kb-tinh", 2)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, types.KBPermissionViewer, perm)

	_, err = svc.GrantTenantAccess(ctx, admin, "kb-tinh", &types.GrantKBAccessRequest{GranteeTenantID: 2})
	require.ErrorIs(t, err, ErrGrantExists)

	_, err = svc.GrantTenantAccess(ctx, admin, "kb-tinh", &types.GrantKBAccessRequest{GranteeTenantID: 1})
	require.ErrorIs(t, err, ErrGrantSelfTarget)
	_, err = svc.GrantTenantAccess(ctx, admin, "kb-tinh", &types.GrantKBAccessRequest{GranteeTenantID: 42})
	require.ErrorIs(t, err, ErrGrantTenantNotFound)
	past := time.Now().Add(-time.Hour)
	_, err = svc.GrantTenantAccess(ctx, admin, "kb-tinh", &types.GrantKBAccessRequest{GranteeTenantID: 3, ExpiresAt: &past})
	require.Error(t, err)

	revoked, err := svc.Revoke(ctx, admin, g.ID)
	require.NoError(t, err)
	require.Equal(t, types.GrantStatusRevoked, revoked.Status)
	_, ok, _ = svc.ApprovedKBPermission(ctx, "kb-tinh", 2)
	require.False(t, ok, "revoked grant must stop authorizing")
	_, err = svc.Revoke(ctx, admin, g.ID)
	require.ErrorIs(t, err, ErrGrantNotPending)
}

func TestGrantTenantAccessRejectsNonManagers(t *testing.T) {
	svc, _, _ := grantFixture()
	req := &types.GrantKBAccessRequest{GranteeTenantID: 2}
	for name, caller := range map[string]types.Caller{
		"owner-tenant member":      {TenantID: 1, UserID: "m1", Role: types.TenantRoleMember},
		"admin of another tenant":  {TenantID: 2, UserID: "admin-sob", Role: types.TenantRoleAdmin},
		"synthetic API-key caller": {TenantID: 1, UserID: "system-1", Role: types.TenantRoleAdmin},
	} {
		_, err := svc.GrantTenantAccess(humanCtx(caller, false), caller, "kb-tinh", req)
		require.ErrorIs(t, err, ErrGrantNotManager, name)
	}
}

func TestGrantTenantAccessBySuperAdminOnAnyKB(t *testing.T) {
	svc, _, _ := grantFixture()
	super := types.Caller{TenantID: 3, UserID: "super", Role: types.TenantRoleMember}
	g, err := svc.GrantTenantAccess(humanCtx(super, true), super, "kb-tinh", &types.GrantKBAccessRequest{GranteeTenantID: 2})
	require.NoError(t, err)
	require.Equal(t, uint64(1), g.OwnerTenantID)

	_, err = svc.GrantTenantAccess(humanCtx(super, true), super, "kb-luat", &types.GrantKBAccessRequest{GranteeTenantID: 2})
	require.ErrorIs(t, err, ErrGrantPlatformKB)
}

func TestApprovedKBPermissionDropsGrantAfterOwnershipChange(t *testing.T) {
	svc, _, kbs := grantFixture()
	admin := types.Caller{TenantID: 1, UserID: "admin-tinh", Role: types.TenantRoleAdmin}
	_, err := svc.GrantTenantAccess(humanCtx(admin, false), admin, "kb-tinh", &types.GrantKBAccessRequest{GranteeTenantID: 2})
	require.NoError(t, err)
	kbs.rows["kb-tinh"].OwnerTenantID = 3
	_, ok, _ := svc.ApprovedKBPermission(context.Background(), "kb-tinh", 2)
	require.False(t, ok)
}

func TestGrantTenantAccessReplacesExpiredGrant(t *testing.T) {
	svc, grants, _ := grantFixture()
	admin := types.Caller{TenantID: 1, UserID: "admin-tinh", Role: types.TenantRoleAdmin}
	past := time.Now().Add(-time.Hour)
	grants.rows["old"] = &types.KBAccessGrant{ID: "old", KBID: "kb-tinh", OwnerTenantID: 1, GranteeTenantID: 2,
		Permission: types.KBPermissionViewer, Status: types.GrantStatusApproved, ExpiresAt: &past}
	_, err := svc.GrantTenantAccess(humanCtx(admin, false), admin, "kb-tinh", &types.GrantKBAccessRequest{GranteeTenantID: 2})
	require.NoError(t, err)
	require.Equal(t, types.GrantStatusExpired, grants.rows["old"].Status)
}
