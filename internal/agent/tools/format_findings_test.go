package tools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/types"
)

// findingsOf checks content (positional labelling, no model) and builds
// its findings.
func findingsOf(t *testing.T, content []byte) []formatFinding {
	t.Helper()
	report := docformat.Check(toolCtx(), content, docformat.Options{SourceName: "cv.docx"})
	if !report.OK {
		t.Fatalf("check failed: %s", report.Error)
	}
	rs, err := docformat.RuleSetForReport(report)
	if err != nil {
		t.Fatal(err)
	}
	return formatFindingsOf(report, rs, content)
}

func TestFormatFindingsFixOneParagraphOneProperty(t *testing.T) {
	content := docxFixture(t) // a recognised công văn with its body in Arial
	findings := findingsOf(t, content)
	if len(findings) == 0 {
		t.Fatal("the Arial body must give findings")
	}
	doc, err := docxedit.Open(content)
	if err != nil {
		t.Fatal(err)
	}
	vdoc := newVirtualDoc(doc.Paragraphs())
	ids := map[string]bool{}
	fontFixes := 0
	for _, f := range findings {
		if f.ID == "" || ids[f.ID] || f.Title == "" || f.CheckID == "" {
			t.Fatalf("id/title: %+v", f)
		}
		ids[f.ID] = true
		if len(f.Ops) == 0 {
			continue
		}
		// the add-in's contract: one formatParagraph on the finding's own
		// anchor, setting at least one property
		if len(f.Ops) != 1 || f.Anchor == nil {
			t.Fatalf("ops: %+v", f)
		}
		op := f.Ops[0]
		if op.Op != OpFormatParagraph || op.Anchor == nil || *op.Anchor != *f.Anchor || f.Anchor.Occurrence < 1 {
			t.Fatalf("op: %+v anchor %+v", op, f.Anchor)
		}
		if op.Font == "" && op.SizePt == nil && op.Bold == nil && op.Italic == nil && op.Alignment == "" {
			t.Fatalf("op sets nothing: %+v", op)
		}
		// the anchor finds the paragraph in the checked file
		hit := false
		for i := range vdoc.texts {
			if a := spotAnchor(vdoc, i); a != nil && *a == *f.Anchor {
				hit = true
			}
		}
		if !hit {
			t.Fatalf("anchor not in the document: %+v", f.Anchor)
		}
		if f.CheckID == "noi_dung.font" {
			fontFixes++
			// only the rule's property: the size rule is another finding
			if op.Font != "Times New Roman" || op.SizePt != nil || op.Alignment != "" {
				t.Fatalf("font fix sets more than the font: %+v", op)
			}
			if f.Current != "Arial" || f.Expected == "" || !strings.Contains(f.Reason, "Times New Roman") {
				t.Fatalf("font finding text: %+v", f)
			}
		}
	}
	if fontFixes == 0 {
		t.Fatalf("one font fix per Arial body paragraph: none in %+v", findings)
	}
}

func TestFormatFindingsOfAnUnrecognisedStructureOfferNoFix(t *testing.T) {
	// without the model the short công văn misses required components:
	// its lines may be labelled wrongly, so nothing is offered as a fix
	findings := findingsOf(t, testCongVan(t, nd30Margins))
	withheld := 0
	for _, f := range findings {
		if len(f.Ops) != 0 {
			t.Fatalf("fix offered on an unrecognised structure: %+v", f)
		}
		if f.Reason == unsureStructureReason {
			withheld++
		}
	}
	if withheld == 0 {
		t.Fatalf("no withheld fix in %+v", findings)
	}
}

func TestFormatFindingsWithoutFixCarryNoOps(t *testing.T) {
	// margins off NĐ30: a page-setup finding, shown but not applied by the
	// add-in (it applies a formatParagraph only)
	findings := findingsOf(t, testCongVan(t, [4]int{1134, 1700, 1134, 1000}))
	page := 0
	for _, f := range findings {
		if strings.HasPrefix(f.CheckID, "page.margin") {
			page++
			if len(f.Ops) != 0 || f.Anchor != nil || f.Expected == "" || f.Current == "" {
				t.Fatalf("page finding: %+v", f)
			}
		}
	}
	if page == 0 {
		t.Fatalf("no page finding in %+v", findings)
	}
}

func TestFormatReportCarriesFindingsWithItsRevision(t *testing.T) {
	resetFormatChecks(t)
	model := &countingChat{fakeChat: fakeChat{reply: "{}", evaluation: "## Kết luận\nChưa đạt."}}
	ws := fakeWorkspaceWithID(docxFixture(t), "ws-rev")
	ws.ws.Revision = 4
	ctx := toolCtx()
	NewCheckDocumentFormatToolForWorkspace(ws, model, "sess-r").ForDocument("ws-rev").StartBackground(ctx, nil)
	if st := waitFormatCheck(t, "ws-rev"); st == nil || st.Status != types.DocumentFormatCheckReady {
		t.Fatalf("state = %+v", st)
	}
	report := SessionFormatCheckReport(ctx, "ws-rev")
	if report == nil || report.ReportID == "" || report.DocumentRevision == nil || *report.DocumentRevision != 4 || len(report.Findings) == 0 {
		t.Fatalf("report: %+v", report)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Findings []struct {
			ID     string `json:"id"`
			Anchor *struct {
				Text       string `json:"text"`
				Occurrence int    `json:"occurrence"`
			} `json:"anchor"`
			Ops []map[string]any `json:"ops"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	withOps := 0
	for _, f := range wire.Findings {
		if len(f.Ops) == 0 {
			continue
		}
		withOps++
		a, _ := f.Ops[0]["anchor"].(map[string]any)
		if f.Ops[0]["op"] != "formatParagraph" || a == nil || a["text"] != f.Anchor.Text || int(a["occurrence"].(float64)) != f.Anchor.Occurrence {
			t.Fatalf("wire op: %+v anchor %+v", f.Ops[0], f.Anchor)
		}
	}
	if withOps == 0 {
		t.Fatalf("no fixable finding on the wire: %s", raw)
	}

	// a cached result from before findings were kept: the plain report
	r := formatChecks.get(ctx, formatCheckDocumentKey("ws-rev"))
	old := *r
	old.Findings, old.ReportID = nil, ""
	formatChecks.put(ctx, &old, formatCheckDocumentKey("ws-rev"))
	if rep := SessionFormatCheckReport(ctx, "ws-rev"); rep.Findings != nil || rep.DocumentRevision != nil || rep.ReportID != "" {
		t.Fatalf("legacy report: %+v", rep)
	}
}

func TestFormatReportRevisionAfterAKeptSave(t *testing.T) {
	resetFormatChecks(t)
	quiet, poll := formatRecheckQuiet, formatRecheckPoll
	formatRecheckQuiet, formatRecheckPoll = 50*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { formatRecheckQuiet, formatRecheckPoll = quiet, poll })

	m := [4]int{20, 15, 30, 20}
	content := longDoc(t, "UBND TỈNH", "Nội dung cũ.", "Times New Roman", m)
	ws := &recheckWorkspace{fakeWorkspace: newFakeWorkspace(content)}
	ws.ws.Revision = 2
	model := &countingChat{fakeChat: fakeChat{reply: "{}", evaluation: "## Kết luận\nĐạt."}}
	tool := NewCheckDocumentFormatToolForWorkspace(ws, model, "sess-kr").ForDocument("ws-1")
	tool.Prewarm(toolCtx())
	waitState(t, "ws-1", func(st *types.DocumentFormatCheck) bool { return st.Status == types.DocumentFormatCheckReady })
	revisionOf := func() int {
		r := SessionFormatCheckReport(toolCtx(), "ws-1")
		if r == nil || r.DocumentRevision == nil {
			t.Fatalf("report: %+v", r)
		}
		return *r.DocumentRevision
	}
	if rev := revisionOf(); rev != 2 {
		t.Fatalf("revision = %d, want 2", rev)
	}
	save := func(c []byte, at time.Time) {
		ws.set(c, at)
		ws.mu.Lock()
		ws.ws.Revision++
		ws.mu.Unlock()
		tool.Recheck(toolCtx())
		waitState(t, "ws-1", func(st *types.DocumentFormatCheck) bool {
			return st.CheckedSavedAt != nil && !st.CheckedSavedAt.Before(at)
		})
	}
	// the same bytes saved again: the anchors hold for the new revision
	save(content, time.Now().Add(time.Second))
	if rev := revisionOf(); rev != 3 {
		t.Fatalf("same bytes: revision = %d, want 3", rev)
	}
	// wording changed, format kept: the evaluation stands, but the
	// findings still name the checked revision (the add-in applies nothing)
	save(longDoc(t, "UBND TỈNH", "Nội dung đã viết lại.", "Times New Roman", m), time.Now().Add(2*time.Second))
	if rev := revisionOf(); rev != 3 || model.evalCount() != 1 {
		t.Fatalf("wording edit: revision = %d (want 3), evaluations %d", rev, model.evalCount())
	}
}

func TestJudgedFormatFindings(t *testing.T) {
	vdoc := &virtualDoc{texts: []string{"Nơi nhận", "- Như trên", "- Lưu: VT.", "Số 5/UBND"}}
	items := []docformat.JudgmentItem{
		{Item: 1, Status: docformat.JudgeFail, By: "rules", Findings: []docformat.JudgmentFinding{{Severity: "error", Rule: "measured elsewhere"}}},
		{Item: 4, Status: docformat.JudgeFail, By: "rules", Findings: []docformat.JudgmentFinding{
			{Severity: "error", Component: "noi_nhan", Rule: "Dòng đầu nơi nhận ghi \"Nơi nhận:\" (có dấu hai chấm)", Line: "Nơi nhận", Fix: "Thêm dấu hai chấm."},
			{Severity: "warn", Component: "noi_nhan", Rule: "Mỗi dòng nơi nhận kết thúc bằng \";\"", Line: "Như trên", Fix: "Thêm \";\"."},
			{Severity: "error", Component: "noi_nhan", Rule: "Dòng cuối là Lưu", Line: "không có trong văn bản"},
		}},
		{Item: 5, Status: docformat.JudgeFail, By: "llm", Title: "Số ký hiệu, địa danh - ngày tháng", Line: "5. Số ký hiệu, địa danh - ngày tháng — Lỗi: thiếu dấu hai chấm sau Số"},
		{Item: 6, Status: docformat.JudgePass, By: "llm", Title: "Yêu cầu riêng theo loại", Line: "6. Yêu cầu riêng theo loại — Đạt"},
	}
	got := judgedFormatFindings(items, vdoc)
	if len(got) != 4 {
		t.Fatalf("findings: %+v", got)
	}
	whole, part, lost, llm := got[0], got[1], got[2], got[3]
	if whole.ID != "judgment.4#1" || whole.Anchor == nil || whole.Anchor.Text != "Nơi nhận" || whole.Text != "" ||
		!strings.Contains(whole.Title, "Nơi nhận") || whole.Reason != "Cách sửa: Thêm dấu hai chấm." || len(whole.Ops) != 0 {
		t.Fatalf("whole-line finding: %+v", whole)
	}
	// a line inside a paragraph: the paragraph, with the text to select
	if part.Anchor == nil || part.Anchor.Text != "- Như trên" || part.Text != "Như trên" || part.TextOccurrence != 1 ||
		!strings.HasPrefix(part.Reason, "Nên xem lại.") {
		t.Fatalf("partial-line finding: %+v", part)
	}
	if lost.Anchor != nil || lost.Current == "" {
		t.Fatalf("a line not in the document is shown unanchored: %+v", lost)
	}
	if llm.ID != "judgment.5#1" || llm.Title != "Số ký hiệu, địa danh - ngày tháng" || llm.Reason != "Lỗi: thiếu dấu hai chấm sau Số" || llm.Anchor != nil {
		t.Fatalf("model-judged item: %+v", llm)
	}
}

func TestMissingComponentFindingSaysHowToFix(t *testing.T) {
	findings := findingsOf(t, testCongVan(t, nd30Margins))
	for _, f := range findings {
		if strings.HasSuffix(f.CheckID, ".present") {
			if f.Reason != "Bổ sung thành phần này đúng vị trí theo NĐ30." || f.Current != "không có" {
				t.Fatalf("missing component: %+v", f)
			}
			return
		}
	}
	t.Fatalf("no missing component in %+v", findings)
}
