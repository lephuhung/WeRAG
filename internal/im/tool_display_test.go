package im

import (
	"strings"
	"testing"
)

func TestFormatIMToolLine_pendingWithQuery(t *testing.T) {
	line := FormatIMToolLine(IMToolStep{
		ToolName:  "knowledge_search",
		Pending:   true,
		Arguments: map[string]any{"query": "Nghị định 30"},
	})
	if line != "Đang gọi Tra cứu kho tri thức..." {
		t.Fatalf("pending line = %q", line)
	}
}

func TestFormatIMToolLine_searchDoneWithQueryAndSummary(t *testing.T) {
	line := FormatIMToolLine(IMToolStep{
		ToolName: "knowledge_search",
		Success:  true,
		Arguments: map[string]any{
			"query": "Nghị định 30",
		},
		Data: map[string]interface{}{
			"results":   []interface{}{map[string]interface{}{}, map[string]interface{}{}, map[string]interface{}{}},
			"kb_counts": map[string]interface{}{"a": 1, "b": 1},
		},
	})
	if !strings.Contains(line, "Tra cứu kho tri thức: “Nghị định 30”") {
		t.Fatalf("title missing query: %q", line)
	}
	if !strings.Contains(line, "Tìm thấy 3 kết quả từ 2 tệp") {
		t.Fatalf("summary missing: %q", line)
	}
}

func TestFormatIMToolLine_grepPatterns(t *testing.T) {
	line := FormatIMToolLine(IMToolStep{
		ToolName: "grep_chunks",
		Success:  true,
		Arguments: map[string]any{
			"patterns": []any{"công văn", "tờ trình"},
		},
		Data: map[string]interface{}{
			"total_matches":  float64(5),
			"document_count": float64(2),
		},
	})
	if line != "Tìm từ khoá: “công văn, tờ trình” · Tìm thấy 5 đoạn phù hợp từ 2 tài liệu" {
		t.Fatalf("grep line = %q", line)
	}
}

func TestFormatIMRagPipelineLine_queryUnderstand(t *testing.T) {
	pending := FormatIMRagPipelineLine(IMToolStep{
		ToolName: "query_understand",
		Pending:  true,
	})
	if pending != "Đang hiểu câu hỏi..." {
		t.Fatalf("pending = %q", pending)
	}
	done := FormatIMRagPipelineLine(IMToolStep{
		ToolName: "query_understand",
		Success:  true,
	})
	if done != "Đã hiểu câu hỏi" {
		t.Fatalf("done = %q", done)
	}
}

func TestFormatIMRagPipelineLine_searchWithQuery(t *testing.T) {
	line := FormatIMRagPipelineLine(IMToolStep{
		ToolName:  "knowledge_search",
		Pending:   true,
		Arguments: map[string]any{"query": "Cổng dịch vụ công tỉnh"},
	})
	if line != "Đang tra cứu kho tri thức: “Cổng dịch vụ công tỉnh”" {
		t.Fatalf("line = %q", line)
	}
}

func TestFormatIMRagPipelineLine_webSearchWithQuery(t *testing.T) {
	line := FormatIMRagPipelineLine(IMToolStep{
		ToolName:  "knowledge_search",
		Pending:   true,
		Arguments: map[string]any{"query": "lịch tiếp công dân của UBND tỉnh", "search_source": "web"},
	})
	if line != "Đang tìm trên web: “lịch tiếp công dân của UBND tỉnh”" {
		t.Fatalf("line = %q", line)
	}
}

func TestIMGetQueryText_joinsUniqueQueries(t *testing.T) {
	got := imGetQueryText(map[string]any{
		"query":   "foo",
		"queries": []any{"foo", "bar"},
	})
	if got != "foo，bar" {
		t.Fatalf("query text = %q", got)
	}
}

func TestFormatIMToolLine_writeSandboxPendingShowsDiffStat(t *testing.T) {
	line := FormatIMToolLine(IMToolStep{
		ToolName: "write_sandbox_file",
		Pending:  true,
		Arguments: map[string]any{
			"path":          "/workspace/output/a.py",
			"added_lines":   12,
			"removed_lines": 0,
		},
	})
	if line != "Ghi tệp sandbox: “/workspace/output/a.py”... +12" {
		t.Fatalf("pending write line = %q", line)
	}
}
