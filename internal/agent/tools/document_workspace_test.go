package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/types"
)

// fakeWorkspace is an in-memory DocumentWorkspaceSource. ws/content is the
// session's first (and active) document; addDocument adds more tabs.
type fakeWorkspace struct {
	ws        types.DocumentWorkspace
	content   []byte
	waited    time.Duration
	sessionID string
	snapshots []string // labels of the snapshots taken
	// documentID is the document the last OpenCurrent/Snapshot targeted.
	documentID string
	others     []fakeDocument
}

type fakeDocument struct {
	ws      types.DocumentWorkspace
	content []byte
}

// addDocument adds another open document (next position) to the session.
func (f *fakeWorkspace) addDocument(id, fileName string, content []byte) *types.DocumentWorkspace {
	f.others = append(f.others, fakeDocument{
		ws:      types.DocumentWorkspace{ID: id, FileName: fileName, Position: 2 + len(f.others), Revision: 1},
		content: content,
	})
	return &f.others[len(f.others)-1].ws
}

func (f *fakeWorkspace) find(documentID string) (*types.DocumentWorkspace, []byte, error) {
	if documentID == "" || documentID == f.ws.ID {
		ws := f.ws
		return &ws, f.content, nil
	}
	for _, d := range f.others {
		if d.ws.ID == documentID {
			ws := d.ws
			return &ws, d.content, nil
		}
	}
	return nil, nil, errors.New("document not found")
}

// Snapshot records the label and returns the next revision sequence.
func (f *fakeWorkspace) Snapshot(_ context.Context, _ uint64, sessionID, documentID, label, source string, wait time.Duration) (*types.DocumentRevision, error) {
	if source != types.DocumentRevisionSourceAI {
		return nil, errors.New("source must be ai")
	}
	f.sessionID, f.waited, f.documentID = sessionID, wait, documentID
	f.snapshots = append(f.snapshots, label)
	return &types.DocumentRevision{Seq: 10 + len(f.snapshots), Label: label, Source: source}, nil
}

func newFakeWorkspace(content []byte) *fakeWorkspace {
	return &fakeWorkspace{ws: types.DocumentWorkspace{ID: "ws-1", FileName: "cong-van.docx", Revision: 3, Position: 1}, content: content}
}

// fakeWorkspaceWithID is newFakeWorkspace with its own document ID, for
// tests whose format-check state must not collide.
func fakeWorkspaceWithID(content []byte, id string) *fakeWorkspace {
	f := newFakeWorkspace(content)
	f.ws.ID = id
	return f
}

func (f *fakeWorkspace) GetBySession(_ context.Context, _ uint64, sessionID string) (*types.DocumentWorkspace, error) {
	f.sessionID = sessionID
	ws := f.ws
	return &ws, nil
}

func (f *fakeWorkspace) Get(_ context.Context, _ uint64, sessionID, documentID string) (*types.DocumentWorkspace, error) {
	f.sessionID = sessionID
	ws, _, err := f.find(documentID)
	return ws, err
}

func (f *fakeWorkspace) List(_ context.Context, _ uint64, sessionID string) ([]*types.DocumentWorkspace, error) {
	f.sessionID = sessionID
	first := f.ws
	out := []*types.DocumentWorkspace{&first}
	for _, d := range f.others {
		ws := d.ws
		out = append(out, &ws)
	}
	return out, nil
}

func (f *fakeWorkspace) OpenCurrent(_ context.Context, _ uint64, sessionID, documentID string) (io.ReadCloser, *types.DocumentWorkspace, error) {
	f.sessionID, f.documentID = sessionID, documentID
	ws, content, err := f.find(documentID)
	if err != nil {
		return nil, nil, err
	}
	return io.NopCloser(bytes.NewReader(content)), ws, nil
}

const testWNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`

// para renders one paragraph with uniform run properties.
func testPara(text, align, font string, sizePt int, bold, italic bool) string {
	var ppr, rpr string
	if align != "" {
		ppr = `<w:pPr><w:jc w:val="` + align + `"/></w:pPr>`
	}
	if font != "" {
		rpr += `<w:rFonts w:ascii="` + font + `" w:hAnsi="` + font + `" w:cs="` + font + `"/>`
	}
	if bold {
		rpr += `<w:b/>`
	}
	if italic {
		rpr += `<w:i/>`
	}
	if sizePt > 0 {
		sz := itoaTest(sizePt * 2)
		rpr += `<w:sz w:val="` + sz + `"/><w:szCs w:val="` + sz + `"/>`
	}
	return `<w:p>` + ppr + `<w:r><w:rPr>` + rpr + `</w:rPr><w:t xml:space="preserve">` + text + `</w:t></w:r></w:p>`
}

func itoaTest(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// buildTestDocx zips a minimal .docx around body markup; margins are in
// twips (top, right, bottom, left).
func buildTestDocx(t *testing.T, body string, margins [4]int) []byte {
	t.Helper()
	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document ` + testWNS + `><w:body>` + body +
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="` + itoaTest(margins[0]) + `" w:right="` + itoaTest(margins[1]) +
		`" w:bottom="` + itoaTest(margins[2]) + `" w:left="` + itoaTest(margins[3]) + `" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr></w:body></w:document>`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct{ name, data string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", doc},
		{"word/styles.xml", `<?xml version="1.0" encoding="UTF-8"?><w:styles ` + testWNS + `><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman"/><w:sz w:val="28"/></w:rPr></w:rPrDefault></w:docDefaults></w:styles>`},
	} {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A4 with NĐ30 margins: top/bottom 20mm, right 15mm, left 30mm.
var nd30Margins = [4]int{1134, 851, 1134, 1701}

// testCongVan is a short công văn: header lines, số ký hiệu, trích yếu and
// a body typed in Arial 12 with left alignment.
func testCongVan(t *testing.T, margins [4]int) []byte {
	body := strings.Join([]string{
		testPara("UBND TỈNH THỪA THIÊN HUẾ", "center", "Times New Roman", 13, false, false),
		testPara("SỞ NỘI VỤ", "center", "Times New Roman", 13, true, false),
		testPara("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", "center", "Times New Roman", 13, true, false),
		testPara("Độc lập - Tự do - Hạnh phúc", "center", "Times New Roman", 14, true, false),
		testPara("Số: 12/SNV-VP", "center", "Times New Roman", 13, false, false),
		testPara("Huế, ngày 05 tháng 10 năm 2026", "center", "Times New Roman", 14, false, true),
		testPara("V/v triển khai công tác cải cách hành chính", "left", "Times New Roman", 13, false, false),
		testPara("Kính gửi: Ủy ban nhân dân các huyện.", "center", "Times New Roman", 14, false, false),
		testPara("Sở Nội vụ đề nghị các đơn vị triển khai công tác cải cách hành chính năm 2026.", "left", "Arial", 12, false, false),
		testPara("Đề nghị các đơn vị báo cáo kết quả trước ngày 30 tháng 11 năm 2026.", "left", "Arial", 12, false, false),
		testPara("Trên đây là nội dung đề nghị của Sở Nội vụ./.", "left", "Arial", 12, false, false),
		testPara("GIÁM ĐỐC", "center", "Times New Roman", 14, true, false),
		testPara("Nguyễn Văn A", "center", "Times New Roman", 14, true, false),
		testPara("Nơi nhận:", "left", "Times New Roman", 12, true, true),
		testPara("- Như trên;", "left", "Times New Roman", 11, false, false),
		testPara("- Lưu: VT.", "left", "Times New Roman", 11, false, false),
	}, "")
	return buildTestDocx(t, body, margins)
}

func toolCtx() context.Context {
	return context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
}

func runTool(t *testing.T, tool interface {
	Execute(context.Context, json.RawMessage) (*types.ToolResult, error)
}, args string,
) *types.ToolResult {
	t.Helper()
	res, err := tool.Execute(toolCtx(), json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// patchDocx rewrites word/document.xml of a package with old→new pairs.
func patchDocx(t *testing.T, docx []byte, pairs ...string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(docx), int64(len(docx)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if f.Name == "word/document.xml" {
			s := string(data)
			for i := 0; i+1 < len(pairs); i += 2 {
				if !strings.Contains(s, pairs[i]) {
					t.Fatalf("patch target %q not found", pairs[i])
				}
				s = strings.Replace(s, pairs[i], pairs[i+1], 1)
			}
			data = []byte(s)
		}
		w, err := zw.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func documentPartXML(t *testing.T, docx []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(docx), int64(len(docx)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			b, err := io.ReadAll(rc)
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
	}
	t.Fatal("no word/document.xml")
	return ""
}

// ---------------------------------------------------------------------------
// read_document_outline

func TestReadDocumentOutlineListsParagraphs(t *testing.T) {
	ws := newFakeWorkspace(docxFixture(t))
	res := runTool(t, NewReadDocumentOutlineTool(ws, nil, "sess-1"), `{}`)
	if !res.Success {
		t.Fatalf("result: %+v", res)
	}
	for _, want := range []string{
		"cong-van.docx", "phiên bản 3",
		"[2] (quoc_hieu) Times New Roman 13 đậm, giữa | CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM",
		"[6] (trich_yeu) Arial 14 đậm, giữa | CÔNG VĂN",
		"[8] (noi_dung) Arial 14, đều | Phòng Quản lý Y",
		"[10] (chuc_danh) Arial 14 đậm, phải | PHÒNG QUẢN LÝ Y",
	} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("outline lacks %q:\n%s", want, res.Output)
		}
	}
	if ws.sessionID != "sess-1" || len(ws.snapshots) != 0 {
		t.Fatalf("outline must only read its session (session %q, snapshots %v)", ws.sessionID, ws.snapshots)
	}
	if res.Data["paragraph_count"] != 15 || res.Data["document_revision"] != 3 || res.Data["to"] != 15 {
		t.Fatalf("data: %+v", res.Data)
	}
}

func TestReadDocumentOutlinePagesAndFilters(t *testing.T) {
	ws := newFakeWorkspace(docxFixture(t))
	res := runTool(t, NewReadDocumentOutlineTool(ws, nil, "s"), `{"from":10,"limit":2}`)
	if !res.Success || !strings.Contains(res.Output, "[10] ") || !strings.Contains(res.Output, "[11] ") ||
		strings.Contains(res.Output, "[12] ") || !strings.Contains(res.Output, "from=12") {
		t.Fatalf("paging: %+v", res)
	}
	if res.Data["from"] != 10 || res.Data["to"] != 12 {
		t.Fatalf("data: %+v", res.Data)
	}
	res = runTool(t, NewReadDocumentOutlineTool(ws, nil, "s"), `{"component":"signature"}`)
	if !strings.Contains(res.Output, "[10] ") || !strings.Contains(res.Output, "[11] ") || strings.Contains(res.Output, "[9] ") {
		t.Fatalf("aggregate component filter:\n%s", res.Output)
	}
	res = runTool(t, NewReadDocumentOutlineTool(ws, nil, "s"), `{"component":"noi_dung"}`)
	lines := 0
	for _, l := range strings.Split(res.Output, "\n") {
		if strings.HasPrefix(l, "[") {
			lines++
			if !strings.Contains(l, "(noi_dung)") {
				t.Errorf("component filter leaked %q", l)
			}
		}
	}
	if lines != 2 {
		t.Fatalf("want the 2 body paragraphs:\n%s", res.Output)
	}
}

func TestDocumentToolsNeedTenant(t *testing.T) {
	ws := newFakeWorkspace(docxFixture(t))
	res, err := NewReadDocumentOutlineTool(ws, nil, "s").Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil || res.Success {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// ---------------------------------------------------------------------------
// rewrite_paragraphs

func runToolCtx(t *testing.T, ctx context.Context, tool interface {
	Execute(context.Context, json.RawMessage) (*types.ToolResult, error)
}, args string,
) *types.ToolResult {
	t.Helper()
	res, err := tool.Execute(ctx, json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestRewriteParagraphsPlansOpsForTheSelection(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	before := append([]byte(nil), ws.content...)
	ctx := selectionCtx("Sở Nội vụ đề nghị các đơn vị triển khai công tác cải cách hành chính năm 2026.\n" +
		"Đề nghị các đơn vị báo cáo kết quả trước ngày 30 tháng 11 năm 2026.\nTrên đây là nội dung đề nghị")
	res := runToolCtx(t, ctx, NewRewriteParagraphsTool(ws, "sess-1"), `{"edits":[
		{"paragraph":8,"old":"năm 2026","new":"năm 2027"},
		{"match":"Trên đây là nội dung","new":"Trên đây là đề nghị của Sở Nội vụ./."},
		{"paragraph":8,"old":"Sở Nội vụ","new":"Sở Nội vụ tỉnh"}
	],"note":"cập nhật năm"}`)
	if !res.Success || res.Data["planned"] != 3 || res.Data["failed"] != 0 {
		t.Fatalf("result: %+v", res)
	}
	if !bytes.Equal(before, ws.content) {
		t.Fatal("the tool must not write the document")
	}
	if len(ws.snapshots) != 1 || ws.snapshots[0] != "ai: viết lại đoạn văn" || ws.waited != snapshotWait || res.Data["snapshot_seq"] != 11 {
		t.Fatalf("snapshot: %v waited %v data %v", ws.snapshots, ws.waited, res.Data["snapshot_seq"])
	}
	ops := resultOps(t, res)
	p8 := "Sở Nội vụ đề nghị các đơn vị triển khai công tác cải cách hành chính năm 2026."
	if len(ops) != 3 || ops[0].Op != OpReplaceText || *ops[0].Old != "năm 2026" || *ops[0].New != "năm 2027" ||
		ops[0].Anchor.Text != p8 || ops[0].Anchor.Occurrence != 1 {
		t.Fatalf("op 0: %+v", ops[0])
	}
	if ops[1].Op != OpReplaceParagraph || ops[1].Old != nil || *ops[1].New != "Trên đây là đề nghị của Sở Nội vụ./." ||
		ops[1].Anchor.Text != "Trên đây là nội dung đề nghị của Sở Nội vụ./." {
		t.Fatalf("op 1: %+v", ops[1])
	}
	// the third edit anchors on paragraph 8 as the first op left it
	if ops[2].Anchor.Text != strings.Replace(p8, "năm 2026", "năm 2027", 1) {
		t.Fatalf("op 2 anchor: %+v", ops[2].Anchor)
	}
	for _, want := range []string{"Sẽ sửa 3 chỗ", "Đoạn [8]: “năm 2026” → “năm 2027”", "cập nhật năm", "Ctrl+Z"} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("output lacks %q:\n%s", want, res.Output)
		}
	}
	if strings.Contains(res.Output, "track changes") {
		t.Error("the output must not mention track changes")
	}
	applyPlan(t, ws, res)
	l := docformat.InspectDocx(ws.content)
	if got := l.Paragraphs[8].Text; !strings.Contains(got, "Sở Nội vụ tỉnh") || !strings.Contains(got, "năm 2027") {
		t.Fatalf("paragraph 8 after the plan = %q", got)
	}
}

func TestRewriteParagraphsNeedsASelection(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	res := runTool(t, NewRewriteParagraphsTool(ws, "s"), `{"edits":[{"paragraph":8,"old":"năm 2026","new":"năm 2027"}]}`)
	if res.Success || !strings.Contains(res.Error, "bôi đen") || len(ws.snapshots) != 0 {
		t.Fatalf("no selection must refuse before snapshotting: %+v", res)
	}
	// an edit outside the selected passage is refused
	ctx := selectionCtx("Đề nghị các đơn vị báo cáo kết quả")
	res = runToolCtx(t, ctx, NewRewriteParagraphsTool(ws, "s"), `{"edits":[{"paragraph":12,"new":"Trần Văn B"}]}`)
	if res.Success || !strings.Contains(res.Error, "không nằm trong phần người dùng đã bôi đen") || res.Data["failed"] != 1 {
		t.Fatalf("an edit outside the selection: %+v", res)
	}
	if ops := resultOps(t, res); len(ops) != 0 {
		t.Fatalf("ops: %+v", ops)
	}
	// inside it, the same call works
	res = runToolCtx(t, ctx, NewRewriteParagraphsTool(ws, "s"), `{"edits":[{"paragraph":9,"old":"báo cáo kết quả","new":"gửi báo cáo kết quả"}]}`)
	if !res.Success || len(resultOps(t, res)) != 1 {
		t.Fatalf("an edit inside the selection: %+v", res)
	}
}

func TestRewriteParagraphsAmbiguousMatchFails(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	res := runToolCtx(t, selectionCtx("các đơn vị"), NewRewriteParagraphsTool(ws, "s"), `{"edits":[{"match":"các đơn vị","new":"x"}]}`)
	if res.Success {
		t.Fatalf("an ambiguous match must not be guessed: %+v", res)
	}
	for _, want := range []string{"khớp nhiều đoạn", "[8]", "[9]", "paragraph"} {
		if !strings.Contains(res.Error, want) {
			t.Errorf("error lacks %q: %s", want, res.Error)
		}
	}
	if res.Data["failed"] != 1 {
		t.Fatalf("data: %+v", res.Data)
	}
}

func TestRewriteParagraphsPartialFailure(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	res := runToolCtx(t, selectionCtx("Đề nghị các đơn vị báo cáo kết quả trước ngày 30 tháng 11 năm 2026."), NewRewriteParagraphsTool(ws, "s"), `{"edits":[
		{"paragraph":9,"old":"không có","new":"x"},
		{"paragraph":9,"old":"30 tháng 11","new":"15 tháng 12"}
	]}`)
	if !res.Success || res.Data["planned"] != 1 || res.Data["failed"] != 1 || len(resultOps(t, res)) != 1 {
		t.Fatalf("result: %+v", res)
	}
	if !strings.Contains(res.Output, "KHÔNG sửa") {
		t.Fatalf("output must report the failed edit:\n%s", res.Output)
	}
}

func TestRewriteParagraphsValidatesInput(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	ctx := selectionCtx("Sở Nội vụ đề nghị")
	var edits []string
	for i := 0; i < maxRewriteEdits+1; i++ {
		edits = append(edits, `{"paragraph":8,"new":"x"}`)
	}
	for name, args := range map[string]string{
		"too many":        `{"edits":[` + strings.Join(edits, ",") + `]}`,
		"empty new":       `{"edits":[{"paragraph":8,"new":"  "}]}`,
		"missing new":     `{"edits":[{"paragraph":8}]}`,
		"no edits":        `{"edits":[]}`,
		"index too large": `{"edits":[{"paragraph":99,"new":"x"}]}`,
	} {
		if res := runToolCtx(t, ctx, NewRewriteParagraphsTool(ws, "s"), args); res.Success {
			t.Errorf("%s: accepted: %+v", name, res)
		}
	}
	// deleting part of a paragraph is allowed: an empty replacement
	res := runToolCtx(t, ctx, NewRewriteParagraphsTool(ws, "s"), `{"edits":[{"paragraph":8,"old":"Sở Nội vụ ","new":""}]}`)
	ops := resultOps(t, res)
	if !res.Success || len(ops) != 1 || ops[0].New == nil || *ops[0].New != "" {
		t.Fatalf("substring deletion: %+v", res)
	}
}

// ---------------------------------------------------------------------------
// apply_format_fixes

func appliedFixes(t *testing.T, res *types.ToolResult) map[string]AppliedFormatFix {
	t.Helper()
	list, ok := res.Data["applied"].([]AppliedFormatFix)
	if !ok {
		t.Fatalf("applied = %T", res.Data["applied"])
	}
	out := map[string]AppliedFormatFix{}
	for _, f := range list {
		out[f.CheckID] = f
	}
	return out
}

func TestApplyFormatFixesDryRunPlansWithoutWriting(t *testing.T) {
	ws := newFakeWorkspace(docxFixture(t))
	before := append([]byte(nil), ws.content...)
	res := runTool(t, NewApplyFormatFixesTool(ws, nil, "s"), `{"dry_run":true}`)
	if !res.Success || !bytes.Equal(before, ws.content) || len(resultOps(t, res)) != 0 || len(ws.snapshots) != 0 {
		t.Fatalf("a dry run plans no ops and takes no snapshot: %+v", res)
	}
	if res.Data["dry_run"] != true || res.Data["snapshot_seq"] != 0 || res.Data["document_type"] != "cong_van" {
		t.Fatalf("data: %+v", res.Data)
	}
	fixes := appliedFixes(t, res)
	if f := fixes["noi_dung.font"]; len(f.Paragraphs) != 2 || f.Paragraphs[0] != 8 || f.Paragraphs[1] != 9 ||
		!strings.Contains(f.Change, "Times New Roman") {
		t.Fatalf("noi_dung.font plan: %+v", f)
	}
	// công văn: trích yếu 12-13, not bold
	if f := fixes["trich_yeu.size"]; len(f.Paragraphs) != 2 || f.Change != "cỡ chữ 13" {
		t.Fatalf("trich_yeu.size plan: %+v", f)
	}
	if f := fixes["trich_yeu.bold"]; len(f.Paragraphs) != 2 || f.Change != "bỏ in đậm" {
		t.Fatalf("trich_yeu.bold plan: %+v", f)
	}
	if !strings.Contains(res.Output, "KẾ HOẠCH") || !strings.Contains(res.Output, "dry_run=false") {
		t.Fatalf("output: %s", res.Output)
	}
}

func TestApplyFormatFixesPlansFormattingOps(t *testing.T) {
	content := patchDocx(t, docxFixture(t),
		// quốc hiệu left-aligned (rule: center)
		`<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:rPr><w:sz w:val="26"/><w:b/></w:rPr><w:t xml:space="preserve">CỘNG HÒA`,
		`<w:p><w:pPr><w:jc w:val="left"/></w:pPr><w:r><w:rPr><w:sz w:val="26"/><w:b/></w:rPr><w:t xml:space="preserve">CỘNG HÒA`,
		// closing body paragraph centered (rule: justify or left)
		`<w:p><w:pPr><w:jc w:val="both"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial"/><w:sz w:val="28"/></w:rPr><w:t xml:space="preserve">Trên`,
		`<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial"/><w:sz w:val="28"/></w:rPr><w:t xml:space="preserve">Trên`,
		// left margin 20mm (rule: 30-35mm), top margin 15mm (rule 20-25)
		`w:top="1360"`, `w:top="850"`,
		`w:left="1920"`, `w:left="1134"`,
	)
	ws := newFakeWorkspace(content)
	res := runTool(t, NewApplyFormatFixesTool(ws, nil, "s"), `{}`)
	if !res.Success || res.Data["snapshot_seq"] != 11 || len(ws.snapshots) != 1 || ws.snapshots[0] != "ai: chuẩn hóa thể thức" {
		t.Fatalf("result: %+v", res)
	}
	ops := applyPlan(t, ws, res)
	byText := map[string]DocumentOp{}
	var page *DocumentOp
	for i, op := range ops {
		switch op.Op {
		case OpFormatParagraph:
			if op.Anchor.Occurrence != 1 {
				t.Errorf("anchor %+v", op.Anchor)
			}
			if _, dup := byText[op.Anchor.Text]; dup {
				t.Errorf("paragraph %q has more than one op: edits must be merged", op.Anchor.Text)
			}
			byText[op.Anchor.Text] = op
		case OpPageSetup:
			page = &ops[i]
		default:
			t.Errorf("unexpected op %+v", op)
		}
	}
	// trích yếu: size and bold from two rules, merged into one op
	if op := byText["CÔNG VĂN"]; op.SizePt == nil || *op.SizePt != 13 || op.Bold == nil || *op.Bold {
		t.Errorf("trích yếu op: %+v", op)
	}
	if op := byText["CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM"]; op.Alignment != "center" {
		t.Errorf("quốc hiệu op: %+v", op)
	}
	if op := byText["Trên đây là nội dung công văn, trân trọng cảm ơn."]; op.Alignment != "both" || op.Font != "Times New Roman" {
		t.Errorf("closing op: %+v", op)
	}
	if page == nil || page.MarginsMm["left"] != 30 || page.MarginsMm["top"] != 20 || len(page.MarginsMm) != 2 || page.A4 {
		t.Fatalf("pageSetup: %+v", page)
	}
	fixes := appliedFixes(t, res)
	for _, id := range []string{"noi_dung.font", "noi_dung.align", "quoc_hieu.align", "trich_yeu.size", "page.margin.left", "page.margin.top"} {
		if _, ok := fixes[id]; !ok {
			t.Errorf("%s not applied: %+v", id, fixes)
		}
	}
	if _, ok := fixes["page.margin.right"]; ok {
		t.Error("a passing margin must not be touched")
	}
	if !strings.Contains(res.Output, "Sẽ áp dụng") || !strings.Contains(res.Output, "Ctrl+Z") || strings.Contains(res.Output, "track changes") {
		t.Fatalf("output: %s", res.Output)
	}

	l := docformat.InspectDocx(ws.content)
	p := l.Paragraphs
	str := func(s *string) string {
		if s == nil {
			return "<nil>"
		}
		return *s
	}
	if str(p[8].FontName) != "Times New Roman" || str(p[9].FontName) != "Times New Roman" {
		t.Errorf("body font = %s / %s", str(p[8].FontName), str(p[9].FontName))
	}
	if p[9].Alignment != "justify" || p[2].Alignment != "center" {
		t.Errorf("alignment: body %q, quốc hiệu %q", p[9].Alignment, p[2].Alignment)
	}
	if p[6].SizePt == nil || *p[6].SizePt != 13 || p[6].Bold == nil || *p[6].Bold {
		t.Errorf("trích yếu size=%v bold=%v", p[6].SizePt, p[6].Bold)
	}
	sec := l.Sections[0]
	if sec.MarginLeftMM == nil || *sec.MarginLeftMM < 29.9 || *sec.MarginLeftMM > 30.1 {
		t.Errorf("left margin = %v", sec.MarginLeftMM)
	}
	if sec.MarginTopMM == nil || *sec.MarginTopMM < 19.9 || *sec.MarginTopMM > 20.1 {
		t.Errorf("top margin = %v", sec.MarginTopMM)
	}
	if sec.MarginRightMM == nil || *sec.MarginRightMM < 19 {
		t.Errorf("right margin changed: %v", sec.MarginRightMM)
	}
	// once the editor applied the plan, the checker passes the fixed rules
	after := docformat.Check(context.Background(), ws.content, docformat.Options{Segmenter: docformat.SegmenterHeuristic})
	status := map[string]string{}
	for _, c := range after.Checks {
		status[c.ID] = c.Status
	}
	for id := range fixes {
		if status[id] != docformat.StatusPass {
			t.Errorf("%s after the fix: %s", id, status[id])
		}
	}
	// re-checking finds no fixable failure left
	again := runTool(t, NewApplyFormatFixesTool(ws, nil, "s"), `{"dry_run":true}`)
	if left := appliedFixes(t, again); len(left) != 0 {
		t.Fatalf("fixes left after applying: %+v", left)
	}
}

func TestApplyFormatFixesFiltersAndListsManualFixes(t *testing.T) {
	ws := newFakeWorkspace(docxFixture(t))
	res := runTool(t, NewApplyFormatFixesTool(ws, nil, "s"), `{"check_ids":["noi_dung.font","quoc_hieu.bold","no.such.rule"]}`)
	if !res.Success || len(applyPlan(t, ws, res)) != 2 {
		t.Fatalf("result: %+v", res)
	}
	fixes := appliedFixes(t, res)
	if len(fixes) != 1 || len(fixes["noi_dung.font"].Paragraphs) != 2 {
		t.Fatalf("only the requested rule is applied: %+v", fixes)
	}
	skipped, _ := res.Data["skipped"].([]SkippedFormatFix)
	reasons := map[string]string{}
	for _, s := range skipped {
		reasons[s.CheckID] = s.Reason
	}
	if !strings.Contains(reasons["quoc_hieu.bold"], "đang đạt") || !strings.Contains(reasons["no.such.rule"], "không có quy tắc") {
		t.Fatalf("skipped: %+v", skipped)
	}

	// a flat document: missing components and their order cannot be fixed
	flat := newFakeWorkspace(testCongVan(t, nd30Margins))
	res = runTool(t, NewApplyFormatFixesTool(flat, nil, "s"), `{"dry_run":true}`)
	manual, _ := res.Data["manual"].([]ManualFormatFix)
	ids := map[string]bool{}
	for _, m := range manual {
		ids[m.CheckID] = true
		if m.Desc == "" {
			t.Errorf("%s has no description", m.CheckID)
		}
	}
	if !ids["component.quoc_hieu.present"] || !ids["component.order"] {
		t.Fatalf("manual: %+v", manual)
	}
	if !strings.Contains(res.Output, "Cần sửa thủ công") {
		t.Fatalf("output: %s", res.Output)
	}
}

func TestApplyFormatFixesSkipsTabSplitLines(t *testing.T) {
	b, err := os.ReadFile("../../docformat/testdata/parity/fx_flat_cong_van.docx")
	if err != nil {
		t.Fatal(err)
	}
	ws := newFakeWorkspace(b)
	res := runTool(t, NewApplyFormatFixesTool(ws, nil, "s"), `{"dry_run":true,"check_ids":["quoc_hieu.bold"]}`)
	skipped, _ := res.Data["skipped"].([]SkippedFormatFix)
	if len(skipped) != 1 || !strings.Contains(skipped[0].Reason, "tab") || len(appliedFixes(t, res)) != 0 {
		t.Fatalf("a tab-split header line must be left for a manual fix: %+v", res.Data)
	}
}

// ---------------------------------------------------------------------------
// check_document_format on the workspace

func TestCheckDocumentFormatReadsWorkspace(t *testing.T) {
	ws := newFakeWorkspace(docxFixture(t))
	tool := NewCheckDocumentFormatToolForWorkspace(ws, nil, "sess-9")
	res := runTool(t, tool, `{"file_name":"ignored.docx"}`)
	if !res.Success || res.Data["file_name"] != "cong-van.docx" || res.Data["document_revision"] != 3 {
		t.Fatalf("result: %+v", res.Data)
	}
	if ws.sessionID != "sess-9" || len(ws.snapshots) != 0 {
		t.Fatalf("checking must only read (session %q, snapshots %v)", ws.sessionID, ws.snapshots)
	}
	if !strings.Contains(tool.Description(), "open in this conversation's editor") {
		t.Fatal("workspace description must name the editor document")
	}
	if strings.Contains(NewCheckDocumentFormatTool(nil, nil, "s").Description(), "editor") {
		t.Fatal("the upload variant keeps its description")
	}
}

func TestDocumentToolSchemasAreValidJSON(t *testing.T) {
	for _, tool := range []BaseTool{readDocumentOutlineTool, rewriteParagraphsTool, applyFormatFixesTool,
		insertParagraphsTool, markPassagesTool, checkSpellingTool} {
		var parsed map[string]any
		if err := json.Unmarshal(tool.schema, &parsed); err != nil || parsed["type"] != "object" {
			t.Errorf("%s schema: %v", tool.name, err)
		}
	}
	if !CanRunConcurrently(ToolReadDocumentOutline) || CanRunConcurrently(ToolRewriteParagraphs) ||
		CanRunConcurrently(ToolApplyFormatFixes) || CanRunConcurrently(ToolInsertParagraphs) || CanRunConcurrently(ToolMarkPassages) ||
		CanRunConcurrently(ToolCheckSpelling) {
		t.Fatal("only the outline read may run concurrently")
	}
}

// ---------------------------------------------------------------------------
// apply_format_fixes: structure guard and model labels

func TestApplyFormatFixesRefusesAnUnrecognisedStructure(t *testing.T) {
	// a flat document: the positional heuristic finds no quốc hiệu or chữ ký
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	before := append([]byte(nil), ws.content...)
	res := runTool(t, NewApplyFormatFixesTool(ws, nil, "s"), `{}`)
	if !res.Success || !bytes.Equal(before, ws.content) || len(resultOps(t, res)) != 0 {
		t.Fatalf("an unreliable structure gets no ops: %+v", res)
	}
	if res.Data["blocked"] != true || res.Data["dry_run"] != true {
		t.Fatalf("data: %+v", res.Data)
	}
	missing, _ := res.Data["missing_components"].([]string)
	if !containsString(missing, "quoc_hieu") {
		t.Fatalf("missing_components = %v", missing)
	}
	if !strings.HasPrefix(res.Output, "⚠ CẢNH BÁO") || !strings.Contains(res.Output, "CHƯA sửa") ||
		!strings.Contains(res.Output, "(quoc_hieu)") || !strings.Contains(res.Output, "force=true") ||
		!strings.Contains(res.Output, "document_type") {
		t.Fatalf("output: %s", res.Output)
	}
	if len(appliedFixes(t, res)) == 0 {
		t.Fatal("the plan is still returned")
	}

	// the user confirmed: force applies the same plan
	res = runTool(t, NewApplyFormatFixesTool(ws, nil, "s"), `{"force":true}`)
	if !res.Success || len(applyPlan(t, ws, res)) == 0 || res.Data["blocked"] != false || res.Data["dry_run"] != false ||
		strings.Contains(res.Output, "CẢNH BÁO") {
		t.Fatalf("force: %+v", res)
	}
}

func TestApplyFormatFixesNotesLargeChanges(t *testing.T) {
	body := `<w:p><w:pPr><w:jc w:val="both"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial"/><w:sz w:val="28"/></w:rPr><w:t xml:space="preserve">Trên`
	extra := strings.Repeat(`<w:p><w:pPr><w:jc w:val="both"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial"/><w:sz w:val="28"/></w:rPr><w:t xml:space="preserve">Đề nghị các đơn vị thực hiện nghiêm nội dung này.</w:t></w:r></w:p>`, 10)
	ws := newFakeWorkspace(patchDocx(t, docxFixture(t), body, extra+body))
	res := runTool(t, NewApplyFormatFixesTool(ws, nil, "s"), `{"check_ids":["noi_dung.font"]}`)
	if !res.Success {
		t.Fatalf("result: %+v", res)
	}
	// eleven equal body lines: one op per paragraph, told apart by occurrence
	ops := resultOps(t, res)
	occ := 0
	for _, op := range ops {
		if op.Anchor.Text == "Đề nghị các đơn vị thực hiện nghiêm nội dung này." {
			occ++
			if op.Anchor.Occurrence != occ {
				t.Fatalf("occurrence %d, want %d", op.Anchor.Occurrence, occ)
			}
		}
	}
	if occ != 10 || len(ops) != 12 {
		t.Fatalf("ops: %d (%d repeated lines)", len(ops), occ)
	}
	applyPlan(t, ws, res)
	if s := checkStatus(t, ws.content)["noi_dung.font"]; s != docformat.StatusPass {
		t.Fatalf("noi_dung.font after the plan: %s", s)
	}
	if n := len(appliedFixes(t, res)["noi_dung.font"].Paragraphs); n != 12 {
		t.Fatalf("noi_dung.font changed %d paragraphs", n)
	}
	if !strings.HasPrefix(res.Output, "Lưu ý: lần sửa này đổi định dạng của 12 đoạn") {
		t.Fatalf("output: %s", res.Output)
	}
	// dry runs and small fixes carry no note
	small := newFakeWorkspace(docxFixture(t))
	if res := runTool(t, NewApplyFormatFixesTool(small, nil, "s"), `{}`); strings.Contains(res.Output, "Lưu ý") {
		t.Fatalf("small fix noted: %s", res.Output)
	}
}

// congVanLabels labels testCongVan the way a model would.
const congVanLabels = `{"document_type":"cong_van","labels":{
	"0":"co_quan_chu_quan","1":"co_quan_ban_hanh","2":"quoc_hieu","3":"tieu_ngu","4":"so_ky_hieu",
	"5":"dia_danh_ngay_thang","6":"trich_yeu","7":"kinh_gui","8":"noi_dung","9":"noi_dung","10":"noi_dung",
	"11":"chuc_danh","12":"nguoi_ky","13":"noi_nhan","14":"noi_nhan","15":"noi_nhan"}}`

func TestDocumentToolsHonourModelLabels(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	model := &fakeChat{reply: congVanLabels}
	res := runTool(t, NewReadDocumentOutlineTool(ws, model, "s"), `{}`)
	if !res.Success {
		t.Fatalf("result: %+v", res)
	}
	for _, want := range []string{"loại văn bản nhận dạng: cong_van", "[2] (quoc_hieu)", "[6] (trich_yeu)", "[11] (chuc_danh)", "[13] (noi_nhan)"} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("outline lacks %q:\n%s", want, res.Output)
		}
	}
	if model.opts == nil || model.opts.Thinking == nil || *model.opts.Thinking {
		t.Fatal("labelling must call the model with thinking off")
	}

	// with the structure recognised the fixes apply without force
	res = runTool(t, NewApplyFormatFixesTool(ws, model, "s"), `{}`)
	if !res.Success || len(resultOps(t, res)) == 0 || res.Data["blocked"] != false || res.Data["segmentation"] != "llm" {
		t.Fatalf("result: %+v", res)
	}
	fixes := appliedFixes(t, res)
	if f := fixes["noi_dung.font"]; len(f.Paragraphs) != 3 || f.Paragraphs[0] != 8 {
		t.Fatalf("noi_dung.font: %+v", fixes)
	}
	for id, f := range fixes {
		for _, p := range f.Paragraphs {
			if p == 11 || p == 12 {
				t.Errorf("%s touched the signature block (paragraph %d): labels ignored", id, p)
			}
		}
	}

	// a failing model degrades to the heuristic
	broken := newFakeWorkspace(testCongVan(t, nd30Margins))
	res = runTool(t, NewApplyFormatFixesTool(broken, &fakeChat{err: errors.New("model down")}, "s"), `{}`)
	if !res.Success || res.Data["segmentation"] != "heuristic" || res.Data["blocked"] != true {
		t.Fatalf("fallback: %+v", res.Data)
	}
}

func TestApplyFormatFixesMergesEditsPerParagraph(t *testing.T) {
	content := docxFixture(t)
	report, err := segmentReport(context.Background(), content, "", "x.docx", nil)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := docformat.RuleSetForReport(report)
	if err != nil {
		t.Fatal(err)
	}
	plan := buildFormatPlan(report, rs, docformat.InspectDocx(content), nil)
	// trích yếu gets size and bold from two checks: merged into one edit
	if e := plan.paras[6]; e == nil || e.run == nil || e.run.SizePt == nil || e.run.Bold == nil || len(e.runChecks) != 2 {
		t.Fatalf("paragraph 6 edit not merged: %+v", e)
	}
	doc, err := docxedit.Open(content)
	if err != nil {
		t.Fatal(err)
	}
	ops := plan.ops(newVirtualDoc(doc.Paragraphs()))
	n := 0
	for _, op := range ops {
		if op.Anchor != nil && op.Anchor.Text == "CÔNG VĂN" {
			n++
			if *op.SizePt != 13 || *op.Bold {
				t.Fatalf("op: %+v", op)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d ops for paragraph 6", n)
	}
}

// ---------------------------------------------------------------------------
// apply_format_fixes: twip rounding and per-paragraph conformity

func checkStatus(t *testing.T, content []byte) map[string]string {
	t.Helper()
	r := docformat.Check(context.Background(), content, docformat.Options{Segmenter: docformat.SegmenterHeuristic})
	out := map[string]string{}
	for _, c := range r.Checks {
		out[c.ID] = c.Status
	}
	return out
}

func TestApplyFormatFixesMarginSurvivesTwipRounding(t *testing.T) {
	// python-docx Mm(15) → 850 twips = 14.99mm: fails the 15-20mm rule, and
	// 15mm written naively rounds back to the same 850 twips
	content := testCongVan(t, [4]int{1134, 850, 1134, 1701})
	if s := checkStatus(t, content)["page.margin.right"]; s != docformat.StatusFail {
		t.Fatalf("fixture: page.margin.right = %s", s)
	}
	ws := newFakeWorkspace(content)
	res := runTool(t, NewApplyFormatFixesTool(ws, &fakeChat{reply: congVanLabels}, "s"), `{"check_ids":["page.margin.right"]}`)
	if !res.Success {
		t.Fatalf("result: %+v", res)
	}
	ops := applyPlan(t, ws, res)
	if len(ops) != 1 || ops[0].Op != OpPageSetup || ops[0].MarginsMm["right"] != 15.01 {
		t.Fatalf("ops: %+v", ops)
	}
	f, ok := appliedFixes(t, res)["page.margin.right"]
	if !ok || f.Change != "lề phải 15.01mm" {
		t.Fatalf("page.margin.right: %+v\n%s", f, res.Output)
	}
	if xml := documentPartXML(t, ws.content); !strings.Contains(xml, `w:right="851"`) {
		t.Fatalf("15.01mm does not land on 851 twips: %s", xml)
	}
	if s := checkStatus(t, ws.content)["page.margin.right"]; s != docformat.StatusPass {
		t.Fatalf("page.margin.right after the fix: %s", s)
	}
}

func TestGridTargetStaysInRange(t *testing.T) {
	readMM := func(n int) float64 { return round2(float64(n) / twipsPerMM) }
	cur := 14.99
	for _, c := range []struct {
		lo, hi, target float64
		cur            *float64
		want           int
	}{
		{15, 20, 15, &cur, 851}, // lower bound: ceil(850.39)
		{30, 35, 30, nil, 1701}, // ceil(1700.79)
		{15, 20, 20, nil, 1133}, // upper bound: floor(1133.86)
		{209, 211, 210, nil, 11906},
	} {
		n, ok := gridTarget(c.lo, c.hi, c.target, c.cur, twipsPerMM, readMM)
		if !ok || n != c.want || readMM(n) < c.lo || readMM(n) > c.hi {
			t.Errorf("%v-%v target %v: %d (%v mm) ok=%v, want %d", c.lo, c.hi, c.target, n, readMM(n), ok, c.want)
		}
	}
	// a current value already on the rounded grid point moves one step in
	at := 850.0 / twipsPerMM
	if n, _ := gridTarget(15, 20, 15, &at, twipsPerMM, readMM); n != 851 {
		t.Errorf("same as current: %d", n)
	}
	// font sizes on the half-point grid
	half := func(n int) float64 { return float64(n) / 2 }
	if n, ok := gridTarget(13.2, 14, 13.2, nil, 2, half); !ok || n != 27 {
		t.Errorf("size lower bound 13.2 → %d half-points", n)
	}
	if n, ok := gridTarget(12, 13.7, 13.7, nil, 2, half); !ok || n != 27 {
		t.Errorf("size upper bound 13.7 → %d half-points", n)
	}
	rule := docformat.RuleCheck{Prop: "size_pt", Op: "range", Value: []any{13.2, 14.0}}
	if v := paraTarget(rule, 12.0); v != 13.5 {
		t.Errorf("paraTarget size = %v", v)
	}
}

// fourParaBody is a well-formed công văn whose body has four paragraphs,
// the second typed in Arial.
func fourParaBody(t *testing.T) []byte {
	body := strings.Join([]string{
		testPara("UBND TỈNH THỪA THIÊN HUẾ", "center", "Times New Roman", 13, false, false),
		testPara("SỞ NỘI VỤ", "center", "Times New Roman", 13, true, false),
		testPara("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", "center", "Times New Roman", 13, true, false),
		testPara("Độc lập - Tự do - Hạnh phúc", "center", "Times New Roman", 14, true, false),
		testPara("Số: 12/SNV-VP", "center", "Times New Roman", 13, false, false),
		testPara("Huế, ngày 05 tháng 10 năm 2026", "center", "Times New Roman", 14, false, true),
		testPara("V/v triển khai công tác cải cách hành chính", "center", "Times New Roman", 12, false, false),
		testPara("Kính gửi: Ủy ban nhân dân các huyện.", "center", "Times New Roman", 14, false, false),
		testPara("Sở Nội vụ đề nghị các đơn vị triển khai công tác cải cách hành chính năm 2026.", "both", "Times New Roman", 14, false, false),
		testPara("Các đơn vị rà soát thủ tục hành chính thuộc phạm vi quản lý.", "both", "Arial", 14, false, false),
		testPara("Đề nghị các đơn vị báo cáo kết quả trước ngày 30 tháng 11 năm 2026.", "both", "Times New Roman", 14, false, false),
		testPara("Trên đây là nội dung đề nghị của Sở Nội vụ./.", "both", "Times New Roman", 14, false, false),
		testPara("GIÁM ĐỐC", "center", "Times New Roman", 14, true, false),
		testPara("Nguyễn Văn A", "center", "Times New Roman", 14, true, false),
		testPara("Nơi nhận:", "left", "Times New Roman", 12, true, true),
		testPara("- Như trên;", "left", "Times New Roman", 11, false, false),
		testPara("- Lưu: VT.", "left", "Times New Roman", 11, false, false),
	}, "")
	return buildTestDocx(t, body, nd30Margins)
}

const fourParaLabels = `{"document_type":"cong_van","labels":{
	"0":"co_quan_chu_quan","1":"co_quan_ban_hanh","2":"quoc_hieu","3":"tieu_ngu","4":"so_ky_hieu",
	"5":"dia_danh_ngay_thang","6":"trich_yeu","7":"kinh_gui","8":"noi_dung","9":"noi_dung","10":"noi_dung","11":"noi_dung",
	"12":"chuc_danh","13":"nguoi_ky","14":"noi_nhan","15":"noi_nhan","16":"noi_nhan"}}`

// paragraphXML returns the markup of the paragraph containing text.
func paragraphXML(t *testing.T, xml, text string) string {
	t.Helper()
	for _, p := range strings.SplitAfter(xml, "</w:p>") {
		if strings.Contains(p, text) {
			return p
		}
	}
	t.Fatalf("no paragraph with %q", text)
	return ""
}

func TestApplyFormatFixesFixesEachNonConformingParagraph(t *testing.T) {
	content := fourParaBody(t)
	model := &fakeChat{reply: fourParaLabels}
	before, err := segmentReport(context.Background(), content, "", "x.docx", model)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range before.Checks {
		if c.ID == "noi_dung.font" && c.Status != docformat.StatusPass {
			t.Fatalf("fixture: noi_dung.font should pass by majority, got %s", c.Status)
		}
	}

	ws := newFakeWorkspace(content)
	res := runTool(t, NewApplyFormatFixesTool(ws, model, "s"), `{}`)
	if !res.Success || res.Data["blocked"] != false {
		t.Fatalf("result: %+v", res)
	}
	for _, op := range resultOps(t, res) {
		if op.Anchor != nil && (strings.HasPrefix(op.Anchor.Text, "Sở Nội vụ đề nghị") || strings.HasPrefix(op.Anchor.Text, "Trên đây là")) {
			t.Errorf("an op for a conforming body paragraph: %+v", op)
		}
	}
	applyPlan(t, ws, res)
	f, ok := appliedFixes(t, res)["noi_dung.font"]
	if !ok || len(f.Paragraphs) != 1 || f.Paragraphs[0] != 9 || f.ComponentParagraphs != 4 {
		t.Fatalf("noi_dung.font: %+v\n%s", f, res.Output)
	}
	if !strings.Contains(res.Output, "noi_dung.font: phông Times New Roman — 1/4 đoạn [9]") {
		t.Fatalf("output: %s", res.Output)
	}
	for id, f := range appliedFixes(t, res) {
		for _, p := range f.Paragraphs {
			if p >= 8 && p <= 11 && p != 9 {
				t.Errorf("%s touched conforming body paragraph %d", id, p)
			}
		}
	}

	xml := documentPartXML(t, ws.content)
	if p := paragraphXML(t, xml, "Các đơn vị rà soát"); !strings.Contains(p, `w:ascii="Times New Roman"`) {
		t.Fatalf("the Arial paragraph got no font change: %s", p)
	}

	// the labelled report on the fixed bytes: the whole body in Times New Roman
	after, err := segmentReport(context.Background(), ws.content, "", "x.docx", &fakeChat{reply: fourParaLabels})
	if err != nil {
		t.Fatal(err)
	}
	layout := docformat.InspectDocx(ws.content)
	body := after.Components["noi_dung"]
	if body == nil || len(body.Paras) != 4 {
		t.Fatalf("noi_dung after the fix: %+v", body)
	}
	for _, i := range body.Paras {
		if fn := layout.Paragraphs[i].FontName; fn == nil || *fn != "Times New Roman" {
			t.Errorf("body paragraph %d font = %v", i, fn)
		}
	}
	// nothing left to fix paragraph by paragraph in the body
	again := runTool(t, NewApplyFormatFixesTool(ws, &fakeChat{reply: fourParaLabels}, "s"), `{"dry_run":true,"check_ids":["noi_dung.font"]}`)
	if left := appliedFixes(t, again); len(left) != 0 {
		t.Fatalf("fixes left: %+v", left)
	}
	skipped, _ := again.Data["skipped"].([]SkippedFormatFix)
	if len(skipped) != 1 || !strings.Contains(skipped[0].Reason, "đang đạt") {
		t.Fatalf("an explicit passing rule keeps its skip reason: %+v", skipped)
	}
}
