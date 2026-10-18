package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testDomainFooter = "\n\n---\nThông tin tham khảo, không phải tư vấn pháp lý.\n"

type answerEvent struct {
	id      string
	content string
	done    bool
}

// recordAnswerEvents keeps every final-answer event in emission order.
func recordAnswerEvents(t *testing.T, engine *AgentEngine) func() []answerEvent {
	t.Helper()
	var mu sync.Mutex
	var events []answerEvent
	engine.eventBus.On(event.EventAgentFinalAnswer, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(event.AgentFinalAnswerData)
		require.True(t, ok)
		mu.Lock()
		events = append(events, answerEvent{id: evt.ID, content: data.Content, done: data.Done})
		mu.Unlock()
		return nil
	})
	return func() []answerEvent {
		mu.Lock()
		defer mu.Unlock()
		return append([]answerEvent(nil), events...)
	}
}

// assertFooterLastBeforeDone checks the footer is streamed once, as the
// answer's last block, on the answer's id, right before its Done.
func assertFooterLastBeforeDone(t *testing.T, events []answerEvent) {
	t.Helper()
	footers := 0
	for _, e := range events {
		if e.content == testDomainFooter {
			footers++
		}
	}
	require.Equal(t, 1, footers, "footer exactly once: %+v", events)
	require.GreaterOrEqual(t, len(events), 2)
	done := events[len(events)-1]
	footer := events[len(events)-2]
	assert.True(t, done.done, "Done comes last")
	assert.Equal(t, testDomainFooter, footer.content)
	assert.Equal(t, done.id, footer.id, "footer rides the answer's own id")
}

func withDomainFooter(engine *AgentEngine) {
	engine.SetDomainFooter(func(context.Context) string { return testDomainFooter })
}

func TestDomainFooterOnNaturalStop(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "Câu trả lời.", Done: true, FinishReason: "stop"},
	}}}}
	engine := newTestEngine(t, model)
	engine.toolRegistry = agentRegistryForTest(t, "counting_tool")
	withDomainFooter(engine)
	events := recordAnswerEvents(t, engine)

	state := &types.AgentState{RoundSteps: []types.AgentStep{}}
	messages := []chat.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "hỏi"}}
	_, err := engine.executeLoop(context.Background(), state, "hỏi", messages,
		engine.buildToolsForLLM(), "sess", "msg")
	require.NoError(t, err)

	assertFooterLastBeforeDone(t, events())
	assert.True(t, strings.HasSuffix(state.FinalAnswer, testDomainFooter), state.FinalAnswer)
	assert.Equal(t, 1, strings.Count(state.FinalAnswer, "---"))
}

func TestDomainFooterOnSynthesis(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "Tổng hợp."},
		{ResponseType: types.ResponseTypeAnswer, Done: true, FinishReason: "stop"},
	}}}}
	engine := newTestEngine(t, model)
	withDomainFooter(engine)
	events := recordAnswerEvents(t, engine)

	state := &types.AgentState{}
	require.NoError(t, engine.streamFinalAnswerToEventBus(context.Background(), "q", state, "sess", emptyMessages()))

	assertFooterLastBeforeDone(t, events())
	assert.Equal(t, "Tổng hợp."+testDomainFooter, state.FinalAnswer)
}

func TestDomainFooterSkipsCodeWrittenAnswers(t *testing.T) {
	t.Run("synthesis fallback", func(t *testing.T) {
		model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
			{ResponseType: types.ResponseTypeAnswer, Done: true, FinishReason: "length"},
		}}}}
		engine := newTestEngine(t, model)
		withDomainFooter(engine)
		content := collectAnswer(t, engine)
		state := &types.AgentState{}
		require.NoError(t, engine.streamFinalAnswerToEventBus(context.Background(), "q", state, "sess", emptyMessages()))
		assert.NotEmpty(t, state.FinalAnswer)
		assert.NotContains(t, *content, testDomainFooter)
	})
	t.Run("no evidence, synthesis path", func(t *testing.T) {
		engine := newTestEngine(t, &mockChat{})
		withDomainFooter(engine)
		content := collectAnswer(t, engine)
		state := &types.AgentState{}
		ctx := types.WithGroundingRequired(context.Background())
		require.NoError(t, engine.streamFinalAnswerToEventBus(ctx, "q", state, "sess", emptyMessages()))
		assert.True(t, strings.HasPrefix(state.FinalAnswer, ungroundedAnswer))
		assert.NotContains(t, *content, testDomainFooter)
	})
	t.Run("no evidence, natural stop", func(t *testing.T) {
		engine := newTestEngine(t, &mockChat{})
		withDomainFooter(engine)
		content := collectAnswer(t, engine)
		state := &types.AgentState{FinalAnswer: ungroundedAnswer}
		engine.appendDomainFooter(context.Background(), state, "sess", "answer-1")
		assert.Equal(t, ungroundedAnswer, state.FinalAnswer)
		assert.Empty(t, *content)
	})
	t.Run("empty answer", func(t *testing.T) {
		engine := newTestEngine(t, &mockChat{})
		withDomainFooter(engine)
		state := &types.AgentState{FinalAnswer: "  "}
		engine.appendDomainFooter(context.Background(), state, "sess", "answer-1")
		assert.Equal(t, "  ", state.FinalAnswer)
	})
}
