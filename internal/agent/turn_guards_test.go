package agent

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

func failedCall(name string) types.ToolCall {
	return types.ToolCall{Name: name, Result: &types.ToolResult{Success: false, Error: "boom"}}
}

func TestDropExhaustedTools(t *testing.T) {
	tools := []chat.Tool{{Function: chat.FunctionDef{Name: "draw_diagram"}}, {Function: chat.FunctionDef{Name: "search_knowledge"}}}
	steps := []types.AgentStep{{ToolCalls: []types.ToolCall{failedCall("draw_diagram"), failedCall("draw_diagram")}}}
	if got := dropExhaustedTools(tools, steps); len(got) != 2 {
		t.Fatalf("two failures must keep the tool, got %d tools", len(got))
	}
	steps = append(steps, types.AgentStep{ToolCalls: []types.ToolCall{failedCall("draw_diagram")}})
	got := dropExhaustedTools(tools, steps)
	if len(got) != 1 || got[0].Function.Name != "search_knowledge" {
		t.Fatalf("three failures must drop draw_diagram, got %+v", got)
	}
}

func TestNoteExhaustedTools(t *testing.T) {
	prior := []types.AgentStep{{ToolCalls: []types.ToolCall{failedCall("draw_diagram"), failedCall("draw_diagram")}}}
	step := types.AgentStep{ToolCalls: []types.ToolCall{failedCall("draw_diagram"), {Name: "search_knowledge", Result: &types.ToolResult{Success: true}}}}
	noteExhaustedTools(&step, prior)
	if !strings.Contains(step.ToolCalls[0].Result.Error, "đã lỗi 3 lần") {
		t.Fatalf("third failure must carry the stop note, got %q", step.ToolCalls[0].Result.Error)
	}
	if step.ToolCalls[1].Result.Error != "" {
		t.Fatalf("a successful call must stay untouched")
	}
}

func TestForcedFinalMessage(t *testing.T) {
	ok := types.ToolCall{Name: "check_document_format", Result: &types.ToolResult{Success: true, Data: map[string]interface{}{ToolResultFinalMessageKey: " Đang kiểm tra. "}}}
	if got := forcedFinalMessage(types.AgentStep{ToolCalls: []types.ToolCall{ok}}); got != "Đang kiểm tra." {
		t.Fatalf("got %q", got)
	}
	if got := forcedFinalMessage(types.AgentStep{ToolCalls: []types.ToolCall{ok, failedCall("x")}}); got != "" {
		t.Fatalf("a round where another call failed must not be cut short, got %q", got)
	}
	// a parallel successful call still reports, after the fixed answer
	spell := types.ToolCall{Name: "check_spelling", Result: &types.ToolResult{Success: true,
		Output: "Không phát hiện lỗi chính tả trong 18 đoạn của thong_bao.docx.\n",
		Data:   map[string]interface{}{"document_ops": []interface{}{}}}}
	if got := forcedFinalMessage(types.AgentStep{ToolCalls: []types.ToolCall{ok, spell}}); got !=
		"Đang kiểm tra.\n\nKhông phát hiện lỗi chính tả trong 18 đoạn của thong_bao.docx." {
		t.Fatalf("parallel results must follow the fixed answer, got %q", got)
	}
	// spelling underlines reach the editor through the tool_result event:
	// a check_spelling that marked findings still ends the turn
	marked := types.ToolCall{Name: "check_spelling", Result: &types.ToolResult{Success: true, Output: "Phát hiện 2 lỗi chính tả",
		Data: map[string]interface{}{"document_ops": []map[string]interface{}{{"op": "mark"}}}}}
	if got := forcedFinalMessage(types.AgentStep{ToolCalls: []types.ToolCall{ok, marked}}); got != "Đang kiểm tra.\n\nPhát hiện 2 lỗi chính tả" {
		t.Fatalf("an allow-listed tool with document ops still reports, got %q", got)
	}
	// a paged spelling check with parts left keeps the turn going
	paged := types.ToolCall{Name: "check_spelling", Result: &types.ToolResult{Success: true,
		Output: "Không phát hiện lỗi chính tả trong 40 đoạn của a.docx.\nCòn các đoạn sau: gọi lại với from=40.\n"}}
	if got := forcedFinalMessage(types.AgentStep{ToolCalls: []types.ToolCall{ok, paged}}); got != "" {
		t.Fatalf("a paged check with parts left must not end the turn, got %q", got)
	}
	// other tools' document ops have more to report
	inserted := types.ToolCall{Name: "insert_paragraphs", Result: &types.ToolResult{Success: true,
		Data: map[string]interface{}{"document_ops": []map[string]interface{}{{"op": "insertParagraph"}}}}}
	if got := forcedFinalMessage(types.AgentStep{ToolCalls: []types.ToolCall{ok, inserted}}); got != "" {
		t.Fatalf("a round with another tool's document ops must not be cut short, got %q", got)
	}
	// the legal-candidates card next to a knowledge search: the search output
	// is markup for the model, never appended to the answer (review B1)
	card := types.ToolCall{Name: "check_legal_status", Result: &types.ToolResult{Success: true,
		Output: "3 documents match", Data: map[string]interface{}{ToolResultFinalMessageKey: "Bạn muốn hỏi văn bản nào?"}}}
	search := types.ToolCall{Name: "search_knowledge", Result: &types.ToolResult{Success: true,
		Output: "<search_results>\n<result rank=\"1\">\n<content>Điều 1…</content>\n</result>\n</search_results>"}}
	if got := forcedFinalMessage(types.AgentStep{ToolCalls: []types.ToolCall{card, search}}); got != "" {
		t.Fatalf("a parallel search must keep the turn going, got %q", got)
	}
	// two fixed answers in one round: neither wins
	if got := forcedFinalMessage(types.AgentStep{ToolCalls: []types.ToolCall{ok, ok}}); got != "" {
		t.Fatalf("two final messages must not end the turn, got %q", got)
	}
	if got := forcedFinalMessage(types.AgentStep{ToolCalls: []types.ToolCall{failedCall("x")}}); got != "" {
		t.Fatalf("a failed result must not end the turn, got %q", got)
	}
}

func okCall(name string) types.ToolCall {
	return types.ToolCall{Name: name, Result: &types.ToolResult{Success: true, Output: "Listed 1/1 chunks"}}
}

func TestDropExhaustedToolsCapsRepeatedReadCalls(t *testing.T) {
	tools := []chat.Tool{{Function: chat.FunctionDef{Name: "search_document_section"}}, {Function: chat.FunctionDef{Name: "read_document"}},
		{Function: chat.FunctionDef{Name: "draw_diagram"}}}
	var steps []types.AgentStep
	for i := 0; i < 7; i++ {
		steps = append(steps, types.AgentStep{ToolCalls: []types.ToolCall{okCall("search_document_section")}})
	}
	if got := dropExhaustedTools(tools, steps); len(got) != 3 {
		t.Fatalf("seven calls keep the tool, got %d tools", len(got))
	}
	steps = append(steps, types.AgentStep{ToolCalls: []types.ToolCall{okCall("search_document_section")}})
	got := dropExhaustedTools(tools, steps)
	if len(got) != 2 || got[0].Function.Name != "read_document" {
		t.Fatalf("eight successful calls must drop search_document_section, got %+v", got)
	}
	// search_knowledge keeps the tighter cap of 6
	sk := []chat.Tool{{Function: chat.FunctionDef{Name: "search_knowledge"}}}
	steps = nil
	for i := 0; i < 6; i++ {
		steps = append(steps, types.AgentStep{ToolCalls: []types.ToolCall{okCall("search_knowledge")}})
	}
	if got := dropExhaustedTools(sk, steps); len(got) != 0 {
		t.Fatalf("six searches must drop search_knowledge, got %+v", got)
	}
	// a tool without a cap is never dropped for its successes
	steps = nil
	for i := 0; i < 10; i++ {
		steps = append(steps, types.AgentStep{ToolCalls: []types.ToolCall{okCall("draw_diagram")}})
	}
	if got := dropExhaustedTools(tools, steps); len(got) != 3 {
		t.Fatalf("an uncapped tool stays, got %d tools", len(got))
	}
}

func TestNoteExhaustedToolsCapNote(t *testing.T) {
	var prior []types.AgentStep
	for i := 0; i < 7; i++ {
		prior = append(prior, types.AgentStep{ToolCalls: []types.ToolCall{okCall("search_document_section")}})
	}
	step := types.AgentStep{ToolCalls: []types.ToolCall{okCall("search_document_section"), okCall("draw_diagram")}}
	noteExhaustedTools(&step, prior)
	out := step.ToolCalls[0].Result.Output
	if !strings.Contains(out, "Đã gọi search_document_section 8 lần") || !strings.Contains(out, "read_document(id=…)") {
		t.Fatalf("the eighth call must carry the cap note, got %q", out)
	}
	if step.ToolCalls[1].Result.Output != "Listed 1/1 chunks" {
		t.Fatalf("an uncapped tool stays untouched")
	}
	// the cap of read_document itself does not point back to read_document
	prior = nil
	for i := 0; i < 7; i++ {
		prior = append(prior, types.AgentStep{ToolCalls: []types.ToolCall{okCall("read_document")}})
	}
	step = types.AgentStep{ToolCalls: []types.ToolCall{okCall("read_document")}}
	noteExhaustedTools(&step, prior)
	if out := step.ToolCalls[0].Result.Output; !strings.Contains(out, "Đã gọi read_document 8 lần") || strings.Contains(out, "dùng read_document") {
		t.Fatalf("read_document cap note, got %q", out)
	}
}
