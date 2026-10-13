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
		t.Fatalf("a round with several tool calls must not be cut short, got %q", got)
	}
	if got := forcedFinalMessage(types.AgentStep{ToolCalls: []types.ToolCall{failedCall("x")}}); got != "" {
		t.Fatalf("a failed result must not end the turn, got %q", got)
	}
}
