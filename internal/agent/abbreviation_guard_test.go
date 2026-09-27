package agent

import (
	"context"
	"sync/atomic"
	"testing"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// guardBinding is the required turn binding the tests share: QA for session
// sess-1 answering user message user-1 with assistant message assist-1.
func guardBinding() types.AbbreviationBinding {
	return types.AbbreviationBinding{
		Owner: types.AbbreviationOwner{
			TenantID:    1,
			SessionID:   "sess-1",
			OwnerID:     "owner-1",
			PrincipalID: "user:owner-1",
		},
		UserMessageID:      "user-1",
		AssistantMessageID: "assist-1",
		RawQuery:           "ATTT là gì",
	}
}

// sealedGuardCtx binds a ready single-meaning resolution for "ATTT là gì"
// and returns the sealed context plus its binding.
func sealedGuardCtx(t *testing.T) (context.Context, types.AbbreviationBinding) {
	t.Helper()
	res := abbreviation.Inspect("ATTT là gì", []*types.Abbreviation{
		{ID: "m-1", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
	})
	require.Equal(t, types.AbbreviationStatusReady, res.Status)
	binding := guardBinding()
	ctx, err := abbreviation.BindTurn(context.Background(), binding, res)
	require.NoError(t, err)
	return ctx, binding
}

// newGuardEngine builds an engine wired like a QA engine: a real registry so
// tool calls could execute if the gate let them through.
func newGuardEngine(t *testing.T, model chat.Chat, registry *agenttools.ToolRegistry) *AgentEngine {
	t.Helper()
	if registry == nil {
		registry = agenttools.NewToolRegistry()
	}
	engine := NewAgentEngine(
		&types.AgentConfig{MaxIterations: 5, Temperature: 0.7},
		model, registry, event.NewEventBus(), nil, nil, "sess-1", "",
	)
	require.NotNil(t, engine)
	return engine
}

// finalAnswerCount subscribes to the engine's bus and counts emitted
// EventAgentFinalAnswer events so tests can assert nothing was streamed.
func finalAnswerCount(engine *AgentEngine) *atomic.Int32 {
	var count atomic.Int32
	engine.eventBus.On(event.EventAgentFinalAnswer, func(context.Context, event.Event) error {
		count.Add(1)
		return nil
	})
	return &count
}

// A bound engine must never reach the model, tools or the answer stream
// when the context carries no sealed turn.
func TestAbbreviationGuard_ExecuteBlockedWithoutSeal(t *testing.T) {
	tool := newCountingTool("fake_tool")
	registry := agenttools.NewToolRegistry()
	registry.RegisterTool(tool)
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{Content: "invented answer", Done: true},
	}}}}
	engine := newGuardEngine(t, model, registry)
	engine.RequireAbbreviationTurn(guardBinding())
	answers := finalAnswerCount(engine)

	state, err := engine.Execute(context.Background(), "sess-1", "assist-1", "ATTT là gì", nil)

	require.Error(t, err)
	assert.Nil(t, state)
	assert.Equal(t, 0, model.callCount, "ChatStream must never run without a sealed turn")
	assert.Equal(t, 0, tool.calls, "no tool may execute without a sealed turn")
	assert.EqualValues(t, 0, answers.Load(), "no answer stream may be emitted")
}

// The engine compares the actual Execute arguments against the binding, not
// just the context against a captured value.
func TestAbbreviationGuard_MismatchedMessageIDRejected(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{Content: "answer", Done: true},
	}}}}
	engine := newGuardEngine(t, model, nil)
	ctx, binding := sealedGuardCtx(t)
	engine.RequireAbbreviationTurn(binding)
	answers := finalAnswerCount(engine)

	_, err := engine.Execute(ctx, "sess-1", "other-assistant", "ATTT là gì", nil)

	require.ErrorIs(t, err, types.ErrAbbreviationConflict)
	assert.Equal(t, 0, model.callCount)
	assert.EqualValues(t, 0, answers.Load())
}

// A sealed context for a different turn cannot satisfy the required binding.
func TestAbbreviationGuard_ForeignSealRejected(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{Content: "answer", Done: true},
	}}}}
	engine := newGuardEngine(t, model, nil)
	ctx, binding := sealedGuardCtx(t)
	engine.RequireAbbreviationTurn(binding)
	answers := finalAnswerCount(engine)

	// Reseal under a different assistant message — the context is valid but
	// belongs to another turn.
	foreign := guardBinding()
	foreign.AssistantMessageID = "assist-2"
	res, ok := abbreviation.ResolutionFromContext(ctx)
	require.True(t, ok)
	foreignCtx, err := abbreviation.BindTurn(context.Background(), foreign, res)
	require.NoError(t, err)

	_, err = engine.Execute(foreignCtx, "sess-1", "assist-1", "ATTT là gì", nil)
	require.ErrorIs(t, err, types.ErrAbbreviationConflict)
	assert.Equal(t, 0, model.callCount)
	assert.EqualValues(t, 0, answers.Load())
}

// The finalize fallback (natural stop, max iterations) must fail before the
// model is invoked when the sealed turn is missing.
func TestAbbreviationGuard_FinalizeBlockedWithoutSeal(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{Content: "synthesized", Done: true},
	}}}}
	engine := newGuardEngine(t, model, nil)
	engine.RequireAbbreviationTurn(guardBinding())
	answers := finalAnswerCount(engine)

	err := engine.streamFinalAnswerToEventBus(
		context.Background(), "ATTT là gì", &types.AgentState{}, "sess-1", emptyMessages())

	require.Error(t, err)
	assert.Equal(t, 0, model.callCount)
	assert.EqualValues(t, 0, answers.Load())
}

// Tool calls are refused before any tool executes when the seal is invalid.
func TestAbbreviationGuard_ToolCallsRefusedWithoutSeal(t *testing.T) {
	tool := newCountingTool("fake_tool")
	registry := agenttools.NewToolRegistry()
	registry.RegisterTool(tool)
	engine := newGuardEngine(t, &mockChat{}, registry)
	engine.RequireAbbreviationTurn(guardBinding())

	step := &types.AgentStep{}
	engine.executeToolCalls(context.Background(), &types.ChatResponse{
		ToolCalls: []types.LLMToolCall{{
			Function: types.FunctionCall{Name: "fake_tool", Arguments: `{}`},
		}},
	}, step, 0, "sess-1", "assist-1")

	assert.Equal(t, 0, tool.calls, "tool must not execute without a sealed turn")
	require.Len(t, step.ToolCalls, 1)
	assert.False(t, step.ToolCalls[0].Result.Success)
}

// With a valid seal the bound engine runs normally and the model sees the
// validated mapping as immutable provenance.
func TestAbbreviationGuard_BoundTurnRunsModel(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{Content: "ATTT là An toàn thông tin", Done: true},
	}}}}
	engine := newGuardEngine(t, model, nil)
	ctx, binding := sealedGuardCtx(t)
	engine.RequireAbbreviationTurn(binding)

	state, err := engine.Execute(ctx, "sess-1", "assist-1", "ATTT là gì", nil)

	require.NoError(t, err)
	require.Equal(t, 1, model.callCount)
	require.NotNil(t, state)
	// The last user message carries the system-owned mapping.
	var userMsg string
	for _, m := range model.calls[0] {
		if m.Role == "user" {
			userMsg = m.Content
		}
	}
	assert.Contains(t, userMsg, `<abbreviation_resolution origin="system"`)
	assert.Contains(t, userMsg, "ATTT = An toàn thông tin")
}

// Autonomous engines (Wiki workers, the installer) never set a binding and
// keep running on an unsealed context.
func TestAbbreviationGuard_NoBindingKeepsLifecycle(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{Content: "done", Done: true},
	}}}}
	engine := newGuardEngine(t, model, nil)

	state, err := engine.Execute(context.Background(), "sess-1", "assist-1", "ATTT là gì", nil)

	require.NoError(t, err)
	require.Equal(t, 1, model.callCount)
	assert.True(t, state.IsComplete)
}
