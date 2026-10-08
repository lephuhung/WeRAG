package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestFindInDocumentsSearchesTargetsAndSources(t *testing.T) {
	freshDocProfiles(t)
	ws := newSourceWorkspace(t)
	tool := NewFindInDocumentsTool(ws, "s-1")

	res := runTool(t, tool, `{"query":"chi đầu tư phát triển 430 tỷ"}`)
	if !res.Success {
		t.Fatalf("search failed: %+v", res)
	}
	for _, want := range []string{
		"## vb2 · bao-cao.pdf (tài liệu nguồn, đơn vị: chunk) — 1 kết quả",
		"[1] (Chương II) Điều 5. Chi đầu tư phát triển 430 tỷ đồng.",
		"↑ [0] Điều 1. Năm 2025",
		"read_document_outline document=<handle> from=<chỉ số>",
	} {
		if !strings.Contains(res.Output, want) {
			t.Fatalf("output lacks %q:\n%s", want, res.Output)
		}
	}
	docs := res.Data["documents"].([]FindDocumentHits)
	if res.Data["display_type"] != "document_search" || len(docs) != 1 || docs[0].Handle != "vb2" ||
		docs[0].Hits[0].Index != 1 || *docs[0].Hits[0].PrevIndex != 0 || docs[0].Hits[0].NextIndex != nil {
		t.Fatalf("data: %+v", res.Data)
	}

	// a target's paragraphs, unaccented query, with the NĐ30 label
	res = runTool(t, tool, `{"query":"bao cao ket qua truoc ngay 30 thang 11","document":"vb1"}`)
	if !res.Success || !strings.Contains(res.Output, "## vb1 · cong-van.docx (văn bản làm việc, đơn vị: đoạn)") ||
		!strings.Contains(res.Output, "] (noi_dung") || !strings.Contains(res.Output, "Đề nghị các đơn vị báo cáo kết quả trước ngày 30 tháng 11") {
		t.Fatalf("target search:\n%s", res.Output)
	}
	if strings.Contains(res.Output, "bao-cao.pdf") {
		t.Fatal("document=vb1 searches vb1 only")
	}

	res = runTool(t, tool, `{"query":"không hề xuất hiện quyxyz"}`)
	if !res.Success || !strings.Contains(res.Output, "Không có đoạn nào khớp") || res.Data["hit_count"] != 0 {
		t.Fatalf("no hit: %+v", res)
	}
	if res := runTool(t, tool, `{"query":"  "}`); res.Success {
		t.Fatal("an empty query is refused")
	}
}

func TestFindInDocumentsSectionFilterAndCaps(t *testing.T) {
	freshDocProfiles(t)
	var body strings.Builder
	body.WriteString(testPara("QUY CHẾ", "center", "Times New Roman", 14, true, false))
	for a := 1; a <= 6; a++ {
		body.WriteString(testPara(fmt.Sprintf("Điều %d. Trách nhiệm của đơn vị số %d", a, a), "left", "Times New Roman", 14, true, false))
		for p := 0; p < 8; p++ {
			body.WriteString(testPara(strings.Repeat(fmt.Sprintf("Ngân sách cấp cho đơn vị %d năm 2026 được sử dụng đúng mục đích. ", a), 6), "left", "Times New Roman", 14, false, false))
		}
	}
	ws := newFakeWorkspace(buildTestDocx(t, body.String(), [4]int{20, 15, 30, 20}))
	tool := NewFindInDocumentsTool(ws, "s-1")

	res := runTool(t, tool, `{"query":"ngân sách năm 2026","limit":20}`)
	if !res.Success || utf8.RuneCountInString(res.Output) > findOutputRunes+400 {
		t.Fatalf("output over the cap (%d runes):\n%s", utf8.RuneCountInString(res.Output), res.Output)
	}
	if shown := res.Data["shown"].(int); shown >= res.Data["hit_count"].(int) || shown == 0 {
		t.Fatalf("a long result list is cut: shown %v of %v", res.Data["shown"], res.Data["hit_count"])
	}
	if !strings.Contains(res.Output, "— 48 kết quả, hiện ") {
		t.Fatalf("the total is reported:\n%s", res.Output[:300])
	}

	res = runTool(t, tool, `{"query":"ngân sách","section":"Điều 3"}`)
	if !res.Success || !strings.Contains(res.Output, "— 8 kết quả") || strings.Contains(res.Output, "đơn vị 4 năm") ||
		!strings.Contains(res.Output, "Điều 3. Trách nhiệm") {
		t.Fatalf("section filter:\n%s", res.Output)
	}
	res = runTool(t, tool, `{"query":"ngân sách","section":"Điều 9"}`)
	if !res.Success || !strings.Contains(res.Output, `không có mục "Điều 9"`) {
		t.Fatalf("unknown section:\n%s", res.Output)
	}
}

func TestFindInDocumentsHistoryKeepsOneLine(t *testing.T) {
	freshDocProfiles(t)
	ws := newSourceWorkspace(t)
	res := runTool(t, NewFindInDocumentsTool(ws, "s-1"), `{"query":"430 tỷ"}`)
	// as stored: Data went through JSON
	raw, _ := json.Marshal(res.Data)
	var stored map[string]interface{}
	_ = json.Unmarshal(raw, &stored)
	got := CompactToolOutputForHistory(ToolFindInDocuments, &types.ToolResult{Success: true, Output: res.Output, Data: stored})
	if got != "đã tìm '430 tỷ' trong vb1, vb2, vb3: 1 kết quả (nội dung không lưu trong lịch sử; tìm lại khi cần)" {
		t.Fatalf("history line: %q", got)
	}
	steps := SanitizeAgentStepsForStorage([]types.AgentStep{{ToolCalls: []types.ToolCall{{Name: ToolFindInDocuments, Result: res}}}})
	if out := steps[0].ToolCalls[0].Result.Output; strings.Contains(out, "Chi đầu tư") {
		t.Fatalf("stored step keeps the passages: %q", out)
	}
}
