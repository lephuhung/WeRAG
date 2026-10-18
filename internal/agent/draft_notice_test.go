package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testDraftNotice = "\n\n---\n**Lưu ý về dự thảo**\n- X là dự thảo.\n"

// withDraftNotice flags any model text naming "doanh nghiệp một người" and
// records what text it was given.
func withDraftNotice(engine *AgentEngine, seen *string) {
	engine.SetDraftNotice(func(_ context.Context, text string) string {
		*seen = text
		if strings.Contains(text, "doanh nghiệp một người") {
			return testDraftNotice
		}
		return ""
	})
}

func TestDraftNoticeOnNaturalStopInOrder(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "Về doanh nghiệp một người…", Done: true, FinishReason: "stop"},
	}}}}
	engine := newTestEngine(t, model)
	engine.toolRegistry = agentRegistryForTest(t, "counting_tool")
	withDomainFooter(engine)
	var seen string
	withDraftNotice(engine, &seen)
	events := recordAnswerEvents(t, engine)

	state := &types.AgentState{RoundSteps: taxSteps()}
	messages := []chat.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "hỏi"}}
	_, err := engine.executeLoop(context.Background(), state, "hỏi", messages, engine.buildToolsForLLM(), "sess", "msg")
	require.NoError(t, err)

	assert.Equal(t, "Về doanh nghiệp một người…", seen, "the check sees the model's text only")
	assert.Equal(t, "Về doanh nghiệp một người…"+testDraftNotice+wantTaxBlock+testDomainFooter, state.FinalAnswer)
	assertFooterLastBeforeDone(t, events())
}

func TestDraftNoticeOnSynthesis(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "Tổng hợp: doanh nghiệp một người."},
		{ResponseType: types.ResponseTypeAnswer, Done: true, FinishReason: "stop"},
	}}}}
	engine := newTestEngine(t, model)
	withDomainFooter(engine)
	var seen string
	withDraftNotice(engine, &seen)
	state := &types.AgentState{}
	require.NoError(t, engine.streamFinalAnswerToEventBus(context.Background(), "q", state, "sess", emptyMessages()))
	assert.Equal(t, "Tổng hợp: doanh nghiệp một người."+testDraftNotice+testDomainFooter, state.FinalAnswer)
}

func TestDraftNoticeSkips(t *testing.T) {
	t.Run("labelled or no mention", func(t *testing.T) {
		engine := newTestEngine(t, &mockChat{})
		var seen string
		withDraftNotice(engine, &seen)
		state := &types.AgentState{FinalAnswer: "Không nhắc."}
		engine.appendDraftNotice(context.Background(), state, "sess", "a1", state.FinalAnswer)
		assert.Equal(t, "Không nhắc.", state.FinalAnswer)
	})
	t.Run("no evidence", func(t *testing.T) {
		engine := newTestEngine(t, &mockChat{})
		var seen string
		withDraftNotice(engine, &seen)
		text := ungroundedAnswer + " doanh nghiệp một người"
		state := &types.AgentState{FinalAnswer: text}
		engine.appendDraftNotice(context.Background(), state, "sess", "a1", text)
		assert.Equal(t, text, state.FinalAnswer)
		assert.Empty(t, seen)
	})
	t.Run("synthesis fallback", func(t *testing.T) {
		model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
			{ResponseType: types.ResponseTypeAnswer, Done: true, FinishReason: "length"},
		}}}}
		engine := newTestEngine(t, model)
		var seen string
		withDraftNotice(engine, &seen)
		state := &types.AgentState{}
		require.NoError(t, engine.streamFinalAnswerToEventBus(context.Background(), "q", state, "sess", emptyMessages()))
		assert.Empty(t, seen, "the code-built fallback is not checked")
	})
}
