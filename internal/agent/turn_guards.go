package agent

import (
	"fmt"
	"reflect"
	"strings"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// Two guards on a turn, both seen live with Qwen3.6 (2026-10-13):
//
//   - a tool that keeps failing: draw_diagram was called 30 times with the
//     same rejected source, discover_mcp_tools 29 times with the same wrong
//     server_id. After maxToolFailuresPerTurn failures the tool's result
//     tells the model to stop and the tool is no longer offered this turn.
//   - a tool result that ends the turn: check_document_format sending the
//     check to the background asks for a short fixed reply, and the model
//     added its own (wrong) findings instead. A result whose Data carries
//     final_message ends the turn with that text (plus the Output of a
//     user-facing tool run in parallel, e.g. check_spelling).
//
// And a third, seen 2026-10-16: search_document_section called 21 times in
// one turn, one Điều per call. The read tools in maxToolCallsPerTurn are
// withdrawn after that many calls, successful or not.

// maxToolFailuresPerTurn is the number of failed calls of one tool after
// which the tool is withdrawn for the rest of the turn.
const maxToolFailuresPerTurn = 3

// toolExhaustedNote ends the error of the failure that reached the cap.
const toolExhaustedNote = "\n\n[Công cụ %s đã lỗi %d lần trong lượt này và không còn được cung cấp nữa. " +
	"ĐỪNG gọi lại: trả lời người dùng với những gì đã có, nói rõ việc chưa làm được và vì sao.]"

// maxToolCallsPerTurn caps the calls of a read tool in one turn, whatever
// their outcome; the call that reaches the cap carries toolCapNote and the
// tool is no longer offered this turn.
var maxToolCallsPerTurn = map[string]int{
	agenttools.ToolSearchDocumentSection: 8,
	agenttools.ToolReadDocument:          8,
	agenttools.ToolReadDocumentOutline:   6,
	agenttools.ToolSearchKnowledge:       6,
}

// toolCapNote ends the result of the call that reached its tool's cap.
const toolCapNote = "\n\n[Đã gọi %s %d lần trong lượt này; công cụ không còn được cung cấp nữa. " +
	"Trả lời người dùng với những gì đã có%s.]"

// toolCapHint is the cheaper way on to the rest of the text, per capped tool.
var toolCapHint = map[string]string{
	agenttools.ToolSearchDocumentSection: "; nếu cần đọc thêm, dùng read_document(id=…) theo trang thay vì từng Điều",
}

// ToolResultFinalMessageKey is the Data key a tool sets to end the turn
// with a fixed answer (see forcedFinalMessage).
const ToolResultFinalMessageKey = "final_message"

// toolFailureCounts counts the failed calls per tool over the steps.
func toolFailureCounts(steps []types.AgentStep) map[string]int {
	counts := map[string]int{}
	for _, s := range steps {
		for _, tc := range s.ToolCalls {
			if tc.Result != nil && !tc.Result.Success {
				counts[tc.ExecutionName()]++
			}
		}
	}
	return counts
}

// toolCallCounts counts every executed call per tool over the steps.
func toolCallCounts(steps []types.AgentStep) map[string]int {
	counts := map[string]int{}
	for _, s := range steps {
		for _, tc := range s.ToolCalls {
			if tc.Result != nil {
				counts[tc.ExecutionName()]++
			}
		}
	}
	return counts
}

// overCallCap reports whether a tool with calls calls in the turn reached
// its maxToolCallsPerTurn cap (tools without a cap never do).
func overCallCap(name string, calls int) bool {
	limit, ok := maxToolCallsPerTurn[name]
	return ok && calls >= limit
}

// dropExhaustedTools removes the tools that failed maxToolFailuresPerTurn
// times or more, or reached their maxToolCallsPerTurn cap, in the steps so
// far.
func dropExhaustedTools(tools []chat.Tool, steps []types.AgentStep) []chat.Tool {
	failures := toolFailureCounts(steps)
	calls := toolCallCounts(steps)
	exhausted := func(name string) bool {
		return failures[name] >= maxToolFailuresPerTurn || overCallCap(name, calls[name])
	}
	found := false
	for _, t := range tools {
		if exhausted(t.Function.Name) {
			found = true
			break
		}
	}
	if !found {
		return tools
	}
	out := make([]chat.Tool, 0, len(tools))
	for _, t := range tools {
		if exhausted(t.Function.Name) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// noteExhaustedTools appends toolExhaustedNote to the error of every failed
// call in step that brings its tool to the failure cap, and toolCapNote to
// the call that brings a read tool to its call cap (counting prior steps),
// so the model reads the stop instruction with the result itself.
func noteExhaustedTools(step *types.AgentStep, prior []types.AgentStep) {
	failures := toolFailureCounts(prior)
	calls := toolCallCounts(prior)
	for i := range step.ToolCalls {
		tc := &step.ToolCalls[i]
		if tc.Result == nil {
			continue
		}
		name := tc.ExecutionName()
		calls[name]++
		if !tc.Result.Success {
			failures[name]++
			if failures[name] >= maxToolFailuresPerTurn && !strings.Contains(tc.Result.Error, "không còn được cung cấp") {
				tc.Result.Error += fmt.Sprintf(toolExhaustedNote, name, failures[name])
				continue
			}
		}
		if limit, ok := maxToolCallsPerTurn[name]; ok && calls[name] == limit {
			note := fmt.Sprintf(toolCapNote, name, calls[name], toolCapHint[name])
			if tc.Result.Success {
				tc.Result.Output += note
			} else if !strings.Contains(tc.Result.Error, "không còn được cung cấp") {
				tc.Result.Error += note
			}
		}
	}
}

// userFacingTools are the tools whose Output is written for the user, so
// it may follow a fixed answer as is; their document_ops (spelling
// underlines) reach the editor through the tool_result event, not the
// answer text, so they do not keep the turn going either.
var userFacingTools = map[string]bool{
	agenttools.ToolCheckSpelling: true,
}

// forcedFinalMessage is the fixed answer a tool result asked for, "" when
// none. Exactly one successful result of the round must carry it; the
// Output of each other successful call follows it when the tool is in
// userFacingTools (a parallel check_spelling still reports). A round where
// another call failed, two results carry a final_message, or another call
// is not user-facing (search results, read chunks: markup for the model)
// or asks to be called again has more to do and is not cut short.
func forcedFinalMessage(step types.AgentStep) string {
	msg := ""
	var others []string
	for _, tc := range step.ToolCalls {
		r := tc.Result
		if r == nil || !r.Success {
			return ""
		}
		if m, _ := r.Data[ToolResultFinalMessageKey].(string); strings.TrimSpace(m) != "" {
			if msg != "" {
				return ""
			}
			msg = strings.TrimSpace(m)
			continue
		}
		out := strings.TrimSpace(r.Output)
		if !userFacingTools[tc.ExecutionName()] {
			if out != "" || hasDocumentOps(r.Data) {
				return ""
			}
			continue
		}
		if strings.Contains(out, "gọi lại với") { // a paged check with parts left
			return ""
		}
		if out != "" {
			others = append(others, out)
		}
	}
	if msg == "" {
		return ""
	}
	return strings.Join(append([]string{msg}, others...), "\n\n")
}

// finalMessageTool names the call of the round that carried final_message.
func finalMessageTool(step types.AgentStep) string {
	for _, tc := range step.ToolCalls {
		if tc.Result != nil {
			if m, _ := tc.Result.Data[ToolResultFinalMessageKey].(string); strings.TrimSpace(m) != "" {
				return tc.ExecutionName()
			}
		}
	}
	return ""
}

// hasDocumentOps reports whether a result's Data carries a non-empty
// document_ops list.
func hasDocumentOps(data map[string]interface{}) bool {
	v, ok := data["document_ops"]
	if !ok || v == nil {
		return false
	}
	rv := reflect.ValueOf(v)
	return (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) && rv.Len() > 0
}
