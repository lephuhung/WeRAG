package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// failingTool always fails with the same validation error, the id varying
// per call like a model retrying with another value.
type failingTool struct {
	agenttools.BaseTool
	calls int
}

func (t *failingTool) Execute(context.Context, json.RawMessage) (*types.ToolResult, error) {
	t.calls++
	return &types.ToolResult{Success: false,
		Error: fmt.Sprintf("parameter 'server_id' must be one of [a, b]; got srv-%d", t.calls)}, nil
}

func offeredNames(opts *chat.ChatOptions) []string {
	if opts == nil {
		return nil
	}
	names := make([]string, 0, len(opts.Tools))
	for _, t := range opts.Tools {
		names = append(names, t.Function.Name)
	}
	return names
}

// A tool failing the same way three times is disabled for the turn: the
// fourth call is not executed, its result says so, and the tool is no
// longer offered after the third failure.
func TestFailureGuardDisablesToolAfterThreeIdenticalFailures(t *testing.T) {
	const name = "discover_mcp_tools"
	responses := make([]mockResponse, 0, 5)
	for i := 1; i <= 4; i++ {
		responses = append(responses, mockResponse{chunks: []types.StreamResponse{{
			ResponseType: types.ResponseTypeAnswer,
			Content:      fmt.Sprintf("trying %d", i),
			ToolCalls: []types.LLMToolCall{{
				ID: fmt.Sprintf("tc%d", i), Type: "function",
				Function: types.FunctionCall{Name: name, Arguments: fmt.Sprintf(`{"server_id":"srv-%d"}`, i)},
			}},
		}}})
	}
	responses = append(responses, mockResponse{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "answer", Done: true, FinishReason: "stop"},
	}})
	model := &mockChat{responses: responses}
	engine := newTestEngine(t, model, withMaxIterations(20))
	engine.eventBus = event.NewEventBus()
	registry := agenttools.NewToolRegistry()
	tool := &failingTool{BaseTool: agenttools.NewBaseTool(name, "test", json.RawMessage(`{"type":"object"}`))}
	registry.RegisterTool(tool)
	registry.RegisterTool(newCountingTool("other_tool"))
	engine.toolRegistry = registry

	state := &types.AgentState{RoundSteps: []types.AgentStep{}}
	messages := []chat.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "start"}}
	_, err := engine.executeLoop(context.Background(), state, "start", messages,
		engine.buildToolsForLLM(), "sess", "msg")
	require.NoError(t, err)

	assert.Equal(t, 3, tool.calls, "the fourth call must not run")
	require.Len(t, state.RoundSteps, 5)
	third := state.RoundSteps[2].ToolCalls[0].Result.Error
	assert.Contains(t, third, "Tool discover_mcp_tools has failed 3 times in this turn with the same error")
	assert.NotContains(t, third, "không còn được cung cấp", "no second stop note")
	fourth := state.RoundSteps[3].ToolCalls[0].Result
	assert.False(t, fourth.Success)
	assert.Contains(t, fourth.Error, toolDisabledMarker)
	assert.Contains(t, fourth.Error, "Do not call it again")

	require.Len(t, model.opts, 5)
	assert.Contains(t, offeredNames(model.opts[2]), name, "offered until the third failure")
	assert.NotContains(t, offeredNames(model.opts[3]), name, "withdrawn after the third failure")
	assert.Contains(t, offeredNames(model.opts[3]), "other_tool")
}

func TestFailureGuardCountsOnlyIdenticalConsecutiveFailures(t *testing.T) {
	var g toolFailureGuard
	_, off := g.record("m", "x", false, "boom 1")
	require.False(t, off)
	_, off = g.record("m", "x", false, "other error")
	require.False(t, off, "a different error starts a new run")
	_, off = g.record("m", "x", false, "other error")
	require.False(t, off)
	_, off = g.record("m", "x", true, "")
	require.False(t, off)
	_, off = g.record("m", "x", false, "other error")
	require.False(t, off, "a success ends the run")
	g.record("m", "x", false, "other error")
	_, off = g.record("m", "x", false, "other error")
	require.True(t, off)
	_, off = g.disabledText("m", "x")
	require.True(t, off)
	_, off = g.disabledText("next-turn", "x")
	require.False(t, off, "a new message id starts over")
}

func TestNormalizeToolError(t *testing.T) {
	assert.Equal(t,
		normalizeToolError("Read failed for 07a20bb1-a662-47cf-9929-06fb5d5b5b5e at page 3"),
		normalizeToolError("read failed for 11111111-2222-3333-4444-555555555555 at page  12"))
	assert.Equal(t, normalizeToolError("chunk a1b2c3d4e5 missing"), normalizeToolError("chunk ffee0099aa missing"))
	assert.NotEqual(t, normalizeToolError("tool not found"), normalizeToolError("timeout"))
}
