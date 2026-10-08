package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// clarifyDocx is a quy chế of ten Điều; paras non-empty paragraphs in all
// (a heading and paragraphs of body each).
func clarifyDocx(t *testing.T, paras int) []byte {
	t.Helper()
	p := func(text string, bold bool) string { return testPara(text, "left", "Times New Roman", 14, bold, false) }
	var b strings.Builder
	b.WriteString(p("ỦY BAN NHÂN DÂN TỈNH A", true))
	b.WriteString(p("Số: 12/QC-UBND", false))
	b.WriteString(p("QUY CHẾ", true))
	titles := []string{"Phạm vi điều chỉnh", "Đối tượng áp dụng", "Nguyên tắc phối hợp", "Trách nhiệm của Sở Tài chính",
		"Chế độ báo cáo", "Kinh phí thực hiện", "Khen thưởng", "Xử lý vi phạm", "Điều khoản chuyển tiếp", "Tổ chức thực hiện"}
	perArticle := max(1, (paras-3)/10)
	n := 1
	for a := 1; a <= 10; a++ {
		b.WriteString(p(fmt.Sprintf("Điều %d. %s", a, titles[a-1]), true))
		for k := 1; k < perArticle; k++ {
			b.WriteString(p(fmt.Sprintf("Khoản %d: đơn vị thực hiện việc thứ %d theo kế hoạch hằng năm, báo cáo kết quả đúng hạn.", k, n), false))
			n++
		}
	}
	b.WriteString(p("Phòng PA05 tổng hợp báo cáo.", false))
	return buildTestDocx(t, b.String(), [4]int{20, 15, 30, 20})
}

func TestGenericScopeRequest(t *testing.T) {
	for q, want := range map[string]bool{
		"góp ý giúp văn bản này":          true,
		"Xem giúp tôi văn bản này nhé":    true,
		"tóm tắt nội dung chính":          true,
		"văn bản này thế nào?":            true,
		"văn bản này có vấn đề gì không":  true,
		"rà soát vb1":                     true,
		"review giúp mình":                true,
		"kiểm tra số liệu":                false, // a concrete object
		"thời hạn nộp báo cáo là khi nào": false,
		"ai ký văn bản này":               false,
		"góp ý phần tổ chức thực hiện":    false,
		"văn bản này":                     false, // no intent
		"":                                false,
		"góp ý giúp tôi văn bản này với nhé, cái đoạn đầu tiên ấy có ổn không ạ": false, // too long
	} {
		if got := genericScopeRequest(q); got != want {
			t.Errorf("genericScopeRequest(%q) = %v, want %v", q, got, want)
		}
	}
	for q, want := range map[string]bool{
		"kiểm tra thể thức":    true,
		"rà soát lỗi chính tả": true,
		"có đúng NĐ30 không":   true,
		"góp ý giúp":           false,
		"tóm tắt toàn bộ":      false,
	} {
		if got := clearTaskRequest(q); got != want {
			t.Errorf("clearTaskRequest(%q) = %v, want %v", q, got, want)
		}
	}
	for q, want := range map[string]bool{
		"tóm tắt toàn bộ văn bản": true,
		"đọc cả văn bản":          true,
		"góp ý đầy đủ":            true,
		"góp ý giúp":              false,
	} {
		if got := wholeDocumentRequest(q); got != want {
			t.Errorf("wholeDocumentRequest(%q) = %v, want %v", q, got, want)
		}
	}
}

// The gate's decision table: long/short × anchored/generic × task clear ×
// answered before × config off.
func TestDocumentScopeClarificationDecision(t *testing.T) {
	freshDocProfiles(t)
	long := newFakeWorkspace(clarifyDocx(t, 200))
	short := newFakeWorkspace(clarifyDocx(t, 30))
	ask := func(src DocumentWorkspaceSource, ctx context.Context, session, query string, disabled bool) *ScopeClarification {
		t.Helper()
		return DocumentScopeClarification(ctx, src, 7, session, ScopeClarificationInput{Query: query, Disabled: disabled})
	}

	c := ask(long, toolCtx(), "s-clar-1", "góp ý giúp văn bản này", false)
	if c == nil {
		t.Fatal("a generic request about a long document asks")
	}
	if len(c.Documents) != 1 || c.Documents[0].ID != "ws-1" || c.Documents[0].Paragraphs < clarifyTargetParagraphs || !c.Documents[0].Long {
		t.Fatalf("documents: %+v", c.Documents)
	}
	if len(c.Documents[0].Sections) < 10 || !strings.HasPrefix(c.Documents[0].Sections[len(c.Documents[0].Sections)-10].Title, "Điều 1.") {
		t.Fatalf("the sections of its card: %+v", c.Documents[0].Sections)
	}
	if c.Suggested.DocumentID != "ws-1" || c.Query != "góp ý giúp văn bản này" {
		t.Fatalf("suggested %+v query %q", c.Suggested, c.Query)
	}
	keys := []string{}
	for _, task := range c.Tasks {
		keys = append(keys, task.Key)
	}
	if strings.Join(keys, ",") != "format,spelling,summary,part,other" {
		t.Fatalf("tasks of a lone tab (no source to compare with): %v", keys)
	}
	if c := ask(long, toolCtx(), "s-clar-1", "tóm tắt", false); c == nil || c.Suggested.Task != ScopeClarifyTaskSummary {
		t.Fatalf("tóm tắt asks with the summary preselected: %+v", c)
	}

	never := map[string]string{
		"short document":    "",
		"section reference": "góp ý Điều 3",
		"section title":     "tóm tắt chế độ báo cáo",
		"code in the text":  "rà soát PA05",
		"concrete object":   "kiểm tra số liệu",
		"format task":       "kiểm tra thể thức văn bản này",
		"spelling task":     "kiểm tra chính tả",
		"whole document":    "tóm tắt toàn bộ văn bản",
		"document type":     "góp ý quy chế này",
	}
	for name, q := range never {
		src := DocumentWorkspaceSource(long)
		if name == "short document" {
			src, q = short, "góp ý giúp văn bản này"
		}
		if c := ask(src, toolCtx(), "s-clar-2", q, false); c != nil {
			t.Errorf("%s (%q): asked %+v", name, q, c.Suggested)
		}
	}
	if c := ask(long, toolCtx(), "s-clar-2", "góp ý giúp", true); c != nil {
		t.Error("config off: asked")
	}
	if c := ask(long, types.WithMentionedDocuments(toolCtx(), []string{"ws-1"}), "s-clar-2", "góp ý giúp", false); c != nil {
		t.Error("an @-mention is an anchor")
	}
	sel := &types.DocumentSelection{DocumentID: "ws-1", Text: "Khoản 1"}
	if c := ask(long, types.WithDocumentSelection(toolCtx(), sel), "s-clar-2", "góp ý giúp", false); c != nil {
		t.Error("a selection is an anchor")
	}

	// a user scope stored: no question
	SetSessionDocumentScope(toolCtx(), "s-clar-3", &types.DocumentScope{DocumentIDs: []string{"ws-1"}, SetBy: types.DocumentScopeSetByUser})
	if c := ask(long, toolCtx(), "s-clar-3", "góp ý giúp", false); c != nil {
		t.Error("a user scope is an anchor")
	}
	ClearSessionDocumentScope(toolCtx(), "s-clar-3")
	if c := ask(long, toolCtx(), "s-clar-3", "góp ý giúp", false); c == nil {
		t.Error("once the scope is cleared the gate asks again")
	}

	// answered before for this document and task
	MarkScopeClarificationAnswered(toolCtx(), "s-clar-4", []string{"ws-1"}, types.DocumentScopeTaskSummary)
	if c := ask(long, toolCtx(), "s-clar-4", "tóm tắt", false); c != nil {
		t.Error("answered for ws-1 + summary: asked again")
	}
	if c := ask(long, toolCtx(), "s-clar-4", "góp ý giúp", false); c != nil {
		t.Error("a request without a task counts any answer for the document")
	}
	MarkScopeClarificationAnswered(toolCtx(), "s-clar-5", []string{"ws-1"}, types.DocumentScopeTaskCompare)
	if c := ask(long, toolCtx(), "s-clar-5", "tóm tắt", false); c == nil {
		t.Error("an answer for another task does not count for summary")
	}
	if c := ask(long, toolCtx(), "s-clar-6", "tóm tắt", false); c == nil {
		t.Error("answers are per session")
	}
}

func TestScopeClarificationAnsweredRoundTrip(t *testing.T) {
	if scopeClarificationAnswered(toolCtx(), "s-ans-1", "ws-9", "") {
		t.Fatal("nothing recorded yet")
	}
	MarkScopeClarificationAnswered(toolCtx(), "s-ans-1", []string{"ws-9", "ws-8"}, types.DocumentScopeTaskLookup)
	MarkScopeClarificationAnswered(toolCtx(), "s-ans-1", []string{"ws-9"}, types.DocumentScopeTaskLookup)
	MarkScopeClarificationAnswered(toolCtx(), "s-ans-1", []string{"ws-9"}, types.DocumentScopeTaskSummary)
	got := loadClarified(toolCtx(), "s-ans-1")
	if strings.Join(got["ws-9"], ",") != "lookup,summary" || strings.Join(got["ws-8"], ",") != "lookup" {
		t.Fatalf("stored pairs: %v", got)
	}
	if !scopeClarificationAnswered(toolCtx(), "s-ans-1", "ws-8", types.DocumentScopeTaskLookup) ||
		scopeClarificationAnswered(toolCtx(), "s-ans-1", "ws-8", types.DocumentScopeTaskSummary) ||
		!scopeClarificationAnswered(toolCtx(), "s-ans-1", "ws-8", "") {
		t.Fatal("answered lookup")
	}
	// clearing the scope keeps the answers
	ClearSessionDocumentScope(toolCtx(), "s-ans-1")
	if !scopeClarificationAnswered(toolCtx(), "s-ans-1", "ws-9", types.DocumentScopeTaskSummary) {
		t.Fatal("the answers outlive the scope")
	}
}

func TestScopeClarificationPayloadAndMessage(t *testing.T) {
	freshDocProfiles(t)
	ws := newFakeWorkspace(clarifyDocx(t, 200))
	ws.addDocument("ws-2", "bao-cao.docx", clarifyDocx(t, 20))
	c := DocumentScopeClarification(toolCtx(), ws, 7, "s-pay-1", ScopeClarificationInput{Query: "xem giúp"})
	if c == nil {
		t.Fatal("asked")
	}
	if len(c.Documents) != 2 || !c.Documents[0].Long || c.Documents[1].Long {
		t.Fatalf("both documents, the first long: %+v", c.Documents)
	}
	if c.Suggested.DocumentID != "ws-1" {
		t.Fatalf("the tab being viewed: %+v", c.Suggested)
	}
	data := c.Data()
	if data["display_type"] != DocumentScopeClarificationType || data["query"] != "xem giúp" {
		t.Fatalf("data: %v", data)
	}
	docs, _ := data["documents"].([]interface{})
	first, _ := docs[0].(map[string]interface{})
	if len(docs) != 2 || first["handle"] != "vb1" || first["file_name"] != "cong-van.docx" || first["role"] != "target" {
		t.Fatalf("documents in data: %v", docs)
	}
	if secs, _ := first["sections"].([]interface{}); len(secs) == 0 {
		t.Fatalf("sections in data: %v", first)
	}
	if tasks, _ := data["tasks"].([]interface{}); len(tasks) != 6 {
		t.Fatalf("with two documents compare is offered: %v", tasks)
	}
	msg := c.Message()
	for _, want := range []string{"vb1 · cong-van.docx", "đoạn", "trang", "tóm tắt", "thẻ bên dưới"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q: %s", want, msg)
		}
	}
	if strings.Contains(msg, "bao-cao.docx") {
		t.Errorf("the short document is not described as long: %s", msg)
	}
}

// The anchors on their own (a generic request rarely holds one, since its
// words then are not all filler).
func TestAnchoredRequest(t *testing.T) {
	freshDocProfiles(t)
	ws := newFakeWorkspace(clarifyDocx(t, 200))
	du, err := loadDocumentUnits(toolCtx(), ws, "s-anchor", &ws.ws, 0)
	if err != nil {
		t.Fatal(err)
	}
	loaded := []*sessionDocUnits{du}
	for q, want := range map[string]bool{
		"góp ý Điều 3":                 true,
		"xem Chương II":                true,
		"phụ lục 1":                    true,
		"trách nhiệm của sở tài chính": true, // a section title
		"PA05":                         true, // a code found in the text
		"XYZ99":                        false,
		"vb1":                          false, // a handle names no part
		"quy chế":                      true,  // the document's type
		"góp ý giúp văn bản này":       false,
	} {
		if got := anchoredRequest(q, loaded); got != want {
			t.Errorf("anchoredRequest(%q) = %v, want %v", q, got, want)
		}
	}
}
