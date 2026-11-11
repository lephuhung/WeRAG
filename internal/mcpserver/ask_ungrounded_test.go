package mcpserver

import (
	"context"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type scriptedSessionService struct {
	interfaces.SessionService
	events []event.Event
}

func (s *scriptedSessionService) emit(ctx context.Context, bus *event.EventBus) error {
	for _, evt := range s.events {
		_ = bus.Emit(ctx, evt)
	}
	return nil
}

func (s *scriptedSessionService) AgentQA(ctx context.Context, _ *types.QARequest, bus *event.EventBus) error {
	return s.emit(ctx, bus)
}

func (s *scriptedSessionService) KnowledgeQA(ctx context.Context, _ *types.QARequest, bus *event.EventBus) error {
	return s.emit(ctx, bus)
}

type lastMessageService struct {
	interfaces.MessageService
	mu   sync.Mutex
	last *types.Message
}

func (s *lastMessageService) CreateMessage(_ context.Context, m *types.Message) (*types.Message, error) {
	c := *m
	c.ID = m.Role + "-id"
	return &c, nil
}

func (s *lastMessageService) UpdateMessage(_ context.Context, m *types.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := *m
	s.last = &c
	return nil
}

func askMarker(t *testing.T, agentMode bool, events ...event.Event) *types.UngroundedMarker {
	t.Helper()
	messages := &lastMessageService{}
	s := &Server{sessionService: &scriptedSessionService{events: events}, messageService: messages}
	agent := &types.CustomAgent{}
	if agentMode {
		agent.Config.AgentMode = types.AgentModeSmartReasoning
	}
	if _, _, err := s.runQA(context.Background(), &types.Session{ID: "s1", TenantID: 1}, agent, "Thủ tục cấp sổ đỏ?", nil); err != nil {
		t.Fatalf("runQA: %v", err)
	}
	if messages.last == nil {
		t.Fatal("assistant message not stored")
	}
	return messages.last.ExecutionContext.Ungrounded
}

// MCP ask stores the completion's marker and marks a quick answer whose
// retrieval found nothing; a failed turn stays unmarked.
func TestAskStoresUngroundedMarker(t *testing.T) {
	gate := types.NewUngroundedMarker(types.UngroundedNoEvidence, "Giá iPhone 18?", "")
	got := askMarker(t, true,
		event.Event{Type: event.EventAgentFinalAnswer, Data: event.AgentFinalAnswerData{Content: "không tìm thấy căn cứ", Done: true}},
		event.Event{Type: event.EventAgentComplete, Data: event.AgentCompleteData{Ungrounded: gate}},
	)
	if got == nil || *got != *gate {
		t.Fatalf("agent marker = %+v", got)
	}

	if got := askMarker(t, true,
		event.Event{Type: event.EventAgentFinalAnswer, Data: event.AgentFinalAnswerData{Content: "trả lời", Done: true}},
		event.Event{Type: event.EventAgentComplete, Data: event.AgentCompleteData{}},
	); got != nil {
		t.Fatalf("a normal answer got a marker: %+v", got)
	}

	got = askMarker(t, false,
		event.Event{Type: event.EventAgentFinalAnswer, Data: event.AgentFinalAnswerData{Content: "Xin lỗi", Done: true, IsFallback: true}},
	)
	if got == nil || got.Reason != types.UngroundedNoEvidence || got.Query != "Thủ tục cấp sổ đỏ?" {
		t.Fatalf("quick-answer marker = %+v", got)
	}

	if got := askMarker(t, true,
		event.Event{Type: event.EventAgentFinalAnswer, Data: event.AgentFinalAnswerData{Content: "một phần"}},
		event.Event{Type: event.EventError, Data: event.ErrorData{Error: types.ModelUnavailableMessage, ErrorCode: types.TurnFailureModelUnavailable}},
		event.Event{Type: event.EventAgentComplete, Data: event.AgentCompleteData{Ungrounded: gate}},
	); got != nil {
		t.Fatalf("a failed turn got a marker: %+v", got)
	}
}
