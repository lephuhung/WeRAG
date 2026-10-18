package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

func toolRoundResponse(i int) mockResponse {
	return mockResponse{chunks: []types.StreamResponse{{
		ResponseType: types.ResponseTypeAnswer,
		Content:      fmt.Sprintf("reading more %d", i),
		ToolCalls: []types.LLMToolCall{{
			ID:       fmt.Sprintf("tc%d", i),
			Type:     "function",
			Function: types.FunctionCall{Name: "counting_tool", Arguments: fmt.Sprintf(`{"page":%d}`, i)},
		}},
	}}}
}

func nudgeCount(msgs []chat.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == "user" {
			n += strings.Count(m.Content, wrapUpNudgeText)
		}
	}
	return n
}

// A model that keeps calling a tool sees the wrap-up nudge first in the 9th
// call, exactly once, and in every later call of the turn still once.
func TestExecuteLoopWrapUpNudgeAfterRoundEight(t *testing.T) {
	const toolRounds = 10
	responses := make([]mockResponse, 0, toolRounds+1)
	for i := 1; i <= toolRounds; i++ {
		responses = append(responses, toolRoundResponse(i))
	}
	responses = append(responses, mockResponse{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "answer", Done: true, FinishReason: "stop"},
	}})
	model := &mockChat{responses: responses}
	engine := newTestEngine(t, model, withMaxIterations(20))
	engine.eventBus = event.NewEventBus()
	engine.toolRegistry = agentRegistryForTest(t, "counting_tool")

	state := &types.AgentState{RoundSteps: []types.AgentStep{}}
	messages := []chat.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "start"}}
	_, err := engine.executeLoop(context.Background(), state, "start", messages,
		engine.buildToolsForLLM(), "sess", "msg")
	require.NoError(t, err)
	require.Len(t, model.calls, toolRounds+1)

	for i, call := range model.calls {
		want := 0
		if i >= wrapUpNudgeRound {
			want = 1
		}
		assert.Equalf(t, want, nudgeCount(call), "call %d", i+1)
	}
	// the nudge follows round 8's tool result, after the original task
	ninth := model.calls[wrapUpNudgeRound]
	last := ninth[len(ninth)-1]
	assert.Equal(t, "user", last.Role)
	assert.Equal(t, wrapUpNudgeText, last.Content)
	assert.Equal(t, "tool", ninth[len(ninth)-2].Role)
	// nothing of it reaches the stored steps
	for _, step := range state.RoundSteps {
		assert.Empty(t, step.UserMessagesBefore)
	}
}

func TestAppendWrapUpNudgeSkipsShortAndUnlimitedTurns(t *testing.T) {
	for _, max := range []int{wrapUpNudgeRound, 5, types.UnlimitedMaxIterations} {
		engine := newTestEngine(t, &mockChat{}, withMaxIterations(max))
		msgs := []chat.Message{{Role: "user", Content: "q"}, {Role: "tool", Content: "r"}}
		state := &types.AgentState{CurrentRound: wrapUpNudgeRound}
		assert.Falsef(t, engine.appendWrapUpNudge(context.Background(), state, &msgs), "max=%d", max)
		assert.Len(t, msgs, 2)
	}

	engine := newTestEngine(t, &mockChat{}, withMaxIterations(20))
	msgs := []chat.Message{{Role: "user", Content: "q"}, {Role: "tool", Content: "r"}}
	early := &types.AgentState{CurrentRound: wrapUpNudgeRound - 1}
	assert.False(t, engine.appendWrapUpNudge(context.Background(), early, &msgs))
	// after a steered user message the nudge rides on it: no two user
	// messages in a row
	msgs = append(msgs, chat.Message{Role: "user", Content: "steer"})
	state := &types.AgentState{CurrentRound: wrapUpNudgeRound}
	require.True(t, engine.appendWrapUpNudge(context.Background(), state, &msgs))
	require.Len(t, msgs, 3)
	assert.Equal(t, "steer\n\n"+wrapUpNudgeText, msgs[2].Content)
	state.CurrentRound++
	assert.False(t, engine.appendWrapUpNudge(context.Background(), state, &msgs), "once per turn")
}
