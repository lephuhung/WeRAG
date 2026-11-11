package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/agent"
	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// streamStep answers one model call: refused, or chunks.
type streamStep struct {
	err    error
	chunks []types.StreamResponse
}

type scriptedModel struct {
	steps []streamStep
	calls int
}

func (m *scriptedModel) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	i := m.calls
	m.calls++
	if i >= len(m.steps) {
		return nil, errors.New("API request failed with status 400: no more steps")
	}
	if m.steps[i].err != nil {
		return nil, m.steps[i].err
	}
	ch := make(chan types.StreamResponse, len(m.steps[i].chunks))
	for _, c := range m.steps[i].chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}
func (m *scriptedModel) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	return nil, errors.New("unused")
}
func (m *scriptedModel) GetModelName() string { return "scripted" }
func (m *scriptedModel) GetModelID() string   { return "scripted" }

// runFailingTurn drives a real agent engine into the stream handler, the
// path a live agent turn takes, and returns the stream events and the
// message as the handler leaves it for persistence.
func runFailingTurn(t *testing.T, model chat.Chat) ([]interfaces.StreamEvent, *types.Message) {
	t.Helper()
	bus := event.NewEventBus()
	streams := &capturingStreamManager{}
	msg := &types.Message{ID: "msg-1", SessionID: "sess-1", Role: "assistant"}
	h := NewAgentStreamHandler(context.Background(), "sess-1", "msg-1", "req-1", 1, time.Now(),
		msg, streams, bus, nil, nil, nil)
	h.Subscribe()

	engine := agent.NewAgentEngine(&types.AgentConfig{MaxIterations: 5}, model,
		agenttools.NewToolRegistry(), bus, nil, nil, "sess-1", "")
	require.NotNil(t, engine)
	_, err := engine.Execute(context.Background(), "sess-1", "msg-1", "Điều 5 quy định gì?", nil)
	require.NoError(t, err, "a model failure is handled inside the turn")
	return streams.events, msg
}

func eventTypes(events []interfaces.StreamEvent) []types.ResponseType {
	out := make([]types.ResponseType, 0, len(events))
	for _, e := range events {
		if len(out) > 0 && out[len(out)-1] == e.Type && e.Type == types.ResponseTypeAnswer {
			continue
		}
		out = append(out, e.Type)
	}
	return out
}

// The live symptom: the error used to land after `complete`, which closes
// the SSE stream, so the client saw an empty answer. Now the notice is the
// last thing before `complete`, the message is marked and has no content.
func TestAgentTurnModelFailureReachesStreamBeforeComplete(t *testing.T) {
	model := &scriptedModel{steps: []streamStep{{err: errors.New("API request failed with status 400: bad request")}}}
	events, msg := runFailingTurn(t, model)

	require.Equal(t, []types.ResponseType{types.ResponseTypeError, types.ResponseTypeComplete}, eventTypes(events))
	errEvt := events[0]
	require.True(t, errEvt.Done)
	require.Equal(t, types.ModelUnavailableMessage, errEvt.Content)
	require.Equal(t, types.TurnFailureModelUnavailable, errEvt.Data["error_code"])
	require.NotContains(t, errEvt.Content, "400")
	require.Equal(t, types.TurnFailureModelUnavailable, events[1].Data["failure"])

	require.True(t, msg.IsCompleted)
	require.Empty(t, msg.Content)
	require.Equal(t, types.TurnFailureModelUnavailable, msg.ExecutionContext.TurnFailure)
	require.True(t, msg.TurnFailed())
}

// Mid-stream: the partial text and the note are both streamed and saved,
// and the turn is marked interrupted.
func TestAgentTurnMidStreamFailureKeepsPartialAndNote(t *testing.T) {
	model := &scriptedModel{steps: []streamStep{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "Điều 5 quy định về người sử dụng đất.\n\n"},
		{ResponseType: types.ResponseTypeError, Content: "unexpected EOF", Done: true},
	}}}}
	events, msg := runFailingTurn(t, model)

	require.Equal(t, []types.ResponseType{
		types.ResponseTypeAnswer, types.ResponseTypeError, types.ResponseTypeComplete,
	}, eventTypes(events))
	streamed := ""
	for _, e := range events {
		if e.Type == types.ResponseTypeAnswer {
			streamed += e.Content
		}
	}
	require.Equal(t, "Điều 5 quy định về người sử dụng đất.\n\n"+types.ModelInterruptedNote, streamed)
	require.Equal(t, streamed, msg.Content, "reload shows what the stream showed")
	require.Equal(t, types.TurnFailureModelInterrupted, msg.ExecutionContext.TurnFailure)
}

// An error event without a failure code leaves the message unmarked.
func TestAgentStreamHandlerPlainErrorDoesNotMarkFailure(t *testing.T) {
	msg := &types.Message{}
	h := NewAgentStreamHandler(context.Background(), "sess-1", "msg-1", "req-1", 1, time.Now(),
		msg, &capturingStreamManager{}, nil, nil, nil, nil)
	require.NoError(t, h.handleError(context.Background(), event.Event{
		Type: event.EventError, Data: event.ErrorData{Stage: "abbreviation_resolution", Error: "x"},
	}))
	require.False(t, msg.TurnFailed())
}
