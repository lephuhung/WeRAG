package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	domainpackassets "github.com/Tencent/WeKnora/domain-packs"
	"github.com/Tencent/WeKnora/internal/agent"
	"github.com/Tencent/WeKnora/internal/agent/domainpacks"
	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/sandbox"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

const testPack = "sample-pack"

func testPackService(t *testing.T) *agentService {
	t.Helper()
	registry, err := domainpacks.Load(os.DirFS("../../agent/domainpacks/testdata"))
	require.NoError(t, err)
	svc := &agentService{}
	svc.SetDomainPacks(registry)
	return svc
}

func engineSkillNames(t *testing.T, engine interfaces.AgentEngine) []string {
	t.Helper()
	mgr := engine.(*agent.AgentEngine).GetSkillsManager()
	require.NotNil(t, mgr)
	var names []string
	for _, item := range mgr.GetAllMetadata() {
		names = append(names, item.Name)
	}
	return names
}

func TestDomainPackRunsWithoutSandboxOnlyForItsAgent(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	svc := testPackService(t)

	plain := &types.AgentConfig{SkillsEnabled: false}
	engine, err := svc.CreateAgentEngine(ctx, plain, &fakeAgentChatModel{}, nil, nil, "sess", "msg")
	require.NoError(t, err)
	require.Equal(t, bundledLegalSkillNames, engineSkillNames(t, engine), "an agent without the pack does not see it")
	state, err := engine.Execute(ctx, "sess", "msg", "hỏi", nil)
	require.NoError(t, err)
	require.Equal(t, "ok", state.FinalAnswer, "no footer without a pack")

	cfg := &types.AgentConfig{SkillsEnabled: false, DomainPacks: []string{testPack, "not-shipped"},
		AllowedTools: []string{tools.ToolThinking, tools.ToolShellExec}}
	engine, err = svc.CreateAgentEngine(ctx, cfg, &fakeAgentChatModel{}, nil, nil, "sess", "msg")
	require.NoError(t, err)
	require.Equal(t, append(append([]string(nil), bundledLegalSkillNames...), testPack), engineSkillNames(t, engine))
	state, err = engine.Execute(ctx, "sess", "msg", "hỏi", nil)
	require.NoError(t, err)
	require.Equal(t, "ok"+domainpacks.FooterMarker+"Thông tin tham khảo, không phải tư vấn pháp lý.\n", state.FinalAnswer)

	// the defaults lack the pack's required thinking tool: no pack, no footer
	cfg = &types.AgentConfig{DomainPacks: []string{testPack}}
	engine, err = svc.CreateAgentEngine(ctx, cfg, &fakeAgentChatModel{}, nil, nil, "sess", "msg")
	require.NoError(t, err)
	require.Equal(t, bundledLegalSkillNames, engineSkillNames(t, engine))
	state, err = engine.Execute(ctx, "sess", "msg", "hỏi", nil)
	require.NoError(t, err)
	require.Equal(t, "ok", state.FinalAnswer)
}

func TestDomainPackResourcesReadableButNotExecutable(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	svc := testPackService(t)
	cfg := &types.AgentConfig{SkillsEnabled: false, DomainPacks: []string{testPack}}
	registry := tools.NewToolRegistry()
	shell := tools.NewShellExecTool(refusingExecutor{t: t}, nil)
	registry.RegisterTool(shell)
	registry.RegisterTool(tools.NewSequentialThinkingTool())
	mgr, err := svc.initializeSkillsManager(ctx, "sess", cfg, registry)
	require.NoError(t, err)
	require.True(t, mgr.IsReadOnlySkill(testPack))

	reader, err := registry.GetTool(tools.ToolReadFile)
	require.NoError(t, err)
	result, err := reader.Execute(ctx, json.RawMessage(`{"path":"skill://sample-pack/references/a.md"}`))
	require.NoError(t, err)
	require.True(t, result.Success, result.Error)
	require.Contains(t, result.Output, "Ghi chú A")
	hidden, err := reader.Execute(ctx, json.RawMessage(`{"path":"skill://sample-pack/catalogue.yaml"}`))
	require.NoError(t, err)
	require.False(t, hidden.Success)

	refused, err := shell.Execute(ctx, json.RawMessage(`{"skill_name":"sample-pack","command":"ls"}`))
	require.NoError(t, err)
	require.False(t, refused.Success)
	require.Contains(t, refused.Error, "read-only")
}

func TestDomainPackFooterOverdueLine(t *testing.T) {
	old := domainPackNow
	domainPackNow = func() time.Time { return time.Date(2027, 5, 1, 9, 0, 0, 0, time.FixedZone("ICT", 7*3600)) }
	t.Cleanup(func() { domainPackNow = old })

	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	cfg := &types.AgentConfig{DomainPacks: []string{testPack}, AllowedTools: []string{tools.ToolThinking}}
	engine, err := testPackService(t).CreateAgentEngine(ctx, cfg, &fakeAgentChatModel{}, nil, nil, "sess", "msg")
	require.NoError(t, err)
	state, err := engine.Execute(ctx, "sess", "msg", "hỏi", nil)
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(state.FinalAnswer,
		"Nội dung cẩm nang có mốc 17/10/2026, đã quá hạn rà soát.\n"), state.FinalAnswer)
}

func TestPerRequestPinAcceptsAgentDomainPack(t *testing.T) {
	cfg := &types.AgentConfig{DomainPacks: []string{testPack}}
	applyPerRequestSkillScope(context.Background(), cfg, "none", []string{testPack, "other-pack"})
	require.Equal(t, []string{testPack}, cfg.PinnedSkillNames)
}

// refusingExecutor fails the test if a command reaches the sandbox.
type refusingExecutor struct{ t *testing.T }

func (e refusingExecutor) ExecShellCommand(context.Context, string, string, string, time.Duration,
	map[string]string) (*sandbox.ExecuteResult, error) {
	e.t.Fatal("a domain pack must never reach the sandbox")
	return nil, nil
}

func TestResolveAgentDomainPacks(t *testing.T) {
	registry, err := domainpacks.Load(os.DirFS("../../agent/domainpacks/testdata"))
	require.NoError(t, err)
	svc := &sessionService{}
	svc.SetDomainPacks(registry)
	ctx := context.Background()

	agentWith := func(mode string, packs ...string) *types.CustomAgent {
		return &types.CustomAgent{ID: "a1", Config: types.CustomAgentConfig{AgentMode: mode, DomainPacks: packs}}
	}
	require.Equal(t, []string{testPack},
		svc.resolveAgentDomainPacks(ctx, agentWith(types.AgentModeSmartReasoning, " "+testPack+" ", "gone", testPack)))
	require.Empty(t, svc.resolveAgentDomainPacks(ctx, agentWith(types.AgentModeQuickAnswer, testPack)),
		"only smart-reasoning agents read packs")
	require.Empty(t, svc.resolveAgentDomainPacks(ctx, agentWith(types.AgentModeSmartReasoning)))
}

// The footer of an earlier answer is cut from history so the model does not
// copy it into the next one.
func TestFinalAnswerHistoryDropsDomainFooter(t *testing.T) {
	msg := &types.Message{Content: "Trả lời." + domainpacks.FooterMarker + "Thông tin tham khảo.\n"}
	got := finalAnswerHistoryMessage(msg)
	require.NotNil(t, got)
	require.Equal(t, "Trả lời.", got.Content)
}

func TestDomainPackPromptsCarryBodiesUpToTheCap(t *testing.T) {
	reg, err := domainpacks.Load(domainpackassets.FS)
	if err != nil {
		t.Fatal(err)
	}
	pack, ok := reg.Get("opc-vn-business-law")
	if !ok {
		t.Fatal("pilot pack not shipped")
	}
	got := domainPackPrompts(context.Background(), []*domainpacks.Pack{pack})
	if len(got) != 1 || got[0].Name != "opc-vn-business-law" || got[0].Body != pack.Skill.Instructions ||
		got[0].Version != pack.Manifest.Metadata.Version {
		t.Fatalf("prompt = %+v", got)
	}
	if strings.Contains(got[0].Body, "answer_footer") {
		t.Error("the body carries no frontmatter")
	}
	capped := domainPackPrompts(context.Background(), []*domainpacks.Pack{pack, pack, pack})
	if len(capped) != agent.MaxDomainPackPrompts {
		t.Fatalf("got %d bodies, cap is %d", len(capped), agent.MaxDomainPackPrompts)
	}
}
