package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type handoffWorkspaces struct {
	interfaces.DocumentWorkspaceService
	enabled bool
	docs    []*types.DocumentWorkspace
}

func (w *handoffWorkspaces) DocumentsEnabled() bool { return w.enabled }
func (w *handoffWorkspaces) List(context.Context, uint64, string) ([]*types.DocumentWorkspace, error) {
	return w.docs, nil
}

type handoffMessages struct {
	interfaces.MessageRepository
	msgs []*types.Message
}

func (m *handoffMessages) GetRecentMessagesBySession(context.Context, string, int) ([]*types.Message, error) {
	return m.msgs, nil
}

func handoffTurn(t *testing.T, svc *sessionService, req *types.QARequest) (bool, map[string]interface{}, string) {
	t.Helper()
	bus := event.NewEventBus()
	var data map[string]interface{}
	var final string
	bus.On(event.EventAgentToolResult, func(_ context.Context, e event.Event) error {
		if d, ok := e.Data.(event.AgentToolResultData); ok && d.ToolName == tools.AgentHandoffType {
			data = d.Data
		}
		return nil
	})
	bus.On(event.EventAgentFinalAnswer, func(_ context.Context, e event.Event) error {
		if d, ok := e.Data.(event.AgentFinalAnswerData); ok && d.Content != "" {
			final = d.Content
		}
		return nil
	})
	handled, err := svc.agentHandoff(context.Background(), req, bus, false)
	if err != nil {
		t.Fatal(err)
	}
	return handled, data, final
}

func TestAgentHandoffGate(t *testing.T) {
	ws := &handoffWorkspaces{enabled: true}
	svc := &sessionService{documentWorkspaces: ws, messageRepo: &handoffMessages{msgs: []*types.Message{
		{ID: "u1", Role: "user", Content: "Tôi cần tổ chức tập huấn, kế hoạch nên có gì?"},
		{ID: "a1", Role: "assistant", Content: "Một báo cáo nên có…"},
		{ID: "u2", Role: "user", Content: "Giờ tạo cho tôi file Word nhé"},
	}}}
	req := func() *types.QARequest {
		return &types.QARequest{Query: "Giờ tạo cho tôi file Word nhé", Channel: types.ChannelWeb, UserMessageID: "u2",
			Session: &types.Session{ID: "s1", TenantID: 7}, AssistantMessageID: "m1",
			CustomAgent: &types.CustomAgent{ID: types.BuiltinSmartReasoningID}}
	}

	handled, data, final := handoffTurn(t, svc, req())
	if !handled || data["target_agent_id"] != types.BuiltinDocumentAssistantID || data["template_id"] != "ke-hoach" ||
		!strings.Contains(final, "Soạn thảo văn bản") {
		t.Fatalf("handled=%v data=%v final=%q", handled, data, final)
	}

	for name, mutate := range map[string]func(*types.QARequest){
		"document assistant": func(r *types.QARequest) { r.CustomAgent.ID = types.BuiltinDocumentAssistantID },
		"im / mcp turn":      func(r *types.QARequest) { r.Channel = "" },
		"embed":              func(r *types.QARequest) { r.Channel = "embed" },
		"template request":   func(r *types.QARequest) { r.DocumentTemplate = &types.DocumentTemplateRequest{} },
		"not a file request": func(r *types.QARequest) { r.Query = "Word là gì?" },
		"a file sent along":  func(r *types.QARequest) { r.Attachments = types.MessageAttachments{{ID: "a", FileName: "kh.docx"}} },
		"an image sent":      func(r *types.QARequest) { r.ImageURLs = []string{"resource://img"} },
	} {
		r := req()
		mutate(r)
		if handled, _, _ := handoffTurn(t, svc, r); handled {
			t.Errorf("%s: no card expected", name)
		}
	}
	ws.docs = []*types.DocumentWorkspace{{ID: "d1"}}
	if handled, _, _ := handoffTurn(t, svc, req()); handled {
		t.Error("a session with documents (Word add-in, pinned web session): no card")
	}
	ws.docs, ws.enabled = nil, false
	if handled, _, _ := handoffTurn(t, svc, req()); handled {
		t.Error("documents disabled on the server: no card")
	}
}

func TestTemplateConversation(t *testing.T) {
	long := strings.Repeat("a", templateConversationMsgRunes+500)
	svc := &sessionService{messageRepo: &handoffMessages{msgs: []*types.Message{
		{ID: "u1", Role: "user", Content: "Kế hoạch nên có gì?"},
		{ID: "a1", Role: "assistant", Content: "<think>nghĩ</think>Nên có mục đích, nội dung."},
		{ID: "u2", Role: "user", Content: "Chốt: 3 lớp, 40 học viên"},
		{ID: "a2", Role: "assistant", Content: long},
		{ID: "u3", Role: "user", Content: "Giờ tạo file Word kế hoạch"},
		{ID: "a3", Role: "assistant", Content: "Agent đang dùng không tạo được file Word…", AgentSteps: types.AgentSteps{{ToolCalls: []types.ToolCall{{
			ID: types.PipelineToolCallIDPrefix + "handoff-a3", Name: tools.AgentHandoffType, Result: &types.ToolResult{Success: true}}}}}},
		{ID: "u4", Role: "user", Content: "Soạn kế hoạch từ nội dung đã trao đổi"},
		{ID: "a4", Role: "assistant", Content: ""},
	}}}
	got := svc.templateConversation(context.Background(), &types.QARequest{Session: &types.Session{ID: "s1"}, UserMessageID: "u4", AssistantMessageID: "a4"})
	var roles []string
	for _, tt := range got {
		roles = append(roles, tt.Role+":"+tt.Text[:min(len(tt.Text), 12)])
	}
	want := []string{"user:Kế hoạch ", "assistant:Nên có mụ", "user:Chốt: 3 l", "assistant:aaaaaaaaaaaa", "user:Giờ tạo "}
	if len(got) != len(want) {
		t.Fatalf("got %v", roles)
	}
	for i := range want {
		if !strings.HasPrefix(roles[i], want[i][:min(len(want[i]), len(roles[i]))]) {
			t.Fatalf("got %v, want %v", roles, want)
		}
	}
	if strings.Contains(got[1].Text, "nghĩ") || len([]rune(got[3].Text)) != templateConversationMsgRunes {
		t.Fatalf("think tags or cut: %q / %d", got[1].Text, len([]rune(got[3].Text)))
	}
}
