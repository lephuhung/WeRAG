package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

const lookupPortalID uint64 = 50

// withPublicLookupBuiltins registers the smart-reasoning and public-lookup
// built-ins as builtin_agents.yaml does (no model ids on either).
func withPublicLookupBuiltins(t *testing.T) {
	t.Helper()
	restore := types.OverrideBuiltinAgentEntriesForTest(map[string]*types.BuiltinAgentEntry{
		types.BuiltinSmartReasoningID: {ID: types.BuiltinSmartReasoningID, IsBuiltin: true,
			I18n:   map[string]types.BuiltinAgentI18n{"default": {Name: "Smart Reasoning"}},
			Config: types.CustomAgentConfig{AgentMode: types.AgentModeSmartReasoning, KBSelectionMode: "all"}},
		types.BuiltinPublicLookupID: {ID: types.BuiltinPublicLookupID, IsBuiltin: true,
			I18n: map[string]types.BuiltinAgentI18n{"default": {Name: "Public Lookup"}},
			Config: types.CustomAgentConfig{AgentMode: types.AgentModeSmartReasoning,
				KBSelectionMode: types.KBSelectionModePublic}},
	})
	t.Cleanup(restore)
	for _, id := range []string{types.BuiltinSmartReasoningID, types.BuiltinPublicLookupID} {
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

// newPublicLookupService is newAgentScopeService plus the public portal
// (tenant 50 on profile 9) and the profile's smart-reasoning row carrying
// the models the SuperAdmin configured.
func newPublicLookupService(t *testing.T) *customAgentService {
	t.Helper()
	svc, db := newAgentScopeService(t)
	require.NoError(t, db.Exec(`INSERT INTO tenants (id, name, kind, config_profile_id) VALUES
		(50, 'Tra cứu công khai', 'public_portal', 9)`).Error)
	repo := repository.NewCustomAgentRepository(db)
	require.NoError(t, repo.CreateAgent(context.Background(), &types.CustomAgent{
		ID: types.BuiltinSmartReasoningID, Name: "Smart Reasoning", TenantID: 9, IsBuiltin: true,
		Config: types.CustomAgentConfig{AgentMode: types.AgentModeSmartReasoning, KBSelectionMode: "all",
			ModelID: "chat-qwen", RerankModelID: "rerank-1"},
	}))
	return svc
}

func portalGuestCtx() context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, lookupPortalID)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey,
		&types.Tenant{ID: lookupPortalID, Kind: types.TenantKindPublicPortal})
	return types.WithCaller(ctx, types.Caller{TenantID: lookupPortalID, UserID: "guest-1"})
}

// The public-lookup agent runs without setup: its empty model ids come from
// the smart-reasoning agent of the profile.
func TestPublicLookupInheritsSmartReasoningModels(t *testing.T) {
	withPublicLookupBuiltins(t)
	svc := newPublicLookupService(t)
	agent, err := svc.GetAgentByID(portalGuestCtx(), types.BuiltinPublicLookupID)
	require.NoError(t, err)
	require.Equal(t, "chat-qwen", agent.Config.ModelID)
	require.Equal(t, "rerank-1", agent.Config.RerankModelID)
	require.Equal(t, types.KBSelectionModePublic, agent.Config.KBSelectionMode)

	// the SuperAdmin's editor reads it as stored
	stored, err := svc.GetAgentByID(scopeSuperAdminCtx(9), types.BuiltinPublicLookupID)
	require.NoError(t, err)
	require.Empty(t, stored.Config.ModelID)
}

// A Guest in the portal resolves and lists the public-lookup agent only.
func TestPublicPortalSeesOnlyPublicLookup(t *testing.T) {
	withPublicLookupBuiltins(t)
	svc := newPublicLookupService(t)
	for _, id := range []string{types.BuiltinSmartReasoningID, "everyone"} {
		_, err := svc.GetAgentByID(portalGuestCtx(), id)
		require.ErrorIsf(t, err, ErrAgentNotFound, "agent %s", id)
	}
	list, err := svc.ListAgents(portalGuestCtx())
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, types.BuiltinPublicLookupID, list[0].ID)

	// members never get it in their picker; the SuperAdmin page lists it
	member, err := svc.ListAgents(scopeMemberCtx(1))
	require.NoError(t, err)
	require.False(t, listedIDs(t, member)[types.BuiltinPublicLookupID])
	require.True(t, listedIDs(t, member)[types.BuiltinSmartReasoningID])
	all, err := svc.ListAllAgents(scopeSuperAdminCtx(9))
	require.NoError(t, err)
	require.True(t, listedIDs(t, all)[types.BuiltinPublicLookupID])
}

// KBSelectionModePublic keeps platform public bases only: never the
// caller's own, published, restricted (invited) or another tenant's.
func TestFilterPlatformPublicKBIDs(t *testing.T) {
	agent := &types.CustomAgent{Config: types.CustomAgentConfig{
		AgentMode: types.AgentModeSmartReasoning, KBSelectionMode: types.KBSelectionModePublic,
		AllowedTools: []string{"search_knowledge", "read_document"},
	}}
	rag := types.IndexingStrategy{VectorEnabled: true, KeywordEnabled: true}
	kbs := []*types.KnowledgeBase{
		{ID: "pub", TenantID: 10000, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic, IndexingStrategy: rag},
		// a public base the tools cannot search (no vector/keyword index)
		{ID: "pub-unsearchable", TenantID: 10000, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic},
		{ID: "own", TenantID: 7, OwnerTenantID: 7, Visibility: types.KBVisibilityTenant, IndexingStrategy: rag},
		{ID: "published", TenantID: 5, OwnerTenantID: 5, Visibility: types.KBVisibilityPublished, IndexingStrategy: rag},
		{ID: "restricted", TenantID: 5, OwnerTenantID: 5, Visibility: types.KBVisibilityRestricted, IndexingStrategy: rag},
		{ID: "public-but-owned", TenantID: 5, OwnerTenantID: 5, Visibility: types.KBVisibilityPublic, IndexingStrategy: rag},
		nil,
	}
	require.Equal(t, []string{"pub"}, filterPlatformPublicKBIDs(kbs, agent))
}
