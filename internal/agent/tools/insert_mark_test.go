package tools

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/docformat"
)

func paraTexts(t *testing.T, content []byte) []string {
	t.Helper()
	l := docformat.InspectDocx(content)
	if len(l.Errors) > 0 && len(l.Paragraphs) == 0 {
		t.Fatalf("InspectDocx: %v", l.Errors)
	}
	out := make([]string, len(l.Paragraphs))
	for i, p := range l.Paragraphs {
		out[i] = strings.TrimSpace(p.Text)
	}
	return out
}

// ---------------------------------------------------------------------------
// insert_paragraphs

func TestInsertParagraphsAfterIndexAtStartAndByMatch(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	before := paraTexts(t, ws.content)
	res := runTool(t, NewInsertParagraphsTool(ws, "s"), `{"inserts":[
		{"after":10,"text":"Đề nghị các đơn vị nghiêm túc thực hiện.","like":9},
		{"at_start":true,"text":"KHẨN","bold":true,"alignment":"left"},
		{"after_match":"Kính gửi","text":"Căn cứ Kế hoạch số 01/KH-UBND ngày 02/01/2026.","italic":true}
	],"note":"bổ sung"}`)
	if !res.Success || ws.commits != 1 || res.Data["document_revision"] != 4 || res.Data["applied"] != 3 || res.Data["failed"] != 0 {
		t.Fatalf("result: %+v", res)
	}
	got := paraTexts(t, ws.content)
	if len(got) != len(before)+3 {
		t.Fatalf("%d paragraphs, want %d", len(got), len(before)+3)
	}
	// at_start → 0; after original 7 (Kính gửi) → shifted by the start insert
	if got[0] != "KHẨN" || got[8] != before[7] || got[9] != "Căn cứ Kế hoạch số 01/KH-UBND ngày 02/01/2026." {
		t.Fatalf("paragraphs: %q", got[:11])
	}
	if got[12] != before[10] || got[13] != "Đề nghị các đơn vị nghiêm túc thực hiện." || got[14] != before[11] {
		t.Fatalf("paragraphs: %q", got[10:15])
	}
	for _, want := range []string{"Đã chèn sau đoạn [10]: “Đề nghị", "Đã chèn ở đầu văn bản: “KHẨN”", "Đã chèn sau đoạn [7]:", "(track changes)", "bổ sung"} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("output lacks %q:\n%s", want, res.Output)
		}
	}
	xml := documentPartXML(t, ws.content)
	if !strings.Contains(xml, `w:author="Trợ lý AI (WeRAG)"`) || !strings.Contains(xml, "<w:ins ") {
		t.Fatalf("inserts must be tracked:\n%s", xml)
	}
	changes, ok := res.Data["changes"].([]insertChange)
	if !ok || len(changes) != 3 || changes[0].After != 10 || changes[1].After != -1 || changes[2].After != 7 ||
		changes[0].Status != "applied" {
		t.Fatalf("changes: %+v", res.Data["changes"])
	}
}

func TestInsertParagraphsMultiLineKeepsOrder(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	before := paraTexts(t, ws.content)
	res := runTool(t, NewInsertParagraphsTool(ws, "s"), `{"inserts":[
		{"after":12,"text":"Nơi nhận:\n- Như trên;\n- Lưu: VT."},
		{"after":12,"text":"(Đã ký)"},
		{"after":3,"text":"Dòng mới sau tiêu ngữ"}
	]}`)
	if !res.Success || res.Data["applied"] != 3 {
		t.Fatalf("result: %+v", res)
	}
	got := paraTexts(t, ws.content)
	want := append(append([]string{}, before[:4]...), "Dòng mới sau tiêu ngữ")
	want = append(want, before[4:13]...)
	want = append(want, "Nơi nhận:", "- Như trên;", "- Lưu: VT.", "(Đã ký)")
	want = append(want, before[13:]...)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("paragraphs:\n got %q\nwant %q", got, want)
	}
}

func TestInsertParagraphsAmbiguousAnchorFails(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	res := runTool(t, NewInsertParagraphsTool(ws, "s"), `{"inserts":[{"after_match":"các đơn vị","text":"x"}]}`)
	if res.Success || ws.commits != 0 || !strings.Contains(res.Error, "khớp nhiều đoạn") || res.Data["failed"] != 1 {
		t.Fatalf("an ambiguous anchor must not be guessed: %+v", res)
	}
	// one bad anchor does not stop the others
	res = runTool(t, NewInsertParagraphsTool(ws, "s"), `{"inserts":[{"after_match":"các đơn vị","text":"x"},{"after":0,"text":"y"}]}`)
	if !res.Success || ws.commits != 1 || res.Data["applied"] != 1 || res.Data["failed"] != 1 || !strings.Contains(res.Output, "KHÔNG chèn") {
		t.Fatalf("partial: %+v", res)
	}
}

func TestInsertParagraphsValidatesInput(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	many := strings.TrimSuffix(strings.Repeat(`{"after":0,"text":"x"},`, maxInserts+1), ",")
	for name, args := range map[string]string{
		"too many":      `{"inserts":[` + many + `]}`,
		"empty text":    `{"inserts":[{"after":0,"text":" \n "}]}`,
		"bad alignment": `{"inserts":[{"after":0,"text":"x","alignment":"justify-all"}]}`,
		"no anchor":     `{"inserts":[{"text":"x"}]}`,
		"out of range":  `{"inserts":[{"after":99,"text":"x"}]}`,
	} {
		if res := runTool(t, NewInsertParagraphsTool(ws, "s"), args); res.Success {
			t.Errorf("%s accepted: %+v", name, res)
		}
	}
	if ws.commits != 0 {
		t.Fatalf("invalid input committed %d times", ws.commits)
	}
}

// ---------------------------------------------------------------------------
// mark_passages

var runRe = regexp.MustCompile(`(?s)<w:r[ >].*?</w:r>`)

// markedRun reports whether one run carries all of attrs and a tracked
// formatting change.
func markedRun(xml string, attrs ...string) bool {
	for _, r := range runRe.FindAllString(xml, -1) {
		ok := strings.Contains(r, "<w:rPrChange")
		for _, a := range attrs {
			ok = ok && strings.Contains(r, a)
		}
		if ok {
			return true
		}
	}
	return false
}

func TestMarkPassagesStyles(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	before := paraTexts(t, ws.content)
	res := runTool(t, NewMarkPassagesTool(ws, "s"), `{"marks":[
		{"paragraph":8,"text":"cải cách hành chính","reason":"Cần ghi rõ chương trình cải cách hành chính nào"},
		{"match":"báo cáo kết quả","style":"highlight","reason":"Thời hạn báo cáo chưa khớp kế hoạch"},
		{"text":"Trên đây là","style":"color","reason":"Câu kết nên theo mẫu"},
		{"paragraph":8,"text":"không có trong đoạn","reason":"x"}
	]}`)
	if !res.Success || ws.commits != 1 || res.Data["document_revision"] != 4 || res.Data["applied"] != 3 || res.Data["failed"] != 1 {
		t.Fatalf("result: %+v", res)
	}
	xml := documentPartXML(t, ws.content)
	if !markedRun(xml, `w:val="wave"`, `w:color="FF0000"`) {
		t.Errorf("no tracked red wavy underline:\n%s", xml)
	}
	if !markedRun(xml, `<w:highlight w:val="yellow"/>`) {
		t.Errorf("no tracked highlight:\n%s", xml)
	}
	if !markedRun(xml, `<w:color w:val="FF0000"/>`) {
		t.Errorf("no tracked red text:\n%s", xml)
	}
	if got := paraTexts(t, ws.content); strings.Join(got, "|") != strings.Join(before, "|") {
		t.Fatalf("marking changed the text:\n%q\n%q", got, before)
	}
	marks, ok := res.Data["marks"].([]markResult)
	if !ok || len(marks) != 4 || marks[0].Paragraph != 8 || marks[0].Style != "underline" ||
		marks[1].Paragraph != 9 || marks[1].Style != "highlight" || marks[1].Text != before[9] ||
		marks[2].Paragraph != 10 || marks[3].Status != "failed" {
		t.Fatalf("marks: %+v", res.Data["marks"])
	}
	for _, want := range []string{
		"1. Đoạn [8] “cải cách hành chính”", "Cần ghi rõ chương trình",
		"2. Đoạn [9]", "3. Đoạn [10] “Trên đây là”", "KHÔNG đánh dấu", "track changes", "từ chối",
	} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("output lacks %q:\n%s", want, res.Output)
		}
	}
	if strings.Contains(xml, "Cần ghi rõ") {
		t.Fatal("a reason must never be written into the document")
	}
}

func TestMarkPassagesWholeParagraphUnderline(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	res := runTool(t, NewMarkPassagesTool(ws, "s"), `{"marks":[{"paragraph":6,"reason":"Trích yếu chưa nêu rõ nội dung"}]}`)
	if !res.Success || ws.commits != 1 {
		t.Fatalf("result: %+v", res)
	}
	if !markedRun(documentPartXML(t, ws.content), `w:val="wave"`, `w:color="FF0000"`) {
		t.Fatal("whole paragraph not underlined")
	}
	if l := docformat.InspectDocx(ws.content); len(l.Paragraphs) != 16 {
		t.Fatalf("InspectDocx: %d paragraphs, %v", len(l.Paragraphs), l.Errors)
	}
}

func TestMarkPassagesValidatesInput(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	many := strings.TrimSuffix(strings.Repeat(`{"paragraph":8,"reason":"r"},`, maxMarks+1), ",")
	for name, args := range map[string]string{
		"too many":  `{"marks":[` + many + `]}`,
		"no reason": `{"marks":[{"paragraph":8,"reason":" "}]}`,
		"bad style": `{"marks":[{"paragraph":8,"style":"blink","reason":"r"}]}`,
		"ambiguous": `{"marks":[{"text":"các đơn vị","reason":"r"}]}`,
	} {
		if res := runTool(t, NewMarkPassagesTool(ws, "s"), args); res.Success {
			t.Errorf("%s accepted: %+v", name, res)
		}
	}
	if ws.commits != 0 {
		t.Fatalf("invalid input committed %d times", ws.commits)
	}
}

func TestInsertParagraphsLikeCopiesFormatting(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	before := paraTexts(t, ws.content)
	res := runTool(t, NewInsertParagraphsTool(ws, "s"), `{"inserts":[
		{"after":3,"text":"Dòng theo định dạng đoạn 10","like":10},
		{"after":8,"text":"Dòng B\nDòng C"}
	]}`)
	if !res.Success || res.Data["applied"] != 2 {
		t.Fatalf("result: %+v", res)
	}
	l := docformat.InspectDocx(ws.content)
	p := l.Paragraphs
	// 0..3, new(4), old 4..8 → 5..9, B(10), C(11), old 9.. → 12..
	if strings.TrimSpace(p[4].Text) != "Dòng theo định dạng đoạn 10" || strings.TrimSpace(p[10].Text) != "Dòng B" ||
		strings.TrimSpace(p[11].Text) != "Dòng C" || strings.TrimSpace(p[14].Text) != before[11] {
		t.Fatalf("paragraphs: %q", paraTexts(t, ws.content))
	}
	src := p[13] // the original paragraph 10, shifted by the three inserts before it
	if strings.TrimSpace(src.Text) != before[10] {
		t.Fatalf("paragraph 13 = %q", src.Text)
	}
	got := p[4]
	if got.FontName == nil || *got.FontName != "Arial" || got.SizePt == nil || *got.SizePt != 12 ||
		got.Alignment != src.Alignment || (got.Bold != nil && *got.Bold) {
		t.Fatalf("like=10 not honoured: font=%v size=%v align=%q bold=%v (tiêu ngữ anchor is TNR 14 bold centred)",
			got.FontName, got.SizePt, got.Alignment, got.Bold)
	}
	// without like a line copies its anchor (paragraph 8: Arial 12, left)
	if b := p[10]; b.FontName == nil || *b.FontName != "Arial" || b.Alignment != "left" {
		t.Fatalf("anchor formatting not copied: font=%v align=%q", b.FontName, b.Alignment)
	}
}

func TestInsertParagraphsFailedInsertKeepsRequestedAnchor(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	res := runTool(t, NewInsertParagraphsTool(ws, "s"), `{"inserts":[{"after":99,"text":"x"},{"after":0,"text":"y"}]}`)
	changes, _ := res.Data["changes"].([]insertChange)
	if !res.Success || len(changes) != 2 || changes[0].After != 99 || changes[0].Status != "failed" || changes[1].After != 0 {
		t.Fatalf("changes: %+v", res.Data["changes"])
	}
}

func TestMarkPassagesAlreadyMarkedIsANoOp(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	args := `{"marks":[{"paragraph":8,"text":"cải cách hành chính","reason":"r"}]}`
	if res := runTool(t, NewMarkPassagesTool(ws, "s"), args); !res.Success || ws.commits != 1 {
		t.Fatalf("first mark: %+v", res)
	}
	res := runTool(t, NewMarkPassagesTool(ws, "s"), args)
	if !res.Success || ws.commits != 1 || res.Data["document_revision"] != 4 ||
		res.Data["applied"] != 0 || res.Data["unchanged"] != 1 {
		t.Fatalf("a repeated mark must not write: %+v", res)
	}
	marks, _ := res.Data["marks"].([]markResult)
	if len(marks) != 1 || marks[0].Status != "unchanged" || !strings.Contains(res.Output, "đã được đánh dấu trước đó") {
		t.Fatalf("result: %+v\n%s", res.Data, res.Output)
	}
}
