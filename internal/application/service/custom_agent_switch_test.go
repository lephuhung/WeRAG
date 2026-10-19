package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// withSwitchableBuiltins registers the quick-answer built-in (off in
// builtin_agents.yaml, as on dev) and the document assistant (on).
func withSwitchableBuiltins(t *testing.T) {
	t.Helper()
	off, on := false, true
	restore := types.OverrideBuiltinAgentEntriesForTest(map[string]*types.BuiltinAgentEntry{
		types.BuiltinQuickAnswerID: {ID: types.BuiltinQuickAnswerID, IsBuiltin: true, Enabled: &off,
			I18n: map[string]types.BuiltinAgentI18n{"default": {Name: "Quick Answer"}}},
		types.BuiltinDocumentAssistantID: {ID: types.BuiltinDocumentAssistantID, IsBuiltin: true, Enabled: &on,
			I18n: map[string]types.BuiltinAgentI18n{"default": {Name: "Soạn thảo văn bản"}}},
	})
	t.Cleanup(restore)
	for _, id := range []string{types.BuiltinQuickAnswerID, types.BuiltinDocumentAssistantID} {
		id := id
		if _, ok := types.BuiltinAgentRegistry[id]; ok {
			continue
		}
		types.BuiltinAgentRegistry[id] = func(tenantID uint64) *types.CustomAgent {
			return types.GetBuiltinAgentWithContext(context.Background(), id, tenantID)
		}
		t.Cleanup(func() { delete(types.BuiltinAgentRegistry, id) })
	}
}

func listedIDs(t *testing.T, agents []*types.CustomAgent) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, a := range agents {
		out[a.ID] = true
		require.Equal(t, a.EffectiveEnabled(), a.Enabled, "API field mirrors the effective switch")
	}
	return out
}

func TestBuiltinAgentSwitchOverridesYAML(t *testing.T) {
	withSwitchableBuiltins(t)
	svc, _ := newAgentScopeService(t)
	profile := scopeSuperAdminCtx(9)

	// YAML defaults: quick answer off, document assistant on
	list, err := svc.ListAgents(scopeMemberCtx(1))
	require.NoError(t, err)
	ids := listedIDs(t, list)
	require.False(t, ids[types.BuiltinQuickAnswerID])
	require.True(t, ids[types.BuiltinDocumentAssistantID])
	all, err := svc.ListAllAgents(profile)
	require.NoError(t, err)
	require.True(t, listedIDs(t, all)[types.BuiltinQuickAnswerID], "the SuperAdmin page lists it to switch it on")
	member, err := svc.ListAllAgents(scopeMemberCtx(1))
	require.NoError(t, err)
	require.False(t, listedIDs(t, member)[types.BuiltinQuickAnswerID], "include_disabled is SuperAdmin-only")

	// switched on in the profile: every workspace lists it
	got, err := svc.SetAgentEnabled(profile, types.BuiltinQuickAnswerID, true)
	require.NoError(t, err)
	require.True(t, got.Enabled)
	list, err = svc.ListAgents(scopeMemberCtx(2))
	require.NoError(t, err)
	require.True(t, listedIDs(t, list)[types.BuiltinQuickAnswerID])

	// the document assistant switched off: gone from pickers, still
	// resolvable by id (sessions and pinned flows keep working)
	_, err = svc.SetAgentEnabled(profile, types.BuiltinDocumentAssistantID, false)
	require.NoError(t, err)
	list, err = svc.ListAgents(scopeMemberCtx(1))
	require.NoError(t, err)
	require.False(t, listedIDs(t, list)[types.BuiltinDocumentAssistantID])
	da, err := svc.GetAgentByID(scopeMemberCtx(1), types.BuiltinDocumentAssistantID)
	require.NoError(t, err)
	require.False(t, da.Enabled)
	require.False(t, types.MCPEndpointAgentSelectable(da, 1), "not offered for a new MCP endpoint")

	// switched back on: the stored value, not YAML, decides
	_, err = svc.SetAgentEnabled(profile, types.BuiltinDocumentAssistantID, true)
	require.NoError(t, err)
	da, err = svc.GetAgentByID(scopeMemberCtx(1), types.BuiltinDocumentAssistantID)
	require.NoError(t, err)
	require.True(t, da.Enabled)
}

func TestCustomAgentSwitchHidesFromEveryone(t *testing.T) {
	svc, _ := newAgentScopeService(t)
	profile := scopeSuperAdminCtx(9)

	_, err := svc.SetAgentEnabled(profile, "everyone", false)
	require.NoError(t, err)
	_, err = svc.GetAgentByID(scopeMemberCtx(2), "everyone")
	require.ErrorIs(t, err, ErrAgentNotFound, "next turn → 2104")
	list, err := svc.ListAgents(scopeMemberCtx(2))
	require.NoError(t, err)
	require.False(t, listedIDs(t, list)["everyone"])

	// the SuperAdmin still opens it, and lists it only on the agent page
	got, err := svc.GetAgentByID(scopeSuperAdminCtx(2), "everyone")
	require.NoError(t, err)
	require.False(t, got.Enabled)
	list, err = svc.ListAgents(scopeSuperAdminCtx(2))
	require.NoError(t, err)
	require.False(t, listedIDs(t, list)["everyone"], "not in the SuperAdmin's chat picker")
	all, err := svc.ListAllAgents(scopeSuperAdminCtx(2))
	require.NoError(t, err)
	require.True(t, listedIDs(t, all)["everyone"])

	_, err = svc.SetAgentEnabled(profile, "everyone", true)
	require.NoError(t, err)
	_, err = svc.GetAgentByID(scopeMemberCtx(2), "everyone")
	require.NoError(t, err)

	// refused for anyone but a SuperAdmin; unknown ids are not found
	_, err = svc.SetAgentEnabled(scopeMemberCtx(1), "everyone", false)
	require.ErrorIs(t, err, ErrAgentSwitchForbidden)
	_, err = svc.SetAgentEnabled(profile, "no-such-agent", false)
	require.ErrorIs(t, err, ErrAgentNotFound)
}

// Review item 4: a SuperAdmin deletes a switched-off custom agent.
func TestSuperAdminDeletesSwitchedOffAgent(t *testing.T) {
	svc, _ := newAgentScopeService(t)
	profile := scopeSuperAdminCtx(9)
	_, err := svc.SetAgentEnabled(profile, "everyone", false)
	require.NoError(t, err)
	require.NoError(t, svc.DeleteAgent(profile, "everyone"))
	_, err = svc.GetAgentByID(profile, "everyone")
	require.ErrorIs(t, err, ErrAgentNotFound)
}

// Review item 9: a platform API key (which the ConfigProfile middleware lets
// through with its own tenant) still writes the switch on the profile.
func TestAgentSwitchFromAWorkspaceWritesTheProfile(t *testing.T) {
	withSwitchableBuiltins(t)
	svc, db := newAgentScopeService(t)
	profileID := uint64(9)
	ctx := scopeSuperAdminCtx(1)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, &types.Tenant{ID: 1, Kind: types.TenantKindWorkspace,
		ConfigProfileID: &profileID})
	_, err := svc.SetAgentEnabled(ctx, types.BuiltinQuickAnswerID, true)
	require.NoError(t, err)
	var tenants []uint64
	require.NoError(t, db.Model(&types.CustomAgent{}).Where("id = ?", types.BuiltinQuickAnswerID).Pluck("tenant_id", &tenants).Error)
	require.Equal(t, []uint64{9}, tenants, "the row is the profile's, not the workspace's")
	list, err := svc.ListAgents(scopeMemberCtx(2))
	require.NoError(t, err)
	require.True(t, listedIDs(t, list)[types.BuiltinQuickAnswerID], "every workspace on the profile sees it")
}
