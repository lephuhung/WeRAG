package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent"
	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/sandbox"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

var bundledLegalSkillNames = []string{
	"legal-document-summary",
}

func TestBuiltinLegalSkillsRunWithoutSandboxOrTenantSkills(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	cfg := &types.AgentConfig{SkillsEnabled: false, AllowedTools: []string{tools.ToolThinking, tools.ToolShellExec}}
	svc := &agentService{}
	chatModel := &fakeAgentChatModel{}
	engine, err := svc.CreateAgentEngine(ctx, cfg, chatModel, nil, nil, "sess", "msg")
	require.NoError(t, err)
	mgr := engine.(*agent.AgentEngine).GetSkillsManager()
	require.NotNil(t, mgr)
	var names []string
	for _, item := range mgr.GetAllMetadata() {
		names = append(names, item.Name)
	}
	require.Equal(t, bundledLegalSkillNames, names)
	_, err = engine.Execute(ctx, "sess", "msg", "Tóm tắt tài liệu", nil)
	require.NoError(t, err)
	require.True(t, toolOffered(chatModel.lastToolNames, tools.ToolReadFile))
	for _, unavailable := range []string{tools.ToolShellExec, tools.ToolListSandboxFiles, tools.ToolWriteSandboxFile, tools.ToolEditSandboxFile} {
		require.False(t, toolOffered(chatModel.lastToolNames, unavailable), "%s must not be granted by read-only skills", unavailable)
	}

	registry := tools.NewToolRegistry()
	_, err = svc.initializeSkillsManager(ctx, "sess", cfg, registry)
	require.NoError(t, err)
	reader, err := registry.GetTool(tools.ToolReadFile)
	require.NoError(t, err)
	result, err := reader.Execute(ctx, json.RawMessage(`{"path":"skill://legal-document-summary/SKILL.md"}`))
	require.NoError(t, err)
	require.True(t, result.Success, result.Error)
	require.Contains(t, result.Output, "legal-document-summary")
	bad, err := reader.Execute(ctx, json.RawMessage(`{"path":"skill://pdf-processing/SKILL.md"}`))
	require.NoError(t, err)
	require.False(t, bad.Success)
}

func TestBuiltinLegalSkillsSurviveUnavailableSandboxConfig(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	cfg := &types.AgentConfig{SandboxConfigID: "missing", SkillsEnabled: false}
	engine, err := (&agentService{}).CreateAgentEngine(ctx, cfg, &fakeAgentChatModel{}, nil, nil, "sess", "msg")
	require.NoError(t, err)
	mgr := engine.(*agent.AgentEngine).GetSkillsManager()
	require.NotNil(t, mgr, "built-in instructions must not depend on resolving a named sandbox")
	require.Len(t, mgr.GetAllMetadata(), len(bundledLegalSkillNames))
}

func TestBuiltinLegalSkillsExcludeInstallerAndDisabledTenantRows(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	installer := &types.AgentConfig{SkillsEnabled: false}
	installer.EnableSkillInstallMode(types.BuiltinSkillInstallerID, sandbox.SkillsImageRoot+"/pptx")
	engine, err := (&agentService{}).CreateAgentEngine(ctx, installer, &fakeAgentChatModel{}, nil, nil, "sess", "msg")
	require.NoError(t, err)
	require.Nil(t, engine.(*agent.AgentEngine).GetSkillsManager())

	cfg := &types.AgentConfig{SkillsEnabled: false, TenantSkills: []*types.TenantSkillEntity{{
		Name: "tenant-secret", Enabled: true, Status: types.SkillStatusReady,
	}}}
	engine, err = (&agentService{}).CreateAgentEngine(ctx, cfg, &fakeAgentChatModel{}, nil, nil, "sess", "msg")
	require.NoError(t, err)
	require.NotNil(t, engine.(*agent.AgentEngine).GetSkillsManager())
	var names []string
	for _, item := range engine.(*agent.AgentEngine).GetSkillsManager().GetAllMetadata() {
		names = append(names, item.Name)
	}
	require.Equal(t, bundledLegalSkillNames, names)
}

func TestBuiltinLegalSkillsSkipAgentsWithTheirOwnTools(t *testing.T) {
	for _, agent := range []*types.CustomAgent{
		{ID: types.BuiltinDocumentAssistantID},
		{ID: types.BuiltinDataAnalystID},
		{ID: types.BuiltinWikiResearcherID},
		{ID: types.BuiltinWikiFixerID},
		{ID: "custom-data", Config: types.CustomAgentConfig{AgentType: types.AgentTypeDataAnalysis}},
		{ID: "custom-wiki", Config: types.CustomAgentConfig{AgentType: types.AgentTypeWikiQA}},
		{ID: "doc-assistant-copy", Config: types.CustomAgentConfig{AllowedTools: []string{tools.ToolRewriteParagraphs}}},
	} {
		require.False(t, builtinLegalSkillsApply(agent), agent.ID)
	}
	require.True(t, builtinLegalSkillsApply(&types.CustomAgent{ID: types.BuiltinSmartReasoningID}))
	require.True(t, builtinLegalSkillsApply(&types.CustomAgent{ID: "custom", Config: types.CustomAgentConfig{AgentType: types.AgentTypeRAGQA}}))

	cfg := &types.AgentConfig{}
	(&sessionService{}).configureSkillsFromAgent(context.Background(), cfg, &types.CustomAgent{ID: types.BuiltinDocumentAssistantID})
	require.True(t, cfg.BuiltinSkillsDisabled)
}

func TestDisabledBuiltinSkillsLeaveNoSkillManagerOrReader(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	cfg := &types.AgentConfig{BuiltinSkillsDisabled: true}
	registry := tools.NewToolRegistry()
	mgr, err := (&agentService{}).initializeSkillsManager(ctx, "sess", cfg, registry)
	require.NoError(t, err)
	require.Nil(t, mgr)
	_, err = registry.GetTool(tools.ToolReadFile)
	require.Error(t, err, "without skills or a sandbox there is nothing for read_file to read")
}
