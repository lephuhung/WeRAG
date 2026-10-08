package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// sourceWorkspace is a session with one target (vb1, ws-1) plus source
// documents whose stored text SourceText serves.
type sourceWorkspace struct {
	*fakeWorkspace
	texts map[string]*types.DocumentWorkspaceText
}

func (f *sourceWorkspace) SourceText(_ context.Context, _ uint64, _ string, documentID string) (*types.DocumentWorkspaceText, *types.DocumentWorkspace, error) {
	ws, _, err := f.find(documentID)
	if err != nil {
		return nil, nil, err
	}
	text := f.texts[documentID]
	if text == nil {
		return nil, ws, errors.New("no text")
	}
	return text, ws, nil
}

// addSource adds a source document (next position) with its stored text.
func (f *sourceWorkspace) addSource(id, fileName, fileType string, text *types.DocumentWorkspaceText) *types.DocumentWorkspace {
	ws := f.addDocument(id, fileName, nil)
	ws.Role = types.DocumentWorkspaceRoleSource
	ws.FileType = fileType
	ws.FileSize = 2 << 20
	ws.TextStatus = types.DocumentSourceTextReady
	f.texts[id] = text
	return ws
}

func newSourceWorkspace(t *testing.T) *sourceWorkspace {
	t.Helper()
	f := &sourceWorkspace{fakeWorkspace: newFakeWorkspace(testCongVan(t, [4]int{20, 15, 30, 20})), texts: map[string]*types.DocumentWorkspaceText{}}
	chunks, _ := json.Marshal([]types.TemporaryDocumentChunk{
		{Seq: 0, ContextHeader: "Chương I", Content: "Điều 1. Năm 2025 thu ngân sách đạt 1.250 tỷ đồng."},
		{Seq: 1, ContextHeader: "Chương II", Content: "Điều 5. Chi đầu tư phát triển 430 tỷ đồng."},
	})
	f.addSource("ws-src", "bao-cao.pdf", "pdf", &types.DocumentWorkspaceText{Content: "x", Chunks: chunks, ChunkCount: 2})
	f.addSource("ws-dem", "ghi-chu.docx", "docx", &types.DocumentWorkspaceText{Content: "Dòng một\n\nDòng hai\n"})
	return f
}

func TestEditToolsRefuseASource(t *testing.T) {
	ws := newSourceWorkspace(t)
	ctx := types.WithMentionedDocuments(toolCtx(), []string{"ws-src"})
	if _, err := resolveDocument(ctx, ws, "s-1", "vb2", true); err == nil ||
		err.Error() != "vb2 là tài liệu nguồn, chỉ tra cứu được; mở nó để soạn thảo nếu cần sửa." {
		t.Fatalf("an edit of a source must be refused with the source message, got %v", err)
	}
	// without a document argument the only target is the edit target,
	// sources do not count as competing documents
	if got, err := resolveDocument(toolCtx(), ws, "s-1", "", true); err != nil || got.ID != "ws-1" {
		t.Fatalf("the single target is edited without @ next to sources: %v, %v", got, err)
	}
	// reading a source is fine
	if got, err := resolveDocument(toolCtx(), ws, "s-1", "bao-cao", false); err != nil || got.ID != "ws-src" {
		t.Fatalf("a source can be read: %v, %v", got, err)
	}
	// tools that need the Word file refuse it even to read
	if _, err := resolveTargetDocument(toolCtx(), ws, "s-1", "vb3", false); err == nil || !strings.Contains(err.Error(), "tài liệu nguồn") {
		t.Fatalf("format and spelling tools refuse a source, got %v", err)
	}

	for name, tool := range map[string]types.Tool{
		"mark":    NewMarkPassagesTool(ws, "s-1"),
		"rewrite": NewRewriteParagraphsTool(ws, "s-1"),
		"insert":  NewInsertParagraphsTool(ws, "s-1"),
	} {
		// rewrite needs a selection first; it is in the source
		ctx := types.WithDocumentSelection(ctx, &types.DocumentSelection{Text: "Điều 1", DocumentID: "ws-src"})
		res, err := tool.Execute(ctx, json.RawMessage(`{"document":"vb2","marks":[{"paragraph":0,"reason":"x"}],"edits":[{"paragraph":0,"new":"x"}],"inserts":[{"after":0,"text":"x"}]}`))
		if err != nil || res.Success || !strings.Contains(res.Error, "tài liệu nguồn") {
			t.Fatalf("%s on a source must be refused: %+v %v", name, res, err)
		}
	}
	if len(ws.snapshots) != 0 {
		t.Fatal("a refused edit must not snapshot")
	}

	// a session holding only a source has nothing to edit
	only := &sourceWorkspace{fakeWorkspace: newFakeWorkspace(nil), texts: map[string]*types.DocumentWorkspaceText{}}
	only.ws.Role = types.DocumentWorkspaceRoleSource
	if _, err := resolveDocument(toolCtx(), only, "s-1", "", true); err == nil || !strings.Contains(err.Error(), "tài liệu nguồn") {
		t.Fatalf("the only document is a source: %v", err)
	}
}

func TestReadDocumentOutlineReadsASourceByChunks(t *testing.T) {
	ws := newSourceWorkspace(t)
	tool := NewReadDocumentOutlineTool(ws, nil, "s-1")

	res, err := tool.Execute(toolCtx(), json.RawMessage(`{"document":"vb2"}`))
	if err != nil || !res.Success {
		t.Fatalf("outline of a source: %+v %v", res, err)
	}
	for _, want := range []string{
		"# Nội dung tài liệu nguồn vb2 · bao-cao.pdf (pdf, 2 chunk)",
		"[0] (chunk) Chương I | Điều 1. Năm 2025 thu ngân sách đạt 1.250 tỷ đồng.",
		"[1] (chunk) Chương II | Điều 5. Chi đầu tư phát triển 430 tỷ đồng.",
		"dẫn nguồn",
	} {
		if !strings.Contains(res.Output, want) {
			t.Fatalf("outline lacks %q:\n%s", want, res.Output)
		}
	}

	res, err = tool.Execute(toolCtx(), json.RawMessage(`{"document":"vb2","from":0,"limit":1}`))
	if err != nil || !res.Success || strings.Contains(res.Output, "Chương II") ||
		!strings.Contains(res.Output, "gọi lại với document=vb2 from=1") {
		t.Fatalf("paging a source by chunks: %+v %v", res, err)
	}

	// a demoted target has no chunks: one line per paragraph
	res, err = tool.Execute(toolCtx(), json.RawMessage(`{"document":"vb3"}`))
	if err != nil || !res.Success || !strings.Contains(res.Output, "(docx, 2 dòng)") ||
		!strings.Contains(res.Output, "[1] Dòng hai") {
		t.Fatalf("outline of an unchunked source: %+v %v", res, err)
	}
}

func TestBuildOpenDocumentPromptIndexesSourcesWithoutTheirText(t *testing.T) {
	ws := newSourceWorkspace(t)
	got := BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-1", "")
	for _, want := range []string{
		"<session_documents>",
		"A source (tài liệu nguồn) is a file the user uploaded at chat",
		"- vb1 · cong-van.docx (văn bản làm việc, tab đang xem)",
		"- vb2 · bao-cao.pdf (tài liệu nguồn, chỉ tra cứu, pdf, 2.0 MB; tra cứu bằng read_document_outline document=vb2)",
		// a tab next to sources is not "the only document": nothing is
		// injected whole, documents without a card show their first lines
		`<relevant_passages handle="vb1"`,
		`<relevant_passages handle="vb2" name="bao-cao.pdf" role="tài liệu nguồn" unit="chunk">`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<open_document handle") {
		t.Fatalf("a source is never injected in full:\n%s", got)
	}
	// a question about the source brings its matching chunk
	got = BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-1", "chi đầu tư phát triển bao nhiêu")
	if !strings.Contains(got, "[1] (Chương II) Điều 5. Chi đầu tư phát triển 430 tỷ đồng.") {
		t.Fatalf("the matching chunk of the source:\n%s", got)
	}

	// @-naming a source does not inject it either; attached now, the index
	// points at the message's attachment instead of the tool
	ctx := types.WithMentionedDocuments(context.Background(), []string{"ws-src"})
	ctx = WithAttachedSources(ctx, []string{"ws-src"})
	got = BuildOpenDocumentPrompt(ctx, ws, 7, "s-1", "")
	if strings.Contains(got, "<open_document handle") || strings.Contains(got, `<relevant_passages handle="vb1"`) ||
		!strings.Contains(got, `<relevant_passages handle="vb2"`) {
		t.Fatalf("a named source is not injected whole; the passages come from it alone:\n%s", got)
	}
	if !strings.Contains(got, "- vb2 · bao-cao.pdf (tài liệu nguồn, chỉ tra cứu, pdf, 2.0 MB; nội dung đính kèm trong tin nhắn này) (người dùng gọi đích danh trong yêu cầu này)") {
		t.Fatalf("the attached source's index line:\n%s", got)
	}

	// a session holding only a source still gets its index
	only := &sourceWorkspace{fakeWorkspace: newFakeWorkspace(nil), texts: map[string]*types.DocumentWorkspaceText{}}
	only.ws.Role, only.ws.FileType, only.ws.TextStatus = types.DocumentWorkspaceRoleSource, "xlsx", types.DocumentSourceTextProcessing
	got = BuildOpenDocumentPrompt(context.Background(), only, 7, "s-1", "")
	if !strings.Contains(got, "- vb1 · cong-van.docx (tài liệu nguồn, chỉ tra cứu, xlsx; đang đọc nội dung, chưa tra cứu được)") ||
		strings.Contains(got, "<open_document handle") {
		t.Fatalf("index of a source-only session:\n%s", got)
	}
}
