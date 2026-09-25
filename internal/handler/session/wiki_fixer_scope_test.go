package session

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type wikiFixerKBLookupStub struct {
	kb         *types.KnowledgeBase
	err        error
	calledWith string
}

func (s *wikiFixerKBLookupStub) GetKnowledgeBaseByIDOnly(_ context.Context, id string) (*types.KnowledgeBase, error) {
	s.calledWith = id
	return s.kb, s.err
}

// wikiFixerKBGrantStub answers ApprovedKBPermission for the caller's tenant.
// Grants are viewer-only in production, but the stub can return editor to
// keep exercising the owner-tenant switch should write grants ever exist.
type wikiFixerKBGrantStub struct {
	permission      types.KBPermission
	isShared        bool
	err             error
	checkedKBID     string
	checkedTenantID uint64
}

func (s *wikiFixerKBGrantStub) ApprovedKBPermission(
	_ context.Context,
	kbID string,
	granteeTenantID uint64,
) (types.KBPermission, bool, error) {
	s.checkedKBID = kbID
	s.checkedTenantID = granteeTenantID
	return s.permission, s.isShared, s.err
}

func (s *wikiFixerKBGrantStub) GetKBScope(_ context.Context, _ string) (*types.KBScope, error) {
	return nil, nil
}

func TestResolveBuiltinWikiFixerTenantScopeRejectsLegacyEditorGrant(t *testing.T) {
	agent := &types.CustomAgent{ID: types.BuiltinWikiFixerID, TenantID: 10, Name: "Wiki Fixer"}
	kbLookup := &wikiFixerKBLookupStub{
		kb: &types.KnowledgeBase{ID: "kb-shared", TenantID: 20, Name: "Shared KB"},
	}
	kbGrants := &wikiFixerKBGrantStub{
		permission: types.KBPermissionEditor,
		isShared:   true,
	}

	gotAgent, effectiveTenantID := resolveBuiltinWikiFixerTenantScope(
		context.Background(),
		agent,
		10,
		types.TenantRoleMember,
		[]string{"kb-shared"},
		kbLookup,
		kbGrants,
	)

	require.Same(t, agent, gotAgent, "legacy tenant-wide grant must not switch execution scope")
	require.Zero(t, effectiveTenantID)
	require.Equal(t, uint64(10), agent.TenantID, "must not mutate the cached built-in agent")
	require.Equal(t, "kb-shared", kbLookup.calledWith)
	require.Empty(t, kbGrants.checkedKBID, "retired grant lookup must not be consulted")
}

// The scoped run executes in the owner's workspace, so the caller's own fixer
// customizations must not select the owner's other KBs, MCP services, skills
// or models there.
func TestResolveBuiltinWikiFixerTenantScopeDoesNotPinToLegacySharedKB(t *testing.T) {
	agent := &types.CustomAgent{ID: types.BuiltinWikiFixerID, TenantID: 10, Config: types.CustomAgentConfig{
		KBSelectionMode:     "all",
		MCPSelectionMode:    "",
		SkillsSelectionMode: "all",
		SandboxConfigID:     "caller-sandbox",
		WebSearchEnabled:    true,
		ModelID:             "caller-model",
	}}
	kbLookup := &wikiFixerKBLookupStub{kb: &types.KnowledgeBase{ID: "kb-shared", TenantID: 20}}
	kbGrants := &wikiFixerKBGrantStub{permission: types.KBPermissionEditor, isShared: true}

	gotAgent, _ := resolveBuiltinWikiFixerTenantScope(
		context.Background(), agent, 10, types.TenantRoleMember, []string{"kb-shared"}, kbLookup, kbGrants,
	)

	require.Same(t, agent, gotAgent, "retired tenant-wide grant must not reconfigure the agent")
	require.Equal(t, "all", gotAgent.Config.KBSelectionMode)
	require.Empty(t, gotAgent.Config.KnowledgeBases)
	require.Equal(t, "all", agent.Config.KBSelectionMode, "must not mutate the caller's agent")
}

func TestResolveBuiltinWikiFixerTenantScope_SharedViewerDoesNotSwitchTenant(t *testing.T) {
	agent := &types.CustomAgent{ID: types.BuiltinWikiFixerID, TenantID: 10}
	kbLookup := &wikiFixerKBLookupStub{
		kb: &types.KnowledgeBase{ID: "kb-shared", TenantID: 20},
	}
	kbGrants := &wikiFixerKBGrantStub{
		permission: types.KBPermissionViewer,
		isShared:   true,
	}

	gotAgent, effectiveTenantID := resolveBuiltinWikiFixerTenantScope(
		context.Background(),
		agent,
		10,
		types.TenantRoleMember,
		[]string{"kb-shared"},
		kbLookup,
		kbGrants,
	)

	require.Same(t, agent, gotAgent)
	require.Zero(t, effectiveTenantID)
	require.Equal(t, uint64(10), gotAgent.TenantID)
}

func TestResolveBuiltinWikiFixerTenantScope_IgnoresNonWikiFixerAgents(t *testing.T) {
	agent := &types.CustomAgent{ID: "custom-agent", TenantID: 10}
	kbLookup := &wikiFixerKBLookupStub{
		kb: &types.KnowledgeBase{ID: "kb-shared", TenantID: 20},
	}
	kbGrants := &wikiFixerKBGrantStub{
		permission: types.KBPermissionEditor,
		isShared:   true,
	}

	gotAgent, effectiveTenantID := resolveBuiltinWikiFixerTenantScope(
		context.Background(),
		agent,
		10,
		types.TenantRoleMember,
		[]string{"kb-shared"},
		kbLookup,
		kbGrants,
	)

	require.Same(t, agent, gotAgent)
	require.Zero(t, effectiveTenantID)
	require.Empty(t, kbLookup.calledWith)
}

func TestResolveBuiltinWikiFixerTenantScope_RequiresSingleKnowledgeBase(t *testing.T) {
	agent := &types.CustomAgent{ID: types.BuiltinWikiFixerID, TenantID: 10}
	kbLookup := &wikiFixerKBLookupStub{
		kb: &types.KnowledgeBase{ID: "kb-shared", TenantID: 20},
	}

	gotAgent, effectiveTenantID := resolveBuiltinWikiFixerTenantScope(
		context.Background(),
		agent,
		10,
		types.TenantRoleMember,
		[]string{"kb-a", "kb-b"},
		kbLookup,
		&wikiFixerKBGrantStub{permission: types.KBPermissionEditor, isShared: true},
	)

	require.Same(t, agent, gotAgent)
	require.Zero(t, effectiveTenantID)
	require.Empty(t, kbLookup.calledWith)
}

func TestResolveBuiltinWikiFixerTenantScope_FallsBackOnLookupOrPermissionErrors(t *testing.T) {
	agent := &types.CustomAgent{ID: types.BuiltinWikiFixerID, TenantID: 10}

	t.Run("kb lookup error", func(t *testing.T) {
		gotAgent, effectiveTenantID := resolveBuiltinWikiFixerTenantScope(
			context.Background(),
			agent,
			10,
			types.TenantRoleMember,
			[]string{"kb-shared"},
			&wikiFixerKBLookupStub{err: errors.New("lookup failed")},
			&wikiFixerKBGrantStub{permission: types.KBPermissionEditor, isShared: true},
		)

		require.Same(t, agent, gotAgent)
		require.Zero(t, effectiveTenantID)
	})

	t.Run("permission check error", func(t *testing.T) {
		gotAgent, effectiveTenantID := resolveBuiltinWikiFixerTenantScope(
			context.Background(),
			agent,
			10,
			types.TenantRoleMember,
			[]string{"kb-shared"},
			&wikiFixerKBLookupStub{kb: &types.KnowledgeBase{ID: "kb-shared", TenantID: 20}},
			&wikiFixerKBGrantStub{err: errors.New("permission failed")},
		)

		require.Same(t, agent, gotAgent)
		require.Zero(t, effectiveTenantID)
	})
}
