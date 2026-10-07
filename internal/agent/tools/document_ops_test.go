package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/types"
)

// resultOps returns the edit plan of a tool result.
func resultOps(t *testing.T, res *types.ToolResult) []DocumentOp {
	t.Helper()
	ops, ok := res.Data["document_ops"].([]DocumentOp)
	if !ok {
		t.Fatalf("document_ops = %T", res.Data["document_ops"])
	}
	if id, _ := res.Data["ops_batch_id"].(string); len(id) != 36 {
		t.Fatalf("ops_batch_id = %v", res.Data["ops_batch_id"])
	}
	if _, ok := res.Data["snapshot_seq"].(int); !ok {
		t.Fatalf("snapshot_seq = %v", res.Data["snapshot_seq"])
	}
	if _, ok := res.Data["document_revision"]; ok {
		t.Fatal("an edit plan carries no document_revision")
	}
	return ops
}

// applyPlan plays the editor plugin: it applies the result's ops to the
// fake workspace's document (with docxedit, as a reference implementation
// of the op contract) and stores the outcome, as the editor's save would.
// (DocumentWorkspaceSource has no write method: the tool cannot write.)
func applyPlan(t *testing.T, ws *fakeWorkspace, res *types.ToolResult) []DocumentOp {
	t.Helper()
	ops := resultOps(t, res)
	ws.content = applyOps(t, ws.content, ops)
	return ops
}

// findAnchor resolves an anchor like the plugin: the occurrence-th
// paragraph whose whitespace-normalised text equals anchor.Text.
func findAnchor(doc *docxedit.Document, a *OpAnchor) (int, error) {
	if a == nil {
		return 0, fmt.Errorf("no anchor")
	}
	if a.AtStart {
		return -1, nil
	}
	n := 0
	for _, p := range doc.Paragraphs() {
		if anchorText(p.Text) == a.Text {
			n++
			if n == max(a.Occurrence, 1) {
				return p.Index, nil
			}
		}
	}
	return 0, fmt.Errorf("anchor %+v not found", *a)
}

var testAuthor = docxedit.Author{Name: "plugin"}

func applyOps(t *testing.T, content []byte, ops []DocumentOp) []byte {
	t.Helper()
	doc, err := docxedit.Open(content)
	if err != nil {
		t.Fatal(err)
	}
	sections := len(docformat.InspectDocx(content).Sections)
	for k, op := range ops {
		fail := func(err error) {
			t.Helper()
			b, _ := json.Marshal(op)
			t.Fatalf("op %d %s: %v", k, b, err)
		}
		at := 0
		if op.Op != OpPageSetup {
			if at, err = findAnchor(doc, op.Anchor); err != nil {
				fail(err)
			}
		}
		switch op.Op {
		case OpReplaceText:
			err = doc.ReplaceSubstring(at, *op.Old, *op.New, testAuthor)
		case OpReplaceParagraph:
			err = doc.ReplaceText(at, *op.New, testAuthor)
		case OpInsertAfter:
			np := docxedit.NewParagraph{Text: op.Text}
			if op.Like != nil {
				like, err := findAnchor(doc, op.Like)
				if err != nil {
					fail(err)
				}
				np.InheritFrom = &like
			}
			if op.Alignment != "" {
				a := op.Alignment
				np.Para = &docxedit.ParaProps{Alignment: &a}
			}
			if op.Bold != nil || op.Italic != nil {
				np.Run = &docxedit.RunProps{Bold: op.Bold, Italic: op.Italic}
			}
			_, err = doc.InsertParagraphAfter(at, np, testAuthor)
		case OpMark:
			s := func(v string) *string { return &v }
			m := map[string]docxedit.Mark{
				"underline": {Underline: s("single"), UnderlineColor: s("FF0000")},
				"highlight": {Highlight: s("yellow")},
				"color":     {Color: s("FF0000")},
			}[op.Style]
			if op.Text != "" {
				err = doc.MarkSubstring(at, op.Text, m, testAuthor)
			} else {
				err = doc.MarkParagraph(at, m, testAuthor)
			}
		case OpFormatParagraph:
			rp := docxedit.RunProps{SizePt: op.SizePt, Bold: op.Bold, Italic: op.Italic}
			if op.Font != "" {
				f := op.Font
				rp.Font = &f
			}
			if rp.Font != nil || rp.SizePt != nil || rp.Bold != nil || rp.Italic != nil {
				err = doc.SetRunProps(at, rp, testAuthor)
			}
			if err == nil && op.Alignment != "" {
				a := op.Alignment
				err = doc.SetParaProps(at, docxedit.ParaProps{Alignment: &a}, testAuthor)
			}
		case OpPageSetup:
			pt := func(mm float64) *float64 { v := math.Round(mm*twipsPerMM) / 20; return &v }
			sp := docxedit.SectionProps{}
			for key, dst := range map[string]**float64{"top": &sp.MarginTopPt, "bottom": &sp.MarginBottomPt,
				"left": &sp.MarginLeftPt, "right": &sp.MarginRightPt} {
				if v, ok := op.MarginsMm[key]; ok {
					*dst = pt(v)
				}
			}
			if op.A4 {
				w, h := 11906.0/20, 16838.0/20
				sp.PageWidthPt, sp.PageHeightPt = &w, &h
			}
			for i := 0; i < sections && err == nil; i++ {
				err = doc.SetSectionProps(i, sp, testAuthor)
			}
		default:
			err = fmt.Errorf("unknown op")
		}
		if err != nil {
			fail(err)
		}
	}
	out, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOpAnchorJSON(t *testing.T) {
	b, _ := json.Marshal(DocumentOp{Op: OpInsertAfter, Anchor: &OpAnchor{AtStart: true}, Text: "x"})
	if string(b) != `{"op":"insertAfter","anchor":{"atStart":true},"text":"x"}` {
		t.Fatalf("%s", b)
	}
	b, _ = json.Marshal(DocumentOp{Op: OpMark, Anchor: &OpAnchor{Text: "a  b", Occurrence: 2}, Style: "underline"})
	if string(b) != `{"op":"mark","anchor":{"text":"a  b","occurrence":2},"style":"underline"}` {
		t.Fatalf("%s", b)
	}
	empty := ""
	b, _ = json.Marshal(DocumentOp{Op: OpReplaceText, Anchor: &OpAnchor{Text: "t", Occurrence: 1}, Old: &empty, New: &empty})
	if !strings.Contains(string(b), `"new":""`) {
		t.Fatalf("an empty replacement must be kept: %s", b)
	}
}

func TestSelectionOverlaps(t *testing.T) {
	for _, c := range []struct {
		sel, target string
		want        bool
	}{
		{"cải cách hành chính", "Sở Nội vụ đề nghị các đơn vị triển khai công tác cải cách hành chính năm 2026.", true},
		{"Sở Nội vụ đề nghị các đơn vị triển khai công tác cải cách hành chính năm 2026. Đề nghị các đơn vị", "năm 2026", true},
		{"triển khai công tác cải cách hành chính năm 2027", "công tác cải cách hành chính năm 2026", true},
		{"cải cách hành chính", "Nguyễn Văn A", false},
		{"", "x", false},
	} {
		if got := selectionOverlaps(c.sel, c.target); got != c.want {
			t.Errorf("selectionOverlaps(%q, %q) = %v", c.sel, c.target, got)
		}
	}
}

func selectionCtx(text string) context.Context {
	return types.WithDocumentSelection(toolCtx(), &types.DocumentSelection{Text: text})
}

func TestNormalizeDocTextForAnchorsAndSelection(t *testing.T) {
	// the paragraph has a zero-width space, a no-break space and an NFD "ế"
	// (e + U+0302 circumflex + U+0301 acute); anchor and selection are NFC
	nfd := "Ki\u200bn ngh\u0065\u0302\u0301\u00a0số  01"
	if got := normalizeDocText(nfd); got != "Kin nghế số 01" {
		t.Fatalf("normalizeDocText = %q", got)
	}
	vdoc := newVirtualDoc([]docxedit.Paragraph{{Index: 0, Text: "Kin nghế số 01"}, {Index: 1, Text: nfd}})
	if a := vdoc.anchor(1); a.Text != "Kin nghế số 01" || a.Occurrence != 2 {
		t.Fatalf("anchor = %+v", a)
	}
	if !selectionOverlaps("nghế số", nfd) || !selectionOverlaps("Kin nghế số 01", "Ki\u200bn  ngh\u00ea\u0301 số 01") {
		t.Fatal("selection must match across zero-width, NBSP and NFD differences")
	}
}

func TestShortSelectionNeedsContainment(t *testing.T) {
	// two words shared with the target but not contained in it
	if selectionOverlaps("hành chính", "công tác hành  nhân sự chính") {
		t.Fatal("a two-word selection must be contained in the target")
	}
	if !selectionOverlaps("hành chính", "cải cách hành chính năm 2026") {
		t.Fatal("a contained two-word selection overlaps")
	}
	// three words or more: a shared run is enough
	if !selectionOverlaps("đề nghị các đơn vị khẩn trương", "Sở đề nghị các đơn vị thực hiện") {
		t.Fatal("a shared four-word run overlaps")
	}
}
