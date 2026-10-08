package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// clarifySources is a session holding one source document of lines.
type clarifySources struct {
	fakeDocumentWorkspaces
	text string
}

func (f *clarifySources) SourceText(context.Context, uint64, string, string) (*types.DocumentWorkspaceText, *types.DocumentWorkspace, error) {
	return &types.DocumentWorkspaceText{WorkspaceID: f.ws.ID, Content: f.text}, f.ws, nil
}

func newClarifySources(lines int) *clarifySources {
	var b strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&b, "Dòng %d: số liệu thu chi của đơn vị trong kỳ báo cáo, chi tiết theo từng nguồn vốn được giao.\n", i)
	}
	return &clarifySources{
		fakeDocumentWorkspaces: fakeDocumentWorkspaces{ws: &types.DocumentWorkspace{ID: "ws-src", SessionID: "s", FileName: "so-lieu.pdf",
			Position: 1, Role: types.DocumentWorkspaceRoleSource, TextStatus: types.DocumentSourceTextReady}},
		text: b.String(),
	}
}

type recordedEvents struct{ types []event.EventType }

func recordingBus(rec *recordedEvents) *event.EventBus {
	bus := event.NewEventBus()
	for _, tp := range []event.EventType{event.EventAgentToolResult, event.EventAgentFinalAnswer, event.EventAgentComplete} {
		bus.On(tp, func(_ context.Context, evt event.Event) error {
			rec.types = append(rec.types, evt.Type)
			return nil
		})
	}
	return bus
}

func clarifyRequest(sessionID, query string, agent *types.CustomAgent) *types.QARequest {
	return &types.QARequest{Session: &types.Session{ID: sessionID, TenantID: 7}, Query: query,
		AssistantMessageID: "msg-a", CustomAgent: agent}
}

// The gate answers the turn itself (tool result, answer, completion) and
// tells AgentQA to stop before the sandbox and the engine.
func TestDocumentScopeClarificationAnswersTheTurn(t *testing.T) {
	svc := &sessionService{documentWorkspaces: newClarifySources(400)}
	rec := &recordedEvents{}
	asked, err := svc.documentScopeClarification(context.Background(), clarifyRequest("s-svc-1", "góp ý giúp", nil), recordingBus(rec))
	require.NoError(t, err)
	require.True(t, asked)
	require.Equal(t, []event.EventType{event.EventAgentToolResult, event.EventAgentFinalAnswer, event.EventAgentComplete}, rec.types)
}

func TestDocumentScopeClarificationLetsTheAgentRun(t *testing.T) {
	off := false
	cases := map[string]struct {
		svc   *sessionService
		query string
		agent *types.CustomAgent
	}{
		"short source":     {&sessionService{documentWorkspaces: newClarifySources(20)}, "góp ý giúp", nil},
		"concrete request": {&sessionService{documentWorkspaces: newClarifySources(400)}, "số liệu thu chi kỳ này bao nhiêu", nil},
		"config off": {&sessionService{documentWorkspaces: newClarifySources(400)}, "góp ý giúp",
			&types.CustomAgent{Config: types.CustomAgentConfig{AskScopeForLongDocuments: &off}}},
		"no editor": {&sessionService{}, "góp ý giúp", nil},
	}
	for name, tc := range cases {
		rec := &recordedEvents{}
		asked, err := tc.svc.documentScopeClarification(context.Background(), clarifyRequest("s-svc-2", tc.query, tc.agent), recordingBus(rec))
		require.NoError(t, err, name)
		require.False(t, asked, name)
		require.Empty(t, rec.types, name)
	}
}

// Choosing a scope (the card's confirm) counts as the answer for its
// documents and task.
func TestDocumentScopeSetMarksTheClarificationAnswered(t *testing.T) {
	src := newClarifySources(400)
	svc := &sessionService{documentWorkspaces: src}
	scopes := NewDocumentScopeService(src)
	_, err := scopes.Set(context.Background(), 7, "s-svc-3", &types.DocumentScope{DocumentIDs: []string{"vb1"}, Task: types.DocumentScopeTaskSummary})
	require.NoError(t, err)
	scopes.Clear(context.Background(), "s-svc-3")
	asked, err := svc.documentScopeClarification(context.Background(), clarifyRequest("s-svc-3", "tóm tắt", nil), recordingBus(&recordedEvents{}))
	require.NoError(t, err)
	require.False(t, asked, "answered for vb1 + summary")
	require.NotNil(t, tools.DocumentScopeClarification(context.Background(), src, 7, "s-svc-4",
		tools.ScopeClarificationInput{Query: "tóm tắt"}), "another session still asks")
}
