package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// twoDocWorkspace is a session with two open documents: vb1 (ws-1, active)
// and vb2 (ws-2, "to-trinh.docx").
func twoDocWorkspace(t *testing.T) *fakeWorkspace {
	t.Helper()
	ws := newFakeWorkspace(testCongVan(t, [4]int{20, 15, 30, 20}))
	ws.addDocument("ws-2", "to-trinh.docx", testCongVan(t, [4]int{20, 15, 30, 20}))
	return ws
}

func TestResolveDocumentSingleDocumentNeedsNoMention(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, [4]int{20, 15, 30, 20}))
	got, err := resolveDocument(toolCtx(), ws, "s-1", "", true)
	if err != nil || got.ID != "ws-1" {
		t.Fatalf("one document is the target without @: %v, %v", got, err)
	}
}

func TestResolveDocumentEditNeedsTheUserToNameTheDocument(t *testing.T) {
	ws := twoDocWorkspace(t)

	// no @ at all: an edit is refused, a read falls back to the active tab
	if _, err := resolveDocument(toolCtx(), ws, "s-1", "", true); err == nil || !strings.Contains(err.Error(), "@") {
		t.Fatalf("edit without a named document must be refused with a hint about @, got %v", err)
	}
	if got, err := resolveDocument(toolCtx(), ws, "s-1", "", false); err != nil || got.ID != "ws-1" {
		t.Fatalf("read without a target uses the active document: %v, %v", got, err)
	}

	// the model names vb2 but the user only mentioned vb1
	ctx := types.WithMentionedDocuments(toolCtx(), []string{"ws-1"})
	if _, err := resolveDocument(ctx, ws, "s-1", "vb2", true); err == nil || !strings.Contains(err.Error(), "chưa gọi đích danh") {
		t.Fatalf("edit of a document the user did not name must be refused, got %v", err)
	}
	if got, err := resolveDocument(ctx, ws, "s-1", "vb2", false); err != nil || got.ID != "ws-2" {
		t.Fatalf("reading another document needs no @: %v, %v", got, err)
	}

	// one @-mention is the default target
	ctx = types.WithMentionedDocuments(toolCtx(), []string{"ws-2"})
	if got, err := resolveDocument(ctx, ws, "s-1", "", true); err != nil || got.ID != "ws-2" {
		t.Fatalf("the single mentioned document is the edit target: %v, %v", got, err)
	}
	if got, err := resolveDocument(ctx, ws, "s-1", "to-trinh", true); err != nil || got.ID != "ws-2" {
		t.Fatalf("a partial file name names the document: %v, %v", got, err)
	}

	// a selection in vb2 designates it too
	ctx = types.WithDocumentSelection(toolCtx(), &types.DocumentSelection{Text: "đoạn", DocumentID: "ws-2"})
	if got, err := resolveDocument(ctx, ws, "s-1", "", true); err != nil || got.ID != "ws-2" {
		t.Fatalf("the selection's document is the edit target: %v, %v", got, err)
	}

	if _, err := resolveDocument(toolCtx(), ws, "s-1", "vb9", false); err == nil || !strings.Contains(err.Error(), "vb1") {
		t.Fatalf("an unknown document lists the open ones, got %v", err)
	}
}

func TestEditToolRoutesOpsToTheNamedDocument(t *testing.T) {
	ws := twoDocWorkspace(t)
	tool := NewMarkPassagesTool(ws, "s-1")
	args := json.RawMessage(`{"document":"vb2","marks":[{"paragraph":0,"reason":"kiểm tra"}]}`)

	res, err := tool.Execute(toolCtx(), args)
	if err != nil || res.Success {
		t.Fatalf("an edit without @ must fail when two documents are open: %+v %v", res, err)
	}
	if len(ws.snapshots) != 0 {
		t.Fatal("a refused edit must not snapshot")
	}

	ctx := types.WithMentionedDocuments(toolCtx(), []string{"ws-2"})
	res, err = tool.Execute(ctx, args)
	if err != nil || !res.Success {
		t.Fatalf("mark on the mentioned document: %+v %v", res, err)
	}
	if ws.documentID != "ws-2" || res.Data["document_id"] != "ws-2" {
		t.Fatalf("ops must target ws-2: snapshot=%q data=%v", ws.documentID, res.Data["document_id"])
	}
}

func TestBuildOpenDocumentPromptListsDocumentsAndInjectsTheNamedOnes(t *testing.T) {
	ws := twoDocWorkspace(t)

	got := BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-1", "")
	for _, want := range []string{"<session_documents>", "vb1 · cong-van.docx (văn bản làm việc, tab đang xem)", "vb2 · to-trinh.docx", `handle="vb1"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, got)
		}
	}
	// Without @ every open document is carried, the viewed tab first, so a
	// question about "hai văn bản này" can be answered for both.
	i1, i2 := strings.Index(got, `handle="vb1"`), strings.Index(got, `handle="vb2"`)
	if i1 < 0 || i2 < 0 || i2 < i1 {
		t.Fatalf("without @ both documents are injected, the active one first:\n%s", got)
	}

	ctx := types.WithMentionedDocuments(context.Background(), []string{"ws-1", "ws-2"})
	got = BuildOpenDocumentPrompt(ctx, ws, 7, "s-1", "")
	if !strings.Contains(got, `handle="vb1"`) || !strings.Contains(got, `handle="vb2"`) {
		t.Fatalf("both named documents are injected:\n%s", got)
	}
	if !strings.Contains(got, "vb2 · to-trinh.docx (văn bản làm việc) (người dùng gọi đích danh trong yêu cầu này)") {
		t.Fatalf("the index marks the named documents:\n%s", got)
	}
}

func TestBuildOpenDocumentPromptWithoutDocumentsIsEmpty(t *testing.T) {
	if got := BuildOpenDocumentPrompt(context.Background(), &emptyWorkspace{}, 7, "s-1", "PA05"); got != "" {
		t.Fatalf("a session without documents adds nothing to the prompt, got %q", got)
	}
}

// emptyWorkspace is a session that holds no document.
type emptyWorkspace struct{ fakeWorkspace }

func (*emptyWorkspace) List(context.Context, uint64, string) ([]*types.DocumentWorkspace, error) {
	return nil, nil
}
