package im

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// markerMessageService keeps the last stored assistant message.
type markerMessageService struct {
	interfaces.MessageService
	mu   sync.Mutex
	last *types.Message
}

func (s *markerMessageService) CreateMessage(_ context.Context, msg *types.Message) (*types.Message, error) {
	created := *msg
	created.ID = created.Role + "-message"
	return &created, nil
}

func (s *markerMessageService) UpdateMessage(_ context.Context, msg *types.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if msg.Role == "assistant" {
		copied := *msg
		s.last = &copied
	}
	return nil
}

func (s *markerMessageService) marker() *types.UngroundedMarker {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		return nil
	}
	return s.last.ExecutionContext.Ungrounded
}

// runIMTurn runs one turn through the stream or the non-stream consumer and
// returns what was stored on the assistant message.
func runIMTurn(t *testing.T, stream, agent bool, events []event.Event) *types.UngroundedMarker {
	t.Helper()
	messages := &markerMessageService{}
	service := &Service{
		sessionService: &exitSessionService{run: func(ctx context.Context, bus *event.EventBus) error {
			for _, evt := range events {
				_ = bus.Emit(ctx, evt)
			}
			return nil
		}},
		messageService: messages,
		streamManager:  &fullOutputStreamManager{},
	}
	var customAgent *types.CustomAgent
	if agent {
		customAgent = &types.CustomAgent{Config: types.CustomAgentConfig{AgentMode: types.AgentModeSmartReasoning}}
	}
	session := &types.Session{ID: "session"}
	if stream {
		_ = service.handleMessageStream(context.Background(),
			&IncomingMessage{Platform: PlatformTelegram, UserID: "user", Content: "Thủ tục cấp sổ đỏ?"},
			session, customAgent, nil, nil, nil, &recordingStreamSender{}, nil, "user", nil)
	} else {
		_, _ = service.runQA(context.Background(), session, "Thủ tục cấp sổ đỏ?", customAgent, nil, nil, nil, "user", nil)
	}
	return messages.marker()
}

// IM channels (Telegram) store the completion's marker and mark a quick
// answer whose retrieval found nothing, like the web chat; a failed turn
// stays unmarked.
func TestIMStoresUngroundedMarker(t *testing.T) {
	gate := types.NewUngroundedMarker(types.UngroundedLegalDocumentMissing, "Nghị định 99/2099/NĐ-CP?", "99/2099/NĐ-CP")
	failure := event.Event{Type: event.EventError, Data: event.ErrorData{
		Error: types.ModelUnavailableMessage, ErrorCode: types.TurnFailureModelUnavailable}}
	for _, stream := range []bool{false, true} {
		name := map[bool]string{false: "runQA", true: "stream"}[stream]
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				got := runIMTurn(t, stream, true, []event.Event{
					{Type: event.EventAgentFinalAnswer, Data: event.AgentFinalAnswerData{Content: "thẻ", Done: true}},
					{Type: event.EventAgentComplete, Data: event.AgentCompleteData{FinalAnswer: "thẻ", Ungrounded: gate}},
				})
				if got == nil || *got != *gate {
					t.Fatalf("agent marker = %+v", got)
				}
			})
			synctest.Test(t, func(t *testing.T) {
				got := runIMTurn(t, stream, true, []event.Event{
					{Type: event.EventAgentFinalAnswer, Data: event.AgentFinalAnswerData{Content: "trả lời", Done: true}},
					{Type: event.EventAgentComplete, Data: event.AgentCompleteData{FinalAnswer: "trả lời"}},
				})
				if got != nil {
					t.Fatalf("a normal answer got a marker: %+v", got)
				}
			})
			synctest.Test(t, func(t *testing.T) {
				got := runIMTurn(t, stream, false, []event.Event{
					{Type: event.EventAgentFinalAnswer, Data: event.AgentFinalAnswerData{Content: "Xin lỗi", Done: true, IsFallback: true}},
				})
				if got == nil || got.Reason != types.UngroundedNoEvidence || got.Query != "Thủ tục cấp sổ đỏ?" {
					t.Fatalf("quick-answer marker = %+v", got)
				}
			})
			synctest.Test(t, func(t *testing.T) {
				got := runIMTurn(t, stream, false, []event.Event{
					{Type: event.EventAgentFinalAnswer, Data: event.AgentFinalAnswerData{Content: "Xin lỗi", IsFallback: true}},
					failure,
				})
				if got != nil {
					t.Fatalf("a failed turn got a marker: %+v", got)
				}
			})
		})
	}
}

func TestApplyIMCompleteDataStoresMarkerUnlessFailed(t *testing.T) {
	gate := types.QuickAnswerUngroundedMarker("q")
	msg := &types.Message{ID: "m1"}
	applyIMCompleteDataToMessage(msg, event.AgentCompleteData{MessageID: "m1", Ungrounded: gate})
	if msg.ExecutionContext.Ungrounded != gate {
		t.Fatalf("marker not stored")
	}
	failed := &types.Message{ID: "m1", ExecutionContext: types.MessageExecutionContext{TurnFailure: types.TurnFailureModelInterrupted}}
	applyIMCompleteDataToMessage(failed, event.AgentCompleteData{MessageID: "m1", Ungrounded: gate})
	if failed.ExecutionContext.Ungrounded != nil {
		t.Fatalf("a failed turn got a marker")
	}
}
