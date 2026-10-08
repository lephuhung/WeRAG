package session

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
)

// The scope clarification turn reaches the client as a tool result carrying
// the card payload, the answer and one completion, and the assistant row
// keeps the payload in its agent steps so the card renders from history.
func TestScopeClarificationTurnStreamsAndPersistsTheCard(t *testing.T) {
	streams := &capturingStreamManager{}
	bus := event.NewEventBus()
	msg := &types.Message{ID: "msg-a", Role: "assistant"}
	h := NewAgentStreamHandler(context.Background(), "s1", "msg-a", "req-1", 7, time.Now(),
		msg, streams, bus, nil, nil, nil)
	h.Subscribe()

	c := &tools.ScopeClarification{
		DisplayType: tools.DocumentScopeClarificationType,
		Query:       "góp ý giúp",
		Documents: []tools.ScopeClarificationDocument{{ID: "ws-1", Handle: "vb1", FileName: "quy-che.docx",
			Role: types.DocumentWorkspaceRoleTarget, Paragraphs: 320, Runes: 40000, Long: true,
			Sections: []tools.ScopeClarificationSection{{Title: "Điều 1. Phạm vi", From: 3, To: 9}}}},
		Tasks:     []tools.ScopeClarificationTask{{Key: tools.ScopeClarifyTaskSummary, Label: "Tóm tắt"}},
		Suggested: tools.ScopeClarificationSuggestion{DocumentID: "ws-1"},
	}
	req := &types.QARequest{Session: &types.Session{ID: "s1", TenantID: 7}, Query: "góp ý giúp", AssistantMessageID: "msg-a"}
	require.NoError(t, service.EmitDocumentScopeClarification(context.Background(), bus, req, c))

	var kinds []types.ResponseType
	var card map[string]interface{}
	for _, e := range streams.events {
		kinds = append(kinds, e.Type)
		if e.Type == types.ResponseTypeToolResult {
			card = e.Data
		}
	}
	require.Equal(t, 1, countType(kinds, types.ResponseTypeComplete), "one terminal event: %v", kinds)
	require.Contains(t, kinds, types.ResponseTypeAnswer)
	require.NotNil(t, card)
	require.Equal(t, tools.DocumentScopeClarificationType, card["display_type"])
	require.Equal(t, "góp ý giúp", card["query"])
	require.NotEmpty(t, card["documents"])

	require.True(t, msg.IsCompleted)
	require.Contains(t, msg.Content, "vb1 · quy-che.docx")
	require.Len(t, msg.AgentSteps, 1)
	require.Len(t, msg.AgentSteps[0].ToolCalls, 1)
	call := msg.AgentSteps[0].ToolCalls[0]
	require.True(t, types.IsPipelineToolCallID(call.ID), "history replays only the answer to the model")
	require.Equal(t, tools.DocumentScopeClarificationType, call.Result.Data["display_type"])
	require.NotEmpty(t, call.Result.Data["documents"])
}
