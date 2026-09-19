package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// fakeTenantOrgRepo is an in-memory TenantOrgRepository for visibility tests.
type fakeTenantOrgRepo struct {
	orgs    map[uint64]*types.TenantOrg
	members map[string]*types.TenantOrgMember // "orgID/userID"
}

func newFakeTenantOrgRepo() *fakeTenantOrgRepo {
	return &fakeTenantOrgRepo{
		orgs:    map[uint64]*types.TenantOrg{},
		members: map[string]*types.TenantOrgMember{},
	}
}

func (r *fakeTenantOrgRepo) CreateOrg(_ context.Context, org *types.TenantOrg) error {
	r.orgs[org.ID] = org
	return nil
}
func (r *fakeTenantOrgRepo) GetOrgByID(_ context.Context, id uint64) (*types.TenantOrg, error) {
	return r.orgs[id], nil
}
func (r *fakeTenantOrgRepo) ListOrgsByTenant(_ context.Context, tenantID uint64) ([]*types.TenantOrg, error) {
	var out []*types.TenantOrg
	for _, o := range r.orgs {
		if o.TenantID == tenantID {
			out = append(out, o)
		}
	}
	return out, nil
}
func (r *fakeTenantOrgRepo) UpdateOrg(_ context.Context, org *types.TenantOrg) error {
	r.orgs[org.ID] = org
	return nil
}
func (r *fakeTenantOrgRepo) DeleteOrg(_ context.Context, id uint64) error {
	delete(r.orgs, id)
	return nil
}
func (r *fakeTenantOrgRepo) AddMember(_ context.Context, m *types.TenantOrgMember) error {
	r.members[memberKey(m.OrgID, m.UserID)] = m
	return nil
}
func (r *fakeTenantOrgRepo) RemoveMember(_ context.Context, orgID uint64, userID string) error {
	delete(r.members, memberKey(orgID, userID))
	return nil
}
func (r *fakeTenantOrgRepo) UpdateMemberRole(_ context.Context, orgID uint64, userID string, role types.TenantOrgRole) error {
	if m, ok := r.members[memberKey(orgID, userID)]; ok {
		m.Role = role
	}
	return nil
}
func (r *fakeTenantOrgRepo) GetMember(_ context.Context, orgID uint64, userID string) (*types.TenantOrgMember, error) {
	return r.members[memberKey(orgID, userID)], nil
}
func (r *fakeTenantOrgRepo) ListMembers(_ context.Context, orgID uint64) ([]*types.TenantOrgMember, error) {
	var out []*types.TenantOrgMember
	for _, m := range r.members {
		if m.OrgID == orgID {
			out = append(out, m)
		}
	}
	return out, nil
}
func (r *fakeTenantOrgRepo) ListOrgIDsForUser(_ context.Context, _ uint64, userID string) ([]uint64, error) {
	var out []uint64
	for _, m := range r.members {
		if m.UserID == userID {
			out = append(out, m.OrgID)
		}
	}
	return out, nil
}
func (r *fakeTenantOrgRepo) CountMembers(_ context.Context, orgID uint64) (int64, error) {
	var n int64
	for _, m := range r.members {
		if m.OrgID == orgID {
			n++
		}
	}
	return n, nil
}

func memberKey(orgID uint64, userID string) string {
	return string(rune(orgID)) + "/" + userID
}

// visCtx builds a caller context with tenant + role + user (+ optional
// system-admin flag), matching what the auth middleware attaches.
func visCtx(tenantID uint64, role types.TenantRole, userID string, sysAdmin bool) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenantID)
	ctx = context.WithValue(ctx, types.TenantRoleContextKey, role)
	if userID != "" {
		ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
	}
	ctx = types.WithCaller(ctx, types.Caller{TenantID: tenantID, UserID: userID, Role: role})
	if sysAdmin {
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
	}
	return ctx
}

func newVisibilityKBService() (*knowledgeBaseService, *fakeKBRepo, *fakeTenantOrgRepo) {
	repo := newFakeKBRepo()
	orgs := newFakeTenantOrgRepo()
	return &knowledgeBaseService{repo: repo, tenantOrgRepo: orgs}, repo, orgs
}

func TestCreateKnowledgeBaseVisibilityMatrix(t *testing.T) {
	orgID := uint64(5)
	foreignOrgID := uint64(9)

	cases := []struct {
		name       string
		visibility types.KBVisibility
		orgID      *uint64
		role       types.TenantRole
		userID     string
		sysAdmin   bool
		orgRole    types.TenantOrgRole
		wantErr    bool
	}{
		{name: "tenant default by contributor", visibility: types.KBVisibilityTenant,
			role: types.TenantRoleContributor, userID: "u1"},
		{name: "invalid visibility normalized to tenant",
			visibility: "bogus", role: types.TenantRoleContributor, userID: "u1"},
		{name: "public by owner", visibility: types.KBVisibilityPublic,
			role: types.TenantRoleOwner, userID: "u1"},
		{name: "public by sysadmin", visibility: types.KBVisibilityPublic,
			role: types.TenantRoleViewer, userID: "u1", sysAdmin: true},
		{name: "public by admin denied", visibility: types.KBVisibilityPublic,
			role: types.TenantRoleAdmin, userID: "u1", wantErr: true},
		{name: "public by viewer denied", visibility: types.KBVisibilityPublic,
			role: types.TenantRoleViewer, userID: "u1", wantErr: true},
		{name: "org without org_id", visibility: types.KBVisibilityOrg,
			role: types.TenantRoleAdmin, userID: "u1", wantErr: true},
		{name: "org foreign tenant rejected", visibility: types.KBVisibilityOrg,
			orgID: &foreignOrgID, role: types.TenantRoleAdmin, userID: "u1", wantErr: true},
		{name: "org by tenant admin", visibility: types.KBVisibilityOrg,
			orgID: &orgID, role: types.TenantRoleAdmin, userID: "u1"},
		{name: "org by org manager", visibility: types.KBVisibilityOrg,
			orgID: &orgID, role: types.TenantRoleContributor, userID: "u1",
			orgRole: types.TenantOrgRoleManager},
		{name: "org by org member denied", visibility: types.KBVisibilityOrg,
			orgID: &orgID, role: types.TenantRoleContributor, userID: "u1",
			orgRole: types.TenantOrgRoleMember, wantErr: true},
		{name: "org by outsider denied", visibility: types.KBVisibilityOrg,
			orgID: &orgID, role: types.TenantRoleContributor, userID: "u2", wantErr: true},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, orgRepo := newVisibilityKBService()
			orgRepo.orgs[orgID] = &types.TenantOrg{ID: orgID, TenantID: 1, Name: "org"}
			orgRepo.orgs[foreignOrgID] = &types.TenantOrg{ID: foreignOrgID, TenantID: 2, Name: "foreign"}
			if tt.orgRole != "" && tt.orgID != nil {
				orgRepo.members[memberKey(*tt.orgID, tt.userID)] = &types.TenantOrgMember{
					OrgID: *tt.orgID, UserID: tt.userID, Role: tt.orgRole,
				}
			}
			kb, err := svc.CreateKnowledgeBase(
				visCtx(1, tt.role, tt.userID, tt.sysAdmin),
				&types.KnowledgeBase{
					Name: "kb", Type: types.KnowledgeBaseTypeDocument,
					Visibility: tt.visibility, OrgID: tt.orgID,
				})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.visibility == types.KBVisibilityOrg {
				require.Equal(t, types.KBVisibilityOrg, kb.Visibility)
				require.NotNil(t, kb.OrgID)
			} else if tt.visibility == types.KBVisibilityPublic {
				require.Equal(t, types.KBVisibilityPublic, kb.Visibility)
				require.Nil(t, kb.OrgID)
			} else {
				require.Equal(t, types.KBVisibilityTenant, kb.Visibility)
				require.Nil(t, kb.OrgID)
			}
		})
	}
}

func TestSetKnowledgeBaseVisibilityMatrix(t *testing.T) {
	orgID := uint64(5)

	cases := []struct {
		name     string
		vis      types.KBVisibility
		orgID    *uint64
		caller   types.Caller
		sysAdmin bool
		wantErr  bool
	}{
		{name: "to public by owner",
			vis:    types.KBVisibilityPublic,
			caller: types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleOwner}},
		{name: "to public by admin denied",
			vis:     types.KBVisibilityPublic,
			caller:  types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleAdmin},
			wantErr: true},
		{name: "to org by admin",
			vis: types.KBVisibilityOrg, orgID: &orgID,
			caller: types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleAdmin}},
		{name: "to org without org_id",
			vis:     types.KBVisibilityOrg,
			caller:  types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleAdmin},
			wantErr: true},
		{name: "to org by member denied",
			vis: types.KBVisibilityOrg, orgID: &orgID,
			caller:  types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleViewer},
			wantErr: true},
		{name: "narrow to tenant by admin",
			vis:    types.KBVisibilityTenant,
			caller: types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleAdmin}},
		{name: "narrow to tenant by viewer denied",
			vis:     types.KBVisibilityTenant,
			caller:  types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleViewer},
			wantErr: true},
		{name: "foreign tenant caller denied",
			vis:     types.KBVisibilityTenant,
			caller:  types.Caller{TenantID: 2, UserID: "u1", Role: types.TenantRoleOwner},
			wantErr: true},
		{name: "invalid visibility",
			vis:     "bogus",
			caller:  types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleAdmin},
			wantErr: true},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, orgRepo := newVisibilityKBService()
			repo.rows["kb-1"] = &types.KnowledgeBase{
				ID: "kb-1", TenantID: 1, Visibility: types.KBVisibilityTenant,
			}
			orgRepo.orgs[orgID] = &types.TenantOrg{ID: orgID, TenantID: 1, Name: "org"}

			ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tt.caller.TenantID)
			ctx = context.WithValue(ctx, types.TenantRoleContextKey, tt.caller.Role)
			ctx = types.WithCaller(ctx, tt.caller)
			if tt.sysAdmin {
				ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
			}

			kb, err := svc.SetKnowledgeBaseVisibility(ctx, "kb-1", tt.vis, tt.orgID)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.vis, kb.Visibility)
			if tt.vis == types.KBVisibilityOrg {
				require.Equal(t, *tt.orgID, *kb.OrgID)
			} else {
				require.Nil(t, kb.OrgID)
			}
		})
	}
}
