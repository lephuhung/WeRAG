package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	selRepeated = "Trong quá trình triển khai, nếu có khó khăn, đề nghị các đơn vị phản ánh kịp thời."
	selFirst    = "Trong năm 2025, toàn ngành đã hoàn thành 18/20 chỉ tiêu được giao."
	selSecond   = "Kinh phí thực hiện được bố trí từ nguồn ngân sách nhà nước."
)

// selectionDocx: [0] repeated, [1] first, [2] repeated, [3] second.
func selectionDocx(t *testing.T) []byte {
	t.Helper()
	var body strings.Builder
	for _, p := range []string{selRepeated, selFirst, selRepeated, selSecond} {
		body.WriteString(testPara(p, "both", "Times New Roman", 14, false, false))
	}
	return buildTestDocx(t, body.String(), nd30Margins)
}

func selCtx(sel types.DocumentSelection) context.Context {
	return types.WithDocumentSelection(toolCtx(), &sel)
}

func TestRewriteProposalPicksTheSelectedOfRepeatedParagraphs(t *testing.T) {
	ws := newFakeWorkspace(selectionDocx(t))
	args := `{"edits":[{"match":"` + selRepeated + `","new":"Trong quá trình triển khai, nếu có vướng mắc, đề nghị các đơn vị kịp thời phản ánh."}]}`

	// the editor says it is the second instance
	res := runToolCtx(t, selCtx(types.DocumentSelection{Text: selRepeated, ParagraphHint: selRepeated, ParagraphOccurrence: 2}),
		NewRewriteParagraphsTool(ws, "s"), args)
	vs := proposalVariants(t, res)
	if !res.Success || len(vs) != 1 || vs[0].Ops[0].Anchor.Occurrence != 2 {
		t.Fatalf("result: %+v", res)
	}

	// the model names the first instance by index: the selection wins
	res = runToolCtx(t, selCtx(types.DocumentSelection{Text: selRepeated, ParagraphHint: selRepeated, ParagraphOccurrence: 2}),
		NewRewriteParagraphsTool(ws, "s"), `{"edits":[{"paragraph":0,"new":"Đề nghị các đơn vị kịp thời phản ánh."}]}`)
	if vs = proposalVariants(t, res); !res.Success || vs[0].Ops[0].Anchor.Occurrence != 2 {
		t.Fatalf("result: %+v", res)
	}
	// …but a paragraph with other text is left as the model gave it
	res = runToolCtx(t, selCtx(types.DocumentSelection{Text: selRepeated, ParagraphHint: selRepeated, ParagraphOccurrence: 2}),
		NewRewriteParagraphsTool(ws, "s"), `{"edits":[{"paragraph":1,"new":"Khác."}]}`)
	if res.Success {
		t.Fatalf("an edit outside the selection is refused: %+v", res)
	}

	// no position from the editor: ambiguous, and no button is promised
	res = runToolCtx(t, selectionCtx(selRepeated), NewRewriteParagraphsTool(ws, "s"), args)
	if res.Success || !strings.Contains(res.Error, "khớp nhiều đoạn") || !strings.Contains(res.Error, "KHÔNG có nút") {
		t.Fatalf("result: %+v", res)
	}
}

func TestRewriteProposalSplitsASelectionAcrossParagraphs(t *testing.T) {
	ws := newFakeWorkspace(selectionDocx(t))
	sel := types.DocumentSelection{Text: selFirst + "\n" + selRepeated + "\n" + selSecond}
	match := `"match":"` + selFirst + `\n` + selRepeated + `\n` + selSecond + `"`

	// one new line per paragraph: one op each
	res := runToolCtx(t, selCtx(types.DocumentSelection{Text: selFirst + "\n" + selSecond}), NewRewriteParagraphsTool(ws, "s"),
		`{"edits":[{"match":"`+selFirst+`\n`+selSecond+`","new":"Năm 2025, toàn ngành hoàn thành 18/20 chỉ tiêu.\nKinh phí do ngân sách nhà nước bố trí."}]}`)
	vs := proposalVariants(t, res)
	if !res.Success || len(vs) != 1 || len(vs[0].Ops) != 2 ||
		vs[0].Ops[0].Anchor.Text != selFirst || vs[0].Ops[1].Anchor.Text != selSecond {
		t.Fatalf("result: %+v", res)
	}
	applyOps(t, ws.content, vs[0].Ops)

	// one merged text for three paragraphs: fails naming them, no button
	res = runToolCtx(t, selCtx(sel), NewRewriteParagraphsTool(ws, "s"), `{"edits":[{`+match+`,"new":"Một đoạn gộp."}]}`)
	if res.Success || !strings.Contains(res.Error, "MỘT đoạn") || !strings.Contains(res.Error, "KHÔNG có nút") {
		t.Fatalf("result: %+v", res)
	}

	// the model joined the paragraphs with spaces: the selection's lines
	// still say where they are
	res = runToolCtx(t, selCtx(types.DocumentSelection{Text: selFirst + "\n" + selSecond}), NewRewriteParagraphsTool(ws, "s"),
		`{"edits":[{"match":"`+selFirst+` `+selSecond+`","new":"Gộp."}]}`)
	if res.Success || !strings.Contains(res.Error, "[1], [3]") {
		t.Fatalf("result: %+v", res)
	}
}

// staleWorkspace serves an old file until a snapshot saves the editor.
type staleWorkspace struct {
	*fakeWorkspace
	fresh []byte
}

func (s *staleWorkspace) Snapshot(ctx context.Context, tenantID uint64, sessionID, documentID, label, source string, wait time.Duration) (*types.DocumentRevision, error) {
	s.content = s.fresh
	s.ws.Revision++
	return s.fakeWorkspace.Snapshot(ctx, tenantID, sessionID, documentID, label, source, wait)
}

func TestRewriteProposalReadsTheEditorWhenTheStoredFileLags(t *testing.T) {
	edited := "Trong năm 2025, toàn ngành đã hoàn thành vượt mức 18/20 chỉ tiêu được giao."
	var body strings.Builder
	for _, p := range []string{selRepeated, edited, selSecond} {
		body.WriteString(testPara(p, "both", "Times New Roman", 14, false, false))
	}
	ws := &staleWorkspace{fakeWorkspace: fakeWorkspaceWithID(selectionDocx(t), "ws-stale"), fresh: buildTestDocx(t, body.String(), nd30Margins)}
	res := runToolCtx(t, selectionCtx(edited), NewRewriteParagraphsTool(ws, "s"),
		`{"edits":[{"match":"`+edited+`","new":"Năm 2025, toàn ngành hoàn thành vượt mức 18/20 chỉ tiêu."}]}`)
	vs := proposalVariants(t, res)
	if !res.Success || len(vs) != 1 || len(ws.snapshots) != 1 || vs[0].Ops[0].Anchor.Text != edited {
		t.Fatalf("snapshots %v result: %+v", ws.snapshots, res)
	}
}

func TestDocumentSelectionKeepsTheOccurrenceOnlyWithAHint(t *testing.T) {
	n := (&types.DocumentSelection{Text: "a", ParagraphHint: "a b", ParagraphOccurrence: 3}).Normalized()
	if n.ParagraphOccurrence != 3 {
		t.Fatalf("%+v", n)
	}
	if n := (&types.DocumentSelection{Text: "a", ParagraphOccurrence: 3}).Normalized(); n.ParagraphOccurrence != 0 {
		t.Fatalf("%+v", n)
	}
}
