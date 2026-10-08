package tools

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/types"
)

func longOutput(head string) string {
	return head + "\n" + strings.Repeat("dòng chi tiết của kết quả công cụ, rất dài. ", 120)
}

func TestHistoryToolOutputRules(t *testing.T) {
	ws := &types.DocumentWorkspace{ID: "w-2", Position: 2, FileName: "To-trinh.docx"}
	ops := func(n int, extra map[string]interface{}) map[string]interface{} {
		d := opsData(make([]DocumentOp, n), 3, ws)
		d["file_name"] = ws.FileName
		for k, v := range extra {
			d[k] = v
		}
		return d
	}
	cases := []struct {
		name   string
		tool   string
		args   map[string]interface{}
		data   map[string]interface{}
		output string
		want   []string
		absent []string
	}{
		{
			name: "format check from Data", tool: ToolCheckDocumentFormat,
			data: map[string]interface{}{
				"file_name": "To-trinh.docx", "evaluated": true,
				"summary":       &docformat.Summary{Pass: 20, Fail: 3, Warn: 2},
				"document_type": &docformat.DocumentTypeInfo{Used: "to_trinh"},
			},
			output: longOutput("## Đánh giá thể thức Tờ trình"),
			want: []string{"Đã kiểm tra thể thức To-trinh.docx (loại to_trinh): 3 phát hiện không đạt, 2 cảnh báo, 20 đạt.",
				"## Đánh giá thể thức Tờ trình", "gọi lại check_document_format"},
			absent: []string{"dòng chi tiết"},
		},
		{
			name: "format check from Output", tool: ToolCheckDocumentFormat,
			data:   map[string]interface{}{"file_name": "a.docx"},
			output: longOutput("## Báo cáo"),
			want:   []string{"## Báo cáo", "dòng chi tiết", "…"},
		},
		{
			name: "outline of a target", tool: ToolReadDocumentOutline,
			args:   map[string]interface{}{"document": "vb1"},
			data:   map[string]interface{}{"file_name": "Bao-cao.docx", "paragraph_count": 240, "from": 40, "to": 120},
			output: longOutput("# Dàn ý tài liệu Bao-cao.docx"),
			want:   []string{"Đã đọc đoạn 40–119 (80 đoạn) của vb1 · Bao-cao.docx; tài liệu có 240 đoạn"},
		},
		{
			name: "outline of a source", tool: ToolReadDocumentOutline,
			args: map[string]interface{}{"document": "VB3"},
			data: map[string]interface{}{"file_name": "QD.pdf", "role": "source", "part_count": 50, "unit": "chunk",
				"from": 0, "to": 12},
			output: longOutput("# Nội dung tài liệu nguồn vb3 · QD.pdf"),
			want:   []string{"Đã đọc chunk 0–11 (12 chunk) của tài liệu nguồn vb3 · QD.pdf; tài liệu có 50 chunk"},
		},
		{
			name: "outline from Output", tool: ToolReadDocumentOutline,
			output: longOutput("# Dàn ý"),
			want:   []string{"# Dàn ý\ndòng chi tiết", "…"},
		},
		{
			name: "spelling from Data", tool: ToolCheckSpelling,
			data: ops(6, map[string]interface{}{"found": 7, "marked": true, "findings": []SpellingFinding{
				{Paragraph: 1, Wrong: "sử lý", Correct: "xử lý"}, {Paragraph: 2, Wrong: "a"}, {Paragraph: 3, Wrong: "b"},
				{Paragraph: 4, Wrong: "c"}, {Paragraph: 5, Wrong: "d"}, {Paragraph: 6, Wrong: "e"}, {Paragraph: 7, Wrong: "f"},
			}}),
			output: longOutput("Phát hiện 7 lỗi chính tả"),
			want: []string{"Đã kiểm tra chính tả vb2 · To-trinh.docx: 7 lỗi chính tả, đã đánh dấu 6",
				"- Đoạn [1]: “sử lý” → “xử lý”", "- Đoạn [5]", "… và 2 lỗi khác"},
			absent: []string{"Đoạn [6]"},
		},
		{
			name: "spelling from Output", tool: ToolCheckSpelling,
			output: longOutput("Phát hiện 3 lỗi chính tả"),
			want:   []string{"Phát hiện 3 lỗi chính tả", "…"},
		},
		{
			name: "format fixes from Data", tool: ToolApplyFormatFixes,
			data: ops(2, map[string]interface{}{"applied": []AppliedFormatFix{
				{CheckID: "noi_dung.font", Change: "Times New Roman"}, {CheckID: "page.margin.left", Change: "30 mm"},
			}, "manual": []ManualFormatFix{{}}, "dry_run": false, "blocked": false}),
			output: longOutput("Đã sửa thể thức"),
			want: []string{"Đã áp dụng 2 thay đổi trên vb2 · To-trinh.docx", "- noi_dung.font: Times New Roman",
				"1 mục cần sửa tay"},
		},
		{
			name: "format fixes dry run", tool: ToolApplyFormatFixes,
			data:   ops(0, map[string]interface{}{"applied": []AppliedFormatFix{{CheckID: "x", Change: "y"}}, "dry_run": true}),
			output: longOutput("Kế hoạch"),
			want:   []string{"Kế hoạch (chưa áp dụng, chạy thử): 1 thay đổi trên vb2"},
		},
		{
			name: "rewrite from Data", tool: ToolRewriteParagraphs,
			data: ops(1, map[string]interface{}{"planned": 1, "failed": 1, "changes": []rewriteChange{
				{Paragraph: 4, Old: "2025", New: "2026", Status: "planned"}, {Paragraph: 9, Status: "failed", Error: "x"},
			}}),
			output: longOutput("Sẽ sửa 1 chỗ"),
			want:   []string{"Đã áp dụng 1 thay đổi trên vb2 · To-trinh.docx (1 chỗ không thực hiện được)", "- Đoạn [4]: “2025” → “2026”"},
			absent: []string{"Đoạn [9]"},
		},
		{
			name: "insert from Data", tool: ToolInsertParagraphs,
			data: ops(2, map[string]interface{}{"planned": 2, "failed": 0, "changes": []insertChange{
				{After: -1, Text: "Kính gửi", Status: "planned"}, {After: 7, Text: "Điều 2\nĐiều 3", Status: "planned"},
			}}),
			output: longOutput("Sẽ chèn 2 mục"),
			want:   []string{"Đã áp dụng 2 thay đổi trên vb2", "- Chèn ở đầu văn bản: “Kính gửi”", "- Chèn sau đoạn [7]: “Điều 2 ↵ Điều 3”"},
		},
		{
			name: "marks from Data", tool: ToolMarkPassages,
			data: ops(1, map[string]interface{}{"planned": 1, "failed": 0, "marks": []markResult{
				{Paragraph: 3, Text: "430 tỷ", Reason: "khác số liệu nguồn", Status: "planned"},
			}}),
			output: longOutput("Sẽ đánh dấu 1 chỗ"),
			want:   []string{"Đã áp dụng 1 thay đổi trên vb2", "- Đánh dấu đoạn [3] “430 tỷ”: khác số liệu nguồn"},
		},
		{
			name: "other tools unchanged", tool: ToolThinking,
			output: longOutput("raw"),
			want:   []string{"raw\ndòng chi tiết"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := &types.ToolResult{Success: true, Output: c.output, Data: c.data}
			got := HistoryToolOutput(c.tool, c.args, res)
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Fatalf("missing %q in\n%s", w, got)
				}
			}
			for _, a := range c.absent {
				if strings.Contains(got, a) {
					t.Fatalf("unexpected %q in\n%s", a, got)
				}
			}
			if _, compacted := documentToolHistoryRules[c.tool]; compacted && utf8.RuneCountInString(got) > historyCompactMaxRunes+1 {
				t.Fatalf("summary is %d runes", utf8.RuneCountInString(got))
			}
			// the stored and streamed forms are not the summary
			steps := SanitizeAgentStepsForStorage([]types.AgentStep{{ToolCalls: []types.ToolCall{{Name: c.tool, Result: res}}}})
			if stored := steps[0].ToolCalls[0].Result.Output; stored != c.output {
				t.Fatalf("stored output changed:\n%s", stored)
			}
			if stream := StreamContentForToolResult(c.tool, true, "", c.data); stream != "" {
				t.Fatalf("stream content changed: %q", stream)
			}
		})
	}
}

func TestHistoryToolOutputKeepsFailuresAndShortOutputs(t *testing.T) {
	failed := &types.ToolResult{Success: false, Error: "không đọc được tài liệu", Data: map[string]interface{}{"found": 0}}
	if got := HistoryToolOutput(ToolCheckSpelling, nil, failed); got != "Error: không đọc được tài liệu" {
		t.Fatalf("failure: %q", got)
	}
	short := &types.ToolResult{Success: true, Output: "OK", Data: map[string]interface{}{"found": 0, "file_name": "a.docx"}}
	if got := HistoryToolOutput(ToolCheckSpelling, nil, short); got != "OK" {
		t.Fatalf("short output: %q", got)
	}
}
