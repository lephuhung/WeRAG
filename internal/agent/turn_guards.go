package agent

import (
	"fmt"
	"strings"

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
//     final_message ends the turn with that text when it was the round's
//     only tool call.

// maxToolFailuresPerTurn is the number of failed calls of one tool after
// which the tool is withdrawn for the rest of the turn.
const maxToolFailuresPerTurn = 3

// toolExhaustedNote ends the error of the failure that reached the cap.
const toolExhaustedNote = "\n\n[Công cụ %s đã lỗi %d lần trong lượt này và không còn được cung cấp nữa. " +
	"ĐỪNG gọi lại: trả lời người dùng với những gì đã có, nói rõ việc chưa làm được và vì sao.]"

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

// dropExhaustedTools removes the tools that failed maxToolFailuresPerTurn
// times or more in the steps so far.
func dropExhaustedTools(tools []chat.Tool, steps []types.AgentStep) []chat.Tool {
	counts := toolFailureCounts(steps)
	exhausted := false
	for _, n := range counts {
		if n >= maxToolFailuresPerTurn {
			exhausted = true
			break
		}
	}
	if !exhausted {
		return tools
	}
	out := make([]chat.Tool, 0, len(tools))
	for _, t := range tools {
		if counts[t.Function.Name] >= maxToolFailuresPerTurn {
			continue
		}
		out = append(out, t)
	}
	return out
}

// noteExhaustedTools appends toolExhaustedNote to the error of every failed
// call in step that brings its tool to the cap (counting prior steps), so
// the model reads the stop instruction with the failure itself.
func noteExhaustedTools(step *types.AgentStep, prior []types.AgentStep) {
	counts := toolFailureCounts(prior)
	for i := range step.ToolCalls {
		tc := &step.ToolCalls[i]
		if tc.Result == nil || tc.Result.Success {
			continue
		}
		name := tc.ExecutionName()
		counts[name]++
		if counts[name] >= maxToolFailuresPerTurn && !strings.Contains(tc.Result.Error, "không còn được cung cấp") {
			tc.Result.Error += fmt.Sprintf(toolExhaustedNote, name, counts[name])
		}
	}
}

// forcedFinalMessage is the fixed answer a tool result asked for, "" when
// none: only when the result succeeded and was the round's only tool call
// (a round that also ran other tools has more to report).
func forcedFinalMessage(step types.AgentStep) string {
	if len(step.ToolCalls) != 1 {
		return ""
	}
	r := step.ToolCalls[0].Result
	if r == nil || !r.Success || r.Data == nil {
		return ""
	}
	msg, _ := r.Data[ToolResultFinalMessageKey].(string)
	return strings.TrimSpace(msg)
}
