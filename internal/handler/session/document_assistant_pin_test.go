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
	// without ONLYOFFICE a session's documents (sources, Word add-in
	// targets) still pin the document assistant
	ws.enabled = false
	if got := h.pinDocumentAssistant(ctx, &types.Session{ID: "with-doc", TenantID: 1}, "x"); got != types.BuiltinDocumentAssistantID {
		t.Errorf("editor disabled: got %q", got)
	}
	if got := (&Handler{}).pinDocumentAssistant(ctx, &types.Session{ID: "with-doc"}, "x"); got != "x" {
		t.Errorf("no workspace service: got %q", got)
	}
}

func TestResolveDocumentReferencesKeepsOnlyTheSessionsDocuments(t *testing.T) {
	ws := &routeWorkspaces{enabled: true, bySess: map[string]*types.DocumentWorkspace{
		"s1": {ID: "ws-a", SessionID: "s1", FileName: "a.docx", Position: 1},
	}}
	h := &Handler{documentWorkspaces: ws}
	session := &types.Session{ID: "s1", TenantID: 7}
	items := []MentionedItemRequest{
		{ID: "ws-a", Type: types.MentionTypeDocument},
		{ID: "ws-of-another-session", Type: types.MentionTypeDocument},
		{ID: "ws-a", Type: types.MentionTypeDocument},
		{ID: "kb-1", Type: "kb"},
	}
	sel := &types.DocumentSelection{Text: "đoạn", DocumentID: "ws-a", Document: "forged by the client"}

	ids, got := h.resolveDocumentReferences(context.Background(), session, items, sel)
	if len(ids) != 1 || ids[0] != "ws-a" {
		t.Fatalf("mentioned documents = %v, want [ws-a]", ids)
	}
	if got.Document != "vb1 · a.docx" {
		t.Fatalf("selection document label = %q", got.Document)
	}

	foreign := &types.DocumentSelection{Text: "đoạn", DocumentID: "ws-x"}
	_, got = h.resolveDocumentReferences(context.Background(), session, nil, foreign)
	if got.DocumentID != "" || got.Document != "" {
		t.Fatalf("a selection in an unknown document loses its reference: %+v", got)
	}
}
