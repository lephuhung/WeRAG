package access

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type shareLookup struct {
	permission types.OrgMemberRole
	err        error
	caller     uint64
	role       types.TenantRole
	scope      *types.KBScope
	orgRole    types.TenantOrgRole
	orgMember  bool
}

func (s *shareLookup) CheckTenantKBPermission(
	_ context.Context,
	_ string,
	caller uint64,
	role types.TenantRole,
) (types.OrgMemberRole, bool, error) {
	s.caller, s.role = caller, role
	return s.permission, s.permission != "", s.err
}

type agentLookup struct {
	agent    *types.CustomAgent
	any      bool
	err      error
	anyCalls int
	source   uint64
}

func (s *agentLookup) GetSharedAgentForTenant(
	_ context.Context,
	_ uint64,
	_ types.TenantRole,
	_ string,
	source ...uint64,
) (*types.CustomAgent, error) {
	s.source = source[0]
	return s.agent, s.err
}

func (s *agentLookup) TenantCanAccessKBViaSomeSharedAgent(
	context.Context,
	uint64,
	types.TenantRole,
	*types.KnowledgeBase,
) (bool, error) {
	s.anyCalls++
	return s.any, s.err
}

func TestResolveKBPermissionMatrix(t *testing.T) {
	for _, tt := range []struct {
		name                                            string
		own                                             bool
		permission                                      types.OrgMemberRole
		required                                        types.OrgMemberRole
		shareError                                      bool
		agentID, source, mode                           string
		selected                                        []string
		wrongTenant, missingAgent, agentError, anyAgent bool
		want                                            types.OrgMemberRole
		wantErr                                         error
	}{
		{name: "own KB", own: true, required: types.OrgRoleEditor, want: types.OrgRoleAdmin},
		{
			name:       "organization reader",
			permission: types.OrgRoleViewer,
			required:   types.OrgRoleViewer,
			want:       types.OrgRoleViewer,
		},

		{
			name:       "organization editor",
			permission: types.OrgRoleEditor,
			required:   types.OrgRoleEditor,
			want:       types.OrgRoleEditor,
		},

		{
			name:       "reader cannot write",
			permission: types.OrgRoleViewer,
			required:   types.OrgRoleEditor,
			wantErr:    ErrForbidden,
		},

		{
			name:       "share lookup fails closed",
			permission: types.OrgRoleEditor,
			shareError: true,
			required:   types.OrgRoleViewer,
			wantErr:    ErrForbidden,
		},

		{
			name:       "independent agent grant after share error",
			shareError: true,
			anyAgent:   true,
			required:   types.OrgRoleViewer,
			want:       types.OrgRoleViewer,
		},

		{name: "any shared agent read", anyAgent: true, required: types.OrgRoleViewer, want: types.OrgRoleViewer},
		{name: "any agent cannot write", anyAgent: true, required: types.OrgRoleEditor, wantErr: ErrForbidden},
		{
			name:       "agent lookup error",
			anyAgent:   true,
			agentError: true,
			required:   types.OrgRoleViewer,
			wantErr:    ErrForbidden,
		},

		{
			name:     "explicit all",
			agentID:  "agent",
			source:   "2",
			mode:     "all",
			required: types.OrgRoleViewer,
			want:     types.OrgRoleViewer,
		},

		{
			name:     "explicit selected",
			agentID:  "agent",
			mode:     "selected",
			selected: []string{"kb"},
			required: types.OrgRoleViewer,
			want:     types.OrgRoleViewer,
		},

		{
			name:     "selection miss does not fall back",
			agentID:  "agent",
			mode:     "selected",
			selected: []string{"other"},
			anyAgent: true,
			required: types.OrgRoleViewer,
			wantErr:  ErrForbidden,
		},

		{
			name:     "none does not fall back",
			agentID:  "agent",
			mode:     "none",
			anyAgent: true,
			required: types.OrgRoleViewer,
			wantErr:  ErrForbidden,
		},

		{
			name:     "unknown mode denied",
			agentID:  "agent",
			mode:     "invalid",
			required: types.OrgRoleViewer,
			wantErr:  ErrForbidden,
		},

		{
			name:        "tenant mismatch denied",
			agentID:     "agent",
			mode:        "all",
			wrongTenant: true,
			required:    types.OrgRoleViewer,
			wantErr:     ErrForbidden,
		},

		{
			name:         "missing explicit agent does not fall back",
			agentID:      "agent",
			missingAgent: true,
			anyAgent:     true,
			required:     types.OrgRoleViewer,
			wantErr:      ErrForbidden,
		},

		{
			name:     "invalid source rejected",
			agentID:  "agent",
			source:   "bad",
			mode:     "all",
			required: types.OrgRoleViewer,
			wantErr:  ErrInvalidAgentSource,
		},

		{
			name:     "own grant precedes agent parsing",
			own:      true,
			agentID:  "agent",
			source:   "bad",
			required: types.OrgRoleViewer,
			want:     types.OrgRoleAdmin,
		},

		{
			name:       "share grant precedes agent parsing",
			permission: types.OrgRoleViewer,
			agentID:    "agent",
			source:     "bad",
			required:   types.OrgRoleViewer,
			want:       types.OrgRoleViewer,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			kb := &types.KnowledgeBase{ID: "kb", TenantID: 2}
			if tt.own {
				kb.TenantID = 1
			}
			shares := &shareLookup{permission: tt.permission}
			if tt.shareError {
				shares.err = errors.New("unavailable")
			}
			agents := &agentLookup{
				any: tt.anyAgent,
				agent: &types.CustomAgent{
					TenantID: 2,
					Config:   types.CustomAgentConfig{KBSelectionMode: tt.mode, KnowledgeBases: tt.selected},
				},
			}
			if tt.wrongTenant {
				agents.agent.TenantID = 3
			}
			if tt.missingAgent {
				agents.agent = nil
			}
			if tt.agentError {
				agents.err = errors.New("unavailable")
			}
			request := KBRequest{
				Caller:              types.Caller{TenantID: 1, Role: types.TenantRoleViewer},
				AgentID:             tt.agentID,
				AgentSourceTenantID: tt.source,
			}
			grant, err := ResolveKB(context.Background(), request, kb, tt.required, shares, agents)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, grant)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, grant.Permission)
				require.Equal(t, uint64(1), grant.Caller.TenantID)
				require.Equal(t, kb.TenantID, grant.EffectiveTenantID)
			}
			if !tt.own {
				require.Equal(t, uint64(1), shares.caller)
				require.Equal(t, types.TenantRoleViewer, shares.role)
			}
			if tt.agentID != "" {
				require.Zero(t, agents.anyCalls)
			}
			if tt.source == "2" {
				require.Equal(t, uint64(2), agents.source)
			}
		})
	}
}

func TestResolveKBMissingIdentityResourceAndAPIKeyScope(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1}
	_, err := ResolveKB(context.Background(), KBRequest{}, kb, types.OrgRoleViewer, nil, nil)
	require.ErrorIs(t, err, ErrUnauthorized)
	_, err = ResolveKB(
		context.Background(),
		KBRequest{Caller: types.Caller{TenantID: 1}},
		nil,
		types.OrgRoleViewer,
		nil,
		nil,
	)
	require.ErrorIs(t, err, ErrNotFound)
	ctx := types.WithTenantAPIKeyScope(
		context.Background(),
		types.TenantAPIKeyScope{KnowledgeBaseIDs: types.StringArray{"other"}},
	)
	_, err = ResolveKB(ctx, KBRequest{Caller: types.Caller{TenantID: 1}}, kb, types.OrgRoleViewer, nil, nil)
	require.Error(t, err, "API key scope must apply even to owned KBs")
}

func (s *shareLookup) GetKBScope(ctx context.Context, kbID string) (*types.KBScope, error) {
	return s.scope, nil
}

func (s *shareLookup) OrgMemberRole(ctx context.Context, tenantID, orgID uint64, userID string) (types.TenantOrgRole, bool, error) {
	return s.orgRole, s.orgMember, nil
}

func TestResolveKBVisibilityMatrix(t *testing.T) {
	orgID := uint64(7)
	for _, tt := range []struct {
		name       string
		visibility types.KBVisibility
		orgID      *uint64
		kbTenant   uint64
		caller     types.Caller
		sysAdmin   bool
		orgRole    types.TenantOrgRole
		orgMember  bool
		required   types.OrgMemberRole
		want       types.OrgMemberRole
		wantErr    error
	}{
		// --- org-scoped, same tenant ---
		{name: "org member reads",
			visibility: types.KBVisibilityOrg, orgID: &orgID, kbTenant: 1,
			caller:  types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleViewer},
			orgRole: types.TenantOrgRoleMember, orgMember: true,
			required: types.OrgRoleViewer, want: types.OrgRoleViewer},
		{name: "org member cannot write",
			visibility: types.KBVisibilityOrg, orgID: &orgID, kbTenant: 1,
			caller:  types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleViewer},
			orgRole: types.TenantOrgRoleMember, orgMember: true,
			required: types.OrgRoleEditor, wantErr: ErrForbidden},
		{name: "org manager writes",
			visibility: types.KBVisibilityOrg, orgID: &orgID, kbTenant: 1,
			caller:  types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleContributor},
			orgRole: types.TenantOrgRoleManager, orgMember: true,
			required: types.OrgRoleEditor, want: types.OrgRoleEditor},
		{name: "non-member same tenant denied",
			visibility: types.KBVisibilityOrg, orgID: &orgID, kbTenant: 1,
			caller:   types.Caller{TenantID: 1, UserID: "u2", Role: types.TenantRoleContributor},
			required: types.OrgRoleViewer, wantErr: ErrForbidden},
		{name: "tenant admin bypasses org membership",
			visibility: types.KBVisibilityOrg, orgID: &orgID, kbTenant: 1,
			caller:   types.Caller{TenantID: 1, UserID: "u3", Role: types.TenantRoleAdmin},
			required: types.OrgRoleEditor, want: types.OrgRoleAdmin},
		{name: "system admin bypasses org membership",
			visibility: types.KBVisibilityOrg, orgID: &orgID, kbTenant: 1,
			caller:   types.Caller{TenantID: 1, UserID: "u4", Role: types.TenantRoleViewer},
			sysAdmin: true,
			required: types.OrgRoleEditor, want: types.OrgRoleAdmin},
		// --- org-scoped, cross tenant: always denied ---
		{name: "org KB invisible across tenants",
			visibility: types.KBVisibilityOrg, orgID: &orgID, kbTenant: 2,
			caller:  types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleViewer},
			orgRole: types.TenantOrgRoleMember, orgMember: true,
			required: types.OrgRoleViewer, wantErr: ErrForbidden},
		// --- public, same tenant ---
		{name: "public KB member reads",
			visibility: types.KBVisibilityPublic, kbTenant: 1,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleViewer},
			required: types.OrgRoleViewer, want: types.OrgRoleViewer},
		{name: "public KB member cannot write",
			visibility: types.KBVisibilityPublic, kbTenant: 1,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleContributor},
			required: types.OrgRoleEditor, wantErr: ErrForbidden},
		{name: "public KB owner writes",
			visibility: types.KBVisibilityPublic, kbTenant: 1,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleOwner},
			required: types.OrgRoleEditor, want: types.OrgRoleAdmin},
		{name: "public KB system admin writes",
			visibility: types.KBVisibilityPublic, kbTenant: 1,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleViewer},
			sysAdmin: true,
			required: types.OrgRoleEditor, want: types.OrgRoleAdmin},
		// --- public, cross tenant ---
		{name: "public KB foreign tenant reads",
			visibility: types.KBVisibilityPublic, kbTenant: 2,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleViewer},
			required: types.OrgRoleViewer, want: types.OrgRoleViewer},
		{name: "public KB foreign tenant cannot write",
			visibility: types.KBVisibilityPublic, kbTenant: 2,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleOwner},
			required: types.OrgRoleEditor, wantErr: ErrForbidden},
		// --- tenant-scoped unchanged ---
		{name: "tenant KB member gets admin in own tenant",
			visibility: types.KBVisibilityTenant, kbTenant: 1,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleViewer},
			required: types.OrgRoleEditor, want: types.OrgRoleAdmin},
		{name: "tenant KB foreign denied without share",
			visibility: types.KBVisibilityTenant, kbTenant: 2,
			caller:   types.Caller{TenantID: 1, UserID: "u1", Role: types.TenantRoleViewer},
			required: types.OrgRoleViewer, wantErr: ErrForbidden},
	} {
		t.Run(tt.name, func(t *testing.T) {
			kb := &types.KnowledgeBase{
				ID: "kb", TenantID: tt.kbTenant,
				Visibility: tt.visibility, OrgID: tt.orgID,
			}
			shares := &shareLookup{orgRole: tt.orgRole, orgMember: tt.orgMember}
			// any:false keeps the shared-agent read fallback out of the
			// matrix — visibility branches return before it anyway.
			agents := &agentLookup{any: false}
			ctx := context.Background()
			if tt.sysAdmin {
				ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
			}
			grant, err := ResolveKB(ctx,
				KBRequest{Caller: tt.caller}, kb, tt.required, shares, agents)
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
