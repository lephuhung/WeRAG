package im

import (
	"strings"
	"testing"
)

func TestStripThinkBlocks(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "no think blocks", input: "Hello, world!", want: "Hello, world!"},
		{name: "empty", input: "", want: ""},
		{
			name:  "single block before answer",
			input: "<think>reasoning</think>The answer is 42.",
			want:  "The answer is 42.",
		},
		{
			name:  "multiline think with tools",
			input: "<think>\nĐể tôi tra cứu kho tri thức trước\nĐang gọi Tìm từ khoá...\nTìm từ khoá: «Nghị định 30»\n</think>\n\nNghị định 30/2020 quy định về công tác văn thư.",
			want:  "Nghị định 30/2020 quy định về công tác văn thư.",
		},
		{
			name:  "multiple blocks",
			input: "<think>first</think>Part 1. <think>second</think>Part 2.",
			want:  "Part 1. Part 2.",
		},
		{name: "only think block", input: "<think>just thinking</think>", want: ""},
		{
			name:  "unclosed think block",
			input: "<think>still streaming",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripThinkBlocks(tt.input)
			if got != tt.want {
				t.Fatalf("StripThinkBlocks() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatIMDisplayContent_intermediate_showsThinkingStyled(t *testing.T) {
	raw := "<think>\nPhân tích câu hỏi của người dùng\nĐang gọi Tra cứu kho tri thức...\n</think>\n\n"
	got := FormatIMDisplayContent(raw, StreamDisplayIntermediate)

	if !strings.Contains(got, "Quá trình suy nghĩ") {
		t.Fatalf("intermediate display should include thinking header, got: %q", got)
	}
	if !strings.Contains(got, "Phân tích câu hỏi của người dùng") {
		t.Fatalf("intermediate display should include thinking body, got: %q", got)
	}
	if !strings.Contains(got, "Tra cứu kho tri thức") {
		t.Fatalf("intermediate display should include tool progress, got: %q", got)
	}
	if strings.Contains(got, "<think>") {
		t.Fatalf("intermediate display must not leak raw think tags, got: %q", got)
	}
}

func TestFormatIMDisplayContent_intermediate_inProgressThink(t *testing.T) {
	raw := "<think>\nĐang suy luận\nĐang gọi Tìm từ khoá...\n"
	got := FormatIMDisplayContent(raw, StreamDisplayIntermediate)

	if !strings.Contains(got, "suy nghĩ") {
		t.Fatalf("open think block should show thinking header, got: %q", got)
	}
	if strings.Contains(got, "<think>") {
		t.Fatalf("must not leak raw tags, got: %q", got)
	}
}

func TestFormatIMDisplayContent_intermediate_showsAnswerPreview(t *testing.T) {
	raw := "<think>\nĐang tra cứu\n</think>\n\nNghị định 30/2020 quy định về công tác văn thư."
	got := FormatIMDisplayContent(raw, StreamDisplayIntermediate)

	if !strings.Contains(got, "Nghị định 30/2020 quy định về công tác văn thư") {
		t.Fatalf("intermediate display should preview answer after think block, got: %q", got)
	}
}

func TestFormatIMDisplayContent_final_stripsThinkingAndTools(t *testing.T) {
	raw := "<think>\nĐể tôi tra cứu kho tri thức trước\nĐang gọi Tìm từ khoá...\nTìm từ khoá: «Nghị định 30»\n</think>\n\nNghị định 30/2020 quy định về công tác văn thư."
	got := FormatIMDisplayContent(raw, StreamDisplayFinal)

	want := "Nghị định 30/2020 quy định về công tác văn thư."
	if got != want {
		t.Fatalf("final display = %q, want %q", got, want)
	}
}

func TestFormatIMDisplayContent_final_plainAnswerUnchanged(t *testing.T) {
	raw := "Đây là câu trả lời cuối cùng."
	got := FormatIMDisplayContent(raw, StreamDisplayFinal)
	if got != raw {
		t.Fatalf("final display = %q, want %q", got, raw)
	}
}

func TestFormatIMDisplayContent_final_ragPipelineHidden(t *testing.T) {
	raw := "<think>\nĐang hiểu câu hỏi...\nĐã hiểu xong câu hỏi\nĐang tra cứu kho tri thức...\nTra cứu kho tri thức: «query» · Tìm thấy 3 kết quả\n</think>\n\nTheo kho tri thức, đáp án là A."
	got := FormatIMDisplayContent(raw, StreamDisplayFinal)

	if strings.Contains(got, "hiểu câu hỏi") || strings.Contains(got, "Tra cứu kho tri thức") {
		t.Fatalf("final display must not contain RAG pipeline steps, got: %q", got)
	}
	if got != "Theo kho tri thức, đáp án là A." {
		t.Fatalf("final display = %q", got)
	}
}

func TestFormatIMAgentIntermediate_answerFirstBeforeTools(t *testing.T) {
	parts := IMStreamParts{
		Mode:       IMStreamModeAgent,
		LiveAnswer: "Được, để tôi tra cứu kho tri thức trước.",
	}
	got := FormatIMIntermediateFromParts(parts, true)
	if got != "Được, để tôi tra cứu kho tri thức trước." {
		t.Fatalf("should stream as plain answer, got: %q", got)
	}
	if strings.Contains(got, "Quá trình suy nghĩ") {
		t.Fatal("think header must not appear while answer is live")
	}
}

func TestFormatIMAgentIntermediate_retractIntoThinkOnTools(t *testing.T) {
	parts := IMStreamParts{
		Mode:       IMStreamModeAgent,
		AgentInner: "Được, để tôi tra cứu kho tri thức trước.\n",
		AgentToolSteps: []IMToolStep{
			{ToolName: "grep_chunks", Pending: true},
			{ToolName: "knowledge_search", Success: true, Arguments: map[string]any{"query": "Nghị định 30"}},
		},
	}
	got := FormatIMIntermediateFromParts(parts, true)
	if !strings.Contains(got, "Quá trình suy nghĩ") {
		t.Fatalf("after tool retract should show think block, got: %q", got)
	}
	if !strings.Contains(got, "Được, để tôi tra cứu kho tri thức trước") {
		t.Fatalf("retracted preamble should be inside think, got: %q", got)
	}
	if !strings.Contains(got, "Tìm từ khoá") {
		t.Fatalf("tool lines should be inside think, got: %q", got)
	}
	if !strings.Contains(got, "Nghị định 30") {
		t.Fatalf("tool query should be inside think, got: %q", got)
	}
}

func TestFormatIMAgentIntermediate_newAnswerAfterTools(t *testing.T) {
	parts := IMStreamParts{
		Mode:       IMStreamModeAgent,
		AgentInner: "Được, để tôi tra cứu\n",
		AgentToolSteps: []IMToolStep{
			{ToolName: "knowledge_search", Success: true, Arguments: map[string]any{"query": "Nghị định 30"}},
		},
		LiveAnswer: "Theo kết quả tra cứu, Nghị định 30 là…",
	}
	got := FormatIMIntermediateFromParts(parts, true)
	if !strings.Contains(got, "Theo kết quả tra cứu, Nghị định 30 là…") {
		t.Fatalf("should still stream live answer, got: %q", got)
	}
	if !strings.Contains(got, "Quá trình suy nghĩ") {
		t.Fatalf("think block should stay visible above answer, got: %q", got)
	}
	if !strings.Contains(got, "Nghị định 30") {
		t.Fatalf("tool query should remain in think block, got: %q", got)
	}
}

func TestBuildIMStreamRaw_agentInProgress_mergesToolsAndNarrativeIntoThink(t *testing.T) {
	parts := IMStreamParts{
		Mode:       IMStreamModeAgent,
		AgentInner: "Người dùng lại hỏi về Nghị định 30\n",
		AgentToolSteps: []IMToolStep{
			{ToolName: "grep_chunks", Pending: true},
			{ToolName: "knowledge_search", Success: true},
		},
	}
	got := FormatIMIntermediateFromParts(parts, true)

	if !strings.Contains(got, "Tìm từ khoá") {
		t.Fatalf("tool progress should be inside think block, got: %q", got)
	}
	if !strings.Contains(got, "Quá trình suy nghĩ") {
		t.Fatalf("agent tooling phase should show think header, got: %q", got)
	}
}

func TestFormatIMQuickQA_separatesPipelineAndThinking(t *testing.T) {
	parts := IMStreamParts{
		Mode: IMStreamModeQuickQA,
		PipelineToolSteps: []IMToolStep{
			{ToolName: "query_understand", Pending: true},
			{ToolName: "knowledge_search", Success: true, Arguments: map[string]any{"query": "Nghị định 30"}},
		},
		ReasoningInner: "Phân tích ý định câu hỏi…",
	}
	got := FormatIMIntermediateFromParts(parts, false)

	if strings.Contains(got, "Quá trình suy nghĩ") {
		t.Fatalf("quick QA should not use agent think header, got: %q", got)
	}
	if !strings.Contains(got, "> 💭 **Suy nghĩ**") {
		t.Fatalf("quick QA reasoning should use separate think section, got: %q", got)
	}
	if !strings.Contains(got, "Phân tích ý định câu hỏi") {
		t.Fatalf("reasoning body missing, got: %q", got)
	}
	if !strings.Contains(got, "Đang hiểu câu hỏi") {
		t.Fatalf("pipeline steps missing, got: %q", got)
	}
	if !strings.Contains(got, "Nghị định 30") {
		t.Fatalf("pipeline query missing, got: %q", got)
	}
}

func TestFormatIMQuickQA_collapsesToAnswerWhenStreaming(t *testing.T) {
	parts := IMStreamParts{
		Mode: IMStreamModeQuickQA,
		PipelineToolSteps: []IMToolStep{
			{ToolName: "query_understand", Success: true},
			{ToolName: "knowledge_search", Success: true},
		},
		Answer: "Nghị định 30/2020 quy định về công tác văn thư.",
	}
	got := FormatIMIntermediateFromParts(parts, false)
	if got != "Nghị định 30/2020 quy định về công tác văn thư." {
		t.Fatalf("quick QA should collapse to answer preview, got: %q", got)
	}
}

func TestFormatIMFinalFromParts_agentAnswerOnly(t *testing.T) {
	parts := IMStreamParts{
		Mode:       IMStreamModeAgent,
		AgentInner: "Được, để tôi tra cứu\n",
		AgentToolSteps: []IMToolStep{
			{ToolName: "grep_chunks", Pending: true},
			{ToolName: "knowledge_search", Success: true},
		},
		LiveAnswer: "Không được xuất hiện trong tin nhắn cuối",
		Answer:     "Nghị định 30/2020 quy định về công tác văn thư.",
	}
	got := FormatIMFinalFromParts(parts)
	if got != "Nghị định 30/2020 quy định về công tác văn thư." {
		t.Fatalf("final should be answer-only, got: %q", got)
	}
	if strings.Contains(got, "Quá trình suy nghĩ") {
		t.Fatalf("final must not include collapsed think header, got: %q", got)
	}
}

func TestFormatIMFinalFromParts_usesAnswerOnly(t *testing.T) {
	parts := IMStreamParts{
		Mode:              IMStreamModeQuickQA,
		PipelineToolSteps: []IMToolStep{{ToolName: "query_understand", Success: true}},
		ReasoningInner:    "Đang suy luận",
		AgentToolSteps:    []IMToolStep{{ToolName: "grep_chunks", Pending: true}},
		Answer:            "Nghị định 30/2020 quy định về công tác văn thư.",
	}
	got := FormatIMFinalFromParts(parts)
	if got != "Nghị định 30/2020 quy định về công tác văn thư." {
		t.Fatalf("final display = %q", got)
	}
}

func TestIsRAGPipelineToolName_matchesWeb(t *testing.T) {
	for _, name := range []string{"query_understand", "knowledge_search"} {
		if !IsRAGPipelineToolName(name) {
			t.Fatalf("%q should be a RAG pipeline tool", name)
		}
	}
	if IsRAGPipelineToolName("grep_chunks") {
		t.Fatal("grep_chunks is agent tool, not RAG pipeline progress tool")
	}
}
