package session

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type contextCapturingMessageService struct {
	interfaces.MessageService
	updates []types.MessageExecutionContext
}

func (s *contextCapturingMessageService) UpdateMessage(_ context.Context, m *types.Message) error {
	s.updates = append(s.updates, m.ExecutionContext)
	return nil
}

func (s *contextCapturingMessageService) IndexMessageToKB(context.Context, string, string, string, string) {
}

func (s *contextCapturingMessageService) last() types.MessageExecutionContext {
	return s.updates[len(s.updates)-1]
}

func quickAnswerTurn(t *testing.T, message *types.Message, query string) *contextCapturingMessageService {
	t.Helper()
	bus := event.NewEventBus()
	NewAgentStreamHandler(context.Background(), "s1", "m1", "req1", 1, time.Now(),
		message, &completionEventRecorder{}, bus, nil, nil, nil).Subscribe()
	stub := &contextCapturingMessageService{}
	h := &Handler{messageService: stub}
	h.completeQuickAnswerTurn(context.Background(), &sseStreamContext{eventBus: bus, assistantMessage: message}, query, "")
	return stub
}

// Quick answer: retrieval found nothing and the fallback answered → the
// stored message carries the no_evidence marker with the question.
func TestQuickAnswerFallbackPersistsUngroundedMarker(t *testing.T) {
	message := &types.Message{ID: "m1", SessionID: "s1", Role: "assistant", Content: "Xin lỗi…", IsFallback: true}
	stub := quickAnswerTurn(t, message, "Thủ tục cấp sổ đỏ?")
	require.Equal(t, &types.UngroundedMarker{Reason: types.UngroundedNoEvidence, Query: "Thủ tục cấp sổ đỏ?"},
		stub.last().Ungrounded)
}

func TestQuickAnswerWithEvidenceHasNoUngroundedMarker(t *testing.T) {
	message := &types.Message{ID: "m1", SessionID: "s1", Role: "assistant", Content: "Theo Điều 5…"}
	stub := quickAnswerTurn(t, message, "Điều 5?")
	require.Nil(t, stub.last().Ungrounded)
}

// A failed quick-answer turn is no ungrounded question.
func TestQuickAnswerFailedTurnHasNoUngroundedMarker(t *testing.T) {
	message := &types.Message{ID: "m1", SessionID: "s1", Role: "assistant", IsFallback: true,
		ExecutionContext: types.MessageExecutionContext{TurnFailure: types.TurnFailureModelUnavailable}}
	stub := quickAnswerTurn(t, message, "q")
	require.Nil(t, stub.last().Ungrounded)
}

// Agent path: the completion's marker is stored on the message; a failure
// recorded before (or after, see finishUngroundedMarker) drops it.
func TestHandleCompleteStoresUngroundedMarker(t *testing.T) {
	marker := types.NewUngroundedMarker(types.UngroundedLegalDocumentMissing, "Nghị định 99/2099/NĐ-CP", "99/2099/NĐ-CP")
	h, message := newCheckpointHandler(t, &stubShellRunner{}, stubSandboxIDLookup{})
	require.NoError(t, h.handleComplete(context.Background(), event.Event{Data: event.AgentCompleteData{
		MessageID: "m1", FinalAnswer: "card", Ungrounded: marker,
	}}))
	require.Equal(t, marker, message.ExecutionContext.Ungrounded)

	h, message = newCheckpointHandler(t, &stubShellRunner{}, stubSandboxIDLookup{})
	message.ExecutionContext.TurnFailure = types.TurnFailureModelUnavailable
	require.NoError(t, h.handleComplete(context.Background(), event.Event{Data: event.AgentCompleteData{
		MessageID: "m1", Ungrounded: marker,
	}}))
	require.Nil(t, message.ExecutionContext.Ungrounded)

	h, message = newCheckpointHandler(t, &stubShellRunner{}, stubSandboxIDLookup{})
	require.NoError(t, h.handleComplete(context.Background(), completeEvent()))
	require.Nil(t, message.ExecutionContext.Ungrounded)
}

func TestFinishUngroundedMarker(t *testing.T) {
	m := &types.Message{ExecutionContext: types.MessageExecutionContext{
		Ungrounded: &types.UngroundedMarker{Reason: types.UngroundedNoEvidence}}}
	finishUngroundedMarker(m, " câu hỏi ")
	require.Equal(t, "câu hỏi", m.ExecutionContext.Ungrounded.Query)

	m.ExecutionContext.TurnFailure = types.TurnFailureModelInterrupted
	finishUngroundedMarker(m, "x")
	require.Nil(t, m.ExecutionContext.Ungrounded)
}
