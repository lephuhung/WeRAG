package tools

import (
	"bytes"
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
	original := append([]byte(nil), ws.content...)
	res := runTool(t, NewInsertParagraphsTool(ws, "s"), `{"inserts":[
		{"after":10,"text":"Đề nghị các đơn vị nghiêm túc thực hiện.","like":9},
		{"at_start":true,"text":"KHẨN","bold":true,"alignment":"left"},
		{"after_match":"Kính gửi","text":"Căn cứ Kế hoạch số 01/KH-UBND ngày 02/01/2026.","italic":true}
	],"note":"bổ sung"}`)
	if !res.Success || res.Data["planned"] != 3 || res.Data["failed"] != 0 || len(ws.snapshots) != 1 || res.Data["snapshot_seq"] != 11 {
		t.Fatalf("result: %+v", res)
	}
	if !bytes.Equal(original, ws.content) {
		t.Fatal("the tool must not write the document")
	}
	ops := resultOps(t, res)
	if len(ops) != 3 {
		t.Fatalf("ops: %+v", ops)
	}
	if o := ops[0]; o.Op != OpInsertAfter || o.Anchor.Text != before[10] || o.Anchor.Occurrence != 1 ||
		o.Like == nil || o.Like.Text != before[9] || o.Text != "Đề nghị các đơn vị nghiêm túc thực hiện." {
		t.Fatalf("op 0: %+v", o)
	}
	if o := ops[1]; !o.Anchor.AtStart || o.Text != "KHẨN" || o.Bold == nil || !*o.Bold || o.Alignment != "left" {
		t.Fatalf("op 1: %+v", o)
	}
	if o := ops[2]; o.Anchor.Text != before[7] || o.Italic == nil || !*o.Italic {
		t.Fatalf("op 2: %+v", o)
	}
	for _, want := range []string{"Chèn sau đoạn [10]: “Đề nghị", "Chèn ở đầu văn bản: “KHẨN”", "Chèn sau đoạn [7]:", "bổ sung", "Ctrl+Z"} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("output lacks %q:\n%s", want, res.Output)
		}
	}
	changes, ok := res.Data["changes"].([]insertChange)
	if !ok || len(changes) != 3 || changes[0].After != 10 || changes[1].After != -1 || changes[2].After != 7 ||
		changes[0].Status != "planned" {
		t.Fatalf("changes: %+v", res.Data["changes"])
	}

	applyPlan(t, ws, res)
	got := paraTexts(t, ws.content)
	if len(got) != len(before)+3 || got[0] != "KHẨN" || got[8] != before[7] ||
		got[9] != "Căn cứ Kế hoạch số 01/KH-UBND ngày 02/01/2026." ||
		got[12] != before[10] || got[13] != "Đề nghị các đơn vị nghiêm túc thực hiện." || got[14] != before[11] {
		t.Fatalf("paragraphs after the plan: %q", got)
	}
}

func TestInsertParagraphsChainsLinesAndKeepsOrder(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	before := paraTexts(t, ws.content)
	res := runTool(t, NewInsertParagraphsTool(ws, "s"), `{"inserts":[
		{"after":12,"text":"Nơi nhận:\n- Như trên;\n\n- Lưu: VT."},
		{"after":12,"text":"(Đã ký)"},
		{"after":3,"text":"Dòng mới sau tiêu ngữ"}
	]}`)
	if !res.Success || res.Data["planned"] != 3 {
		t.Fatalf("result: %+v", res)
	}
	ops := resultOps(t, res)
	// the second line anchors on the first, the third on the second; the
	// repeated texts already in the document are told apart by occurrence
	if len(ops) != 5 || ops[0].Anchor.Text != before[12] || ops[1].Anchor.Text != "Nơi nhận:" || ops[1].Anchor.Occurrence != 1 ||
		ops[2].Anchor.Text != "- Như trên;" || ops[2].Anchor.Occurrence != 1 ||
		ops[3].Anchor.Text != "- Lưu: VT." || ops[3].Anchor.Occurrence != 1 || ops[3].Text != "(Đã ký)" {
		t.Fatalf("ops: %+v", ops)
	}
	applyPlan(t, ws, res)
	want := append(append([]string{}, before[:4]...), "Dòng mới sau tiêu ngữ")
	want = append(want, before[4:13]...)
	want = append(want, "Nơi nhận:", "- Như trên;", "- Lưu: VT.", "(Đã ký)")
	want = append(want, before[13:]...)
	if got := paraTexts(t, ws.content); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("paragraphs:\n got %q\nwant %q", got, want)
	}
}

func TestInsertParagraphsOccurrenceOfRepeatedAnchor(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	// paragraph 15 "- Lưu: VT." is unique, but inserting a copy before it
	// makes the second insert's anchor the 2nd occurrence
	res := runTool(t, NewInsertParagraphsTool(ws, "s"), `{"inserts":[
		{"after":13,"text":"- Lưu: VT."},
		{"after":15,"text":"Ghi chú cuối"}
	]}`)
	ops := resultOps(t, res)
	if len(ops) != 2 || ops[1].Anchor.Text != "- Lưu: VT." || ops[1].Anchor.Occurrence != 2 {
		t.Fatalf("ops: %+v", ops)
	}
	applyPlan(t, ws, res)
	got := paraTexts(t, ws.content)
	if got[len(got)-1] != "Ghi chú cuối" || got[14] != "- Lưu: VT." {
		t.Fatalf("paragraphs: %q", got)
	}
}

func TestInsertParagraphsLikeCopiesFormatting(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	before := paraTexts(t, ws.content)
	res := runTool(t, NewInsertParagraphsTool(ws, "s"), `{"inserts":[
		{"after":3,"text":"Dòng theo định dạng đoạn 10","like":10},
		{"after":8,"text":"Dòng B\nDòng C"}
	]}`)
	if !res.Success || res.Data["planned"] != 2 {
		t.Fatalf("result: %+v", res)
	}
	applyPlan(t, ws, res)
	p := docformat.InspectDocx(ws.content).Paragraphs
	// 0..3, new(4), old 4..8 → 5..9, B(10), C(11), old 9.. → 12..
	if strings.TrimSpace(p[4].Text) != "Dòng theo định dạng đoạn 10" || strings.TrimSpace(p[10].Text) != "Dòng B" ||
		strings.TrimSpace(p[11].Text) != "Dòng C" || strings.TrimSpace(p[13].Text) != before[10] {
		t.Fatalf("paragraphs: %q", paraTexts(t, ws.content))
	}
	src, got := p[13], p[4]
	if got.FontName == nil || *got.FontName != "Arial" || got.SizePt == nil || *got.SizePt != 12 ||
		got.Alignment != src.Alignment || (got.Bold != nil && *got.Bold) {
		t.Fatalf("like=10 not honoured: font=%v size=%v align=%q bold=%v", got.FontName, got.SizePt, got.Alignment, got.Bold)
	}
	if b := p[10]; b.FontName == nil || *b.FontName != "Arial" || b.Alignment != "left" {
		t.Fatalf("anchor formatting not copied: font=%v align=%q", b.FontName, b.Alignment)
	}
}

func TestInsertParagraphsAmbiguousAnchorFails(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	res := runTool(t, NewInsertParagraphsTool(ws, "s"), `{"inserts":[{"after_match":"các đơn vị","text":"x"}]}`)
	if res.Success || !strings.Contains(res.Error, "khớp nhiều đoạn") || res.Data["failed"] != 1 || len(resultOps(t, res)) != 0 {
		t.Fatalf("an ambiguous anchor must not be guessed: %+v", res)
	}
	res = runTool(t, NewInsertParagraphsTool(ws, "s"), `{"inserts":[{"after":99,"text":"x"},{"after":0,"text":"y"}]}`)
	changes, _ := res.Data["changes"].([]insertChange)
	if !res.Success || res.Data["planned"] != 1 || len(resultOps(t, res)) != 1 || !strings.Contains(res.Output, "KHÔNG chèn") ||
		len(changes) != 2 || changes[0].After != 99 || changes[0].Status != "failed" {
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
}

// ---------------------------------------------------------------------------
// mark_passages

var runRe = regexp.MustCompile(`(?s)<w:r[ >].*?</w:r>`)

// markedRun reports whether one run carries all of attrs.
func markedRun(xml string, attrs ...string) bool {
	for _, r := range runRe.FindAllString(xml, -1) {
		ok := true
		for _, a := range attrs {
			ok = ok && strings.Contains(r, a)
		}
		if ok {
			return true
		}
	}
	return false
}

func TestMarkPassagesPlansMarks(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	before := paraTexts(t, ws.content)
	original := append([]byte(nil), ws.content...)
	res := runTool(t, NewMarkPassagesTool(ws, "s"), `{"marks":[
		{"paragraph":8,"text":"cải cách hành chính","reason":"Cần ghi rõ chương trình cải cách hành chính nào"},
		{"match":"báo cáo kết quả","style":"highlight","reason":"Thời hạn báo cáo chưa khớp kế hoạch"},
		{"text":"Trên đây là","style":"color","reason":"Câu kết nên theo mẫu"},
		{"paragraph":8,"text":"không có trong đoạn","reason":"x"}
	]}`)
	if !res.Success || res.Data["planned"] != 3 || res.Data["failed"] != 1 || len(ws.snapshots) != 1 ||
		ws.snapshots[0] != "ai: đánh dấu chỗ cần xem lại" {
		t.Fatalf("result: %+v", res)
	}
	if !bytes.Equal(original, ws.content) {
		t.Fatal("the tool must not write the document")
	}
	ops := resultOps(t, res)
	if len(ops) != 3 || ops[0].Op != OpMark || ops[0].Style != "underline" || ops[0].Text != "cải cách hành chính" ||
		ops[0].Anchor.Text != before[8] || ops[1].Style != "highlight" || ops[1].Text != "" || ops[1].Anchor.Text != before[9] ||
		ops[2].Style != "color" || ops[2].Anchor.Text != before[10] {
		t.Fatalf("ops: %+v", ops)
	}
	marks, ok := res.Data["marks"].([]markResult)
	if !ok || len(marks) != 4 || marks[0].Paragraph != 8 || marks[1].Text != before[9] || marks[3].Status != "failed" {
		t.Fatalf("marks: %+v", res.Data["marks"])
	}
	for _, want := range []string{"1. Đoạn [8] “cải cách hành chính”", "Cần ghi rõ chương trình", "2. Đoạn [9]",
		"3. Đoạn [10] “Trên đây là”", "KHÔNG đánh dấu", "Ctrl+Z"} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("output lacks %q:\n%s", want, res.Output)
		}
	}
	applyPlan(t, ws, res)
	xml := documentPartXML(t, ws.content)
	if !markedRun(xml, `<w:u w:val="single"`, `w:color="FF0000"`) || !markedRun(xml, `<w:highlight w:val="yellow"/>`) ||
		!markedRun(xml, `<w:color w:val="FF0000"/>`) {
		t.Fatalf("marks not applied:\n%s", xml)
	}
	if got := paraTexts(t, ws.content); strings.Join(got, "|") != strings.Join(before, "|") {
		t.Fatal("marking changed the text")
	}
	if strings.Contains(xml, "Cần ghi rõ") {
		t.Fatal("a reason must never be written into the document")
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
}

func TestMarkPassagesFindsTheTextNearAWrongParagraph(t *testing.T) {
	// a spelling mistake the model places one paragraph off is still
	// underlined where it is: the text decides
	ws := newFakeWorkspace(testCongVan(t, nd30Margins))
	before := paraTexts(t, ws.content)
	res := runTool(t, NewMarkPassagesTool(ws, "s"), `{"marks":[
		{"paragraph":9,"text":"cải cách hành chính","reason":"→ x"},
		{"paragraph":11,"text":"không có ở đâu","reason":"→ y"}
	]}`)
	if !res.Success || res.Data["planned"] != 1 || res.Data["failed"] != 1 {
		t.Fatalf("result: %+v", res)
	}
	ops := resultOps(t, res)
	if len(ops) != 1 || ops[0].Anchor.Text != before[8] || ops[0].Text != "cải cách hành chính" || ops[0].Style != "underline" {
		t.Fatalf("ops: %+v", ops)
	}
	if marks, _ := res.Data["marks"].([]markResult); len(marks) != 2 || marks[0].Paragraph != 8 {
		t.Fatalf("marks: %+v", res.Data["marks"])
	}
	if !strings.Contains(res.Output, "1. Đoạn [8]") {
		t.Fatalf("output: %s", res.Output)
	}
}
