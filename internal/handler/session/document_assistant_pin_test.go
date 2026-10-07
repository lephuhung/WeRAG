package session

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestPinDocumentAssistant(t *testing.T) {
	ws := &routeWorkspaces{enabled: true, bySess: map[string]*types.DocumentWorkspace{
		"with-doc": {SessionID: "with-doc", FileName: "a.docx", Status: types.DocumentWorkspaceStatusOpen},
		"closed":   {SessionID: "closed", FileName: "b.docx", Status: types.DocumentWorkspaceStatusClosed},
	}}
	h := &Handler{documentWorkspaces: ws}
	ctx := context.Background()
	cases := []struct {
		session, agent, want string
	}{
		{"with-doc", "builtin-smart-reasoning", types.BuiltinDocumentAssistantID},
		{"with-doc", "", types.BuiltinDocumentAssistantID},
		{"with-doc", types.BuiltinDocumentAssistantID, types.BuiltinDocumentAssistantID},
		{"no-doc", "builtin-smart-reasoning", "builtin-smart-reasoning"},
		{"closed", "builtin-smart-reasoning", types.BuiltinDocumentAssistantID}, // a closed document is still the session's document
	}
	for _, c := range cases {
		got := h.pinDocumentAssistant(ctx, &types.Session{ID: c.session, TenantID: 1}, c.agent)
		if got != c.want {
			t.Errorf("session %s agent %q: got %q, want %q", c.session, c.agent, got, c.want)
		}
	}
	ws.enabled = false
	if got := h.pinDocumentAssistant(ctx, &types.Session{ID: "with-doc"}, "x"); got != "x" {
		t.Errorf("editor disabled: got %q", got)
	}
	if got := (&Handler{}).pinDocumentAssistant(ctx, &types.Session{ID: "with-doc"}, "x"); got != "x" {
		t.Errorf("no workspace service: got %q", got)
	}
}
