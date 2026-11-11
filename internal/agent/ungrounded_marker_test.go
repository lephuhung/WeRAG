package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// The grounding gate replaced the answer: the completion carries the marker
// the stream handler stores on the assistant message (admins' ungrounded
// list), with the user's question and the recorded legal request.
func TestCompletionCarriesUngroundedMarkerWhenTheGateFires(t *testing.T) {
	model := &scriptedChat{id: "ungrounded-marker", replies: []*types.ChatResponse{
		{Content: "iPhone 18 Pro chưa ra mắt.", FinishReason: "stop"},
	}}
	engine, log := failureEngine(t, model)
	ctx := types.WithGroundingRequired(context.Background())
	ctx = types.WithUserQuery(ctx, "  Giá iPhone 18 Pro là bao nhiêu? ")
	ctx = types.WithLegalRequestRecorded(ctx, "12/2024/QĐ-UBND")

	state, err := engine.Execute(ctx, "sess-1", "msg-1", "giá iphone 18 pro (viết lại)", nil)
	require.NoError(t, err)
	require.Contains(t, state.FinalAnswer, ungroundedAnswer)
	require.NotNil(t, log.final)
	require.Equal(t, &types.UngroundedMarker{
		Reason: types.UngroundedNoEvidence, Query: "Giá iPhone 18 Pro là bao nhiêu?", LegalRequest: "12/2024/QĐ-UBND",
	}, log.final.Ungrounded)
}

// A normal answer carries no marker.
func TestCompletionHasNoUngroundedMarkerForAnAnswer(t *testing.T) {
	model := &failingChat{steps: []scriptStep{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "Điều 5 quy định về người sử dụng đất."},
		{ResponseType: types.ResponseTypeAnswer, Done: true, FinishReason: "stop"},
	}}}}
	engine, log := failureEngine(t, model)
	_, err := execute(t, engine)
	require.NoError(t, err)
	require.NotNil(t, log.final)
	require.NotEmpty(t, log.final.FinalAnswer)
	require.Nil(t, log.final.Ungrounded)
}

// A failed turn (turn_failure) is no ungrounded question, even on a turn
// that needed evidence.
func TestCompletionHasNoUngroundedMarkerForAFailedTurn(t *testing.T) {
	model := &failingChat{steps: []scriptStep{{startErr: errors.New("x")}}}
	engine, log := failureEngine(t, model)
	engine.turnFailure = types.TurnFailureModelUnavailable
	state := &types.AgentState{FinalAnswer: ungroundedAnswer, Ungrounded: true}
	require.Nil(t, engine.ungroundedMarker(context.Background(), state, "q"))
	engine.turnFailure = ""
	require.NotNil(t, engine.ungroundedMarker(context.Background(), state, "q"))

	ctx := types.WithGroundingRequired(context.Background())
	_, err := engine.Execute(ctx, "sess-1", "msg-1", "q", nil)
	require.NoError(t, err)
	require.NotNil(t, log.final)
	require.NotEmpty(t, engine.turnFailure)
	require.Nil(t, log.final.Ungrounded)
}

// The marker follows the state flag, not the answer text: blocks appended
// after the gate's sentence keep it, a look-alike answer without the flag
// does not get it.
func TestUngroundedMarkerFollowsTheStateFlag(t *testing.T) {
	engine := newTestEngine(t, &failingChat{})
	marked := &types.AgentState{FinalAnswer: ungroundedAnswer + "\n\n---\nKhối thêm vào sau", Ungrounded: true}
	require.NotNil(t, engine.ungroundedMarker(context.Background(), marked, "q"))
	lookAlike := &types.AgentState{FinalAnswer: ungroundedAnswer}
	require.Nil(t, engine.ungroundedMarker(context.Background(), lookAlike, "q"))
}

// The synthesis gate (no evidence after the loop) marks the state.
func TestSynthesisGateMarksTheState(t *testing.T) {
	engine, log := failureEngine(t, &failingChat{})
	ctx := types.WithGroundingRequired(context.Background())
	state := &types.AgentState{}
	require.NoError(t, engine.streamFinalAnswerToEventBus(ctx, "q", state, "sess-1", emptyMessages()))
	require.True(t, state.Ungrounded)
	require.Contains(t, state.FinalAnswer, ungroundedAnswer)
	require.Contains(t, log.answer.String(), ungroundedAnswer)
	require.NotNil(t, engine.ungroundedMarker(ctx, state, "q"))
}

// A synthesized answer replaces whatever came before: the flag is cleared.
func TestReplacedAnswerIsNotMarked(t *testing.T) {
	model := &failingChat{steps: []scriptStep{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "Điều 5 quy định về người sử dụng đất."},
		{ResponseType: types.ResponseTypeAnswer, Done: true, FinishReason: "stop"},
	}}}}
	engine, _ := failureEngine(t, model)
	state := &types.AgentState{Ungrounded: true, RoundSteps: []types.AgentStep{{ToolCalls: []types.ToolCall{{
		Name: "search_knowledge", Result: &types.ToolResult{Success: true, Output: "Điều 5…", Data: map[string]interface{}{"count": 1}}}}}}}
	engine.handleMaxIterations(types.WithGroundingRequired(context.Background()), "q", state, "sess-1", emptyMessages())
	require.NotEmpty(t, state.FinalAnswer)
	require.NotContains(t, state.FinalAnswer, ungroundedAnswer)
	require.False(t, state.Ungrounded)
	require.Nil(t, engine.ungroundedMarker(context.Background(), state, "q"))
}
