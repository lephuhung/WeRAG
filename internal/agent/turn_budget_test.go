package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// toolRound is one model response that calls the probe tool and reports
// tokens of usage.
func toolRound(i, tokens int) mockResponse {
	return mockResponse{chunks: []types.StreamResponse{{
		ResponseType: types.ResponseTypeAnswer,
		ToolCalls: []types.LLMToolCall{{
			ID:       fmt.Sprintf("call-%d", i),
			Type:     "function",
			Function: types.FunctionCall{Name: "probe", Arguments: `{}`},
		}},
		Done:         true,
		FinishReason: "tool_calls",
		Usage:        &types.TokenUsage{PromptTokens: tokens - 100, CompletionTokens: 100, TotalTokens: tokens},
	}}}
}

func collectTurnLimits(bus *event.EventBus) (*[]event.AgentTurnLimitData, *sync.Mutex) {
	var (
		mu     sync.Mutex
		limits []event.AgentTurnLimitData
	)
	bus.On(event.EventAgentTurnLimit, func(_ context.Context, evt event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		limits = append(limits, evt.Data.(event.AgentTurnLimitData))
		return nil
	})
	return &limits, &mu
}

// A plan's per-turn token cap ends the loop between rounds and the answer is
// synthesized from what the turn gathered, the same way as after the last
// iteration — the 46-round turn this cap exists for never gets to round 3.
func TestExecuteLoopStopsAtTurnTokenCap(t *testing.T) {
	model := &mockChat{responses: []mockResponse{
		toolRound(1, 60000),
		toolRound(2, 60000),
		{chunks: []types.StreamResponse{
			{ResponseType: types.ResponseTypeAnswer, Content: "answer from what was found", Done: true, FinishReason: "stop"},
		}},
	}}
	engine := newTestEngine(t, model, withMaxIterations(10), func(cfg *types.AgentConfig) {
		cfg.TurnTokenCap = 100000
	})
	engine.toolRegistry = agenttools.NewToolRegistry()
	probe := newCountingTool("probe")
	engine.toolRegistry.RegisterTool(probe)
	limits, mu := collectTurnLimits(engine.eventBus)

	state := &types.AgentState{RoundSteps: []types.AgentStep{}}
	messages := []chat.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "research this"}}
	_, err := engine.executeLoop(context.Background(), state, "research this", messages, nil, "sess", "msg")
	require.NoError(t, err)

	require.Equal(t, 3, model.callCount, "two rounds, then one synthesis call")
	require.Equal(t, 2, probe.calls)
	require.Equal(t, 2, state.CurrentRound)
	require.True(t, state.IsComplete)
	require.Equal(t, "answer from what was found", state.FinalAnswer)
	require.Equal(t, "none", model.opts[2].ToolChoice, "the synthesis call may not call tools")

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, *limits, 1)
	got := (*limits)[0]
	require.Equal(t, event.TurnLimitTurnBudget, got.Reason)
	require.Equal(t, 2, got.Rounds)
	require.Equal(t, 120000, got.Tokens)
	require.Equal(t, 100000, got.TokenCap)
}

// Without a cap the same rounds run to the iteration limit, which is
// reported as such.
func TestExecuteLoopWithoutTurnCapRunsToMaxIterations(t *testing.T) {
	model := &mockChat{responses: []mockResponse{
		toolRound(1, 60000),
		toolRound(2, 60000),
		{chunks: []types.StreamResponse{
			{ResponseType: types.ResponseTypeAnswer, Content: "done", Done: true, FinishReason: "stop"},
		}},
	}}
	engine := newTestEngine(t, model, withMaxIterations(2))
	engine.toolRegistry = agenttools.NewToolRegistry()
	engine.toolRegistry.RegisterTool(newCountingTool("probe"))
	limits, mu := collectTurnLimits(engine.eventBus)

	state := &types.AgentState{RoundSteps: []types.AgentStep{}}
	messages := []chat.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "q"}}
	_, err := engine.executeLoop(context.Background(), state, "q", messages, nil, "sess", "msg")
	require.NoError(t, err)
	require.Equal(t, 3, model.callCount)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, *limits, 1)
	require.Equal(t, event.TurnLimitMaxIterations, (*limits)[0].Reason)
	require.Zero(t, (*limits)[0].TokenCap)
}

// A natural finish under the cap is untouched: no limit event.
func TestExecuteLoopUnderTurnCapFinishesNaturally(t *testing.T) {
	model := &mockChat{responses: []mockResponse{
		toolRound(1, 10000),
		{chunks: []types.StreamResponse{
			{ResponseType: types.ResponseTypeAnswer, Content: "natural", Done: true, FinishReason: "stop",
				Usage: &types.TokenUsage{TotalTokens: 10000}},
		}},
	}}
	engine := newTestEngine(t, model, func(cfg *types.AgentConfig) { cfg.TurnTokenCap = 100000 })
	engine.toolRegistry = agenttools.NewToolRegistry()
	engine.toolRegistry.RegisterTool(newCountingTool("probe"))
	limits, mu := collectTurnLimits(engine.eventBus)

	state := &types.AgentState{RoundSteps: []types.AgentStep{}}
	messages := []chat.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "q"}}
	_, err := engine.executeLoop(context.Background(), state, "q", messages, nil, "sess", "msg")
	require.NoError(t, err)
	require.Equal(t, "natural", state.FinalAnswer)
	mu.Lock()
	defer mu.Unlock()
	require.Empty(t, *limits)
}
