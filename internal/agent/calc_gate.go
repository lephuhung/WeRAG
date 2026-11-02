package agent

import (
	"context"
	"regexp"
	"strings"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// Figures a domain pack computes come from its Go tools, never from the
// model (user decision 2026-10-19: model-computed figures go through
// tools). Qwen still answered a household-tax question with figures without
// calling calculate_household_tax and opened with a conclusion its own body
// contradicted ("VẪN PHẢI nộp thuế TNCN" over "Miễn thuế TNCN"); with the
// tool called, the conclusion follows the tool's code-written "Đọc kết
// quả". So when the question carries an amount of money and the agent has a
// calculation tool not yet called this turn, a round is not streamed: a tool
// call goes ahead as usual; a plain answer that concludes (an amount, or
// "phải / không phải nộp…") is held back once and the model is asked to call
// the tool first. A second plain answer is shown as it is (the model may
// have good reason, e.g. a figure the user must still give), and so is a
// plain answer that concludes nothing (a clarifying question).

type calcGateKey struct{}

var (
	// an amount of money: "2,4 tỷ", "900 triệu đồng", "1.000.000.000 đ"
	calcAmountRe = regexp.MustCompile(`(?i)\d[\d.,]*\s*(?:tỷ|triệu|nghìn|ngàn|đồng|vnđ|vnd|đ)(?:[^\p{L}]|$)`)
	// a conclusion on an obligation or an amount owed
	calcConclusionRe = regexp.MustCompile(`(?i)(?:phải|cần|không phải|không cần)\s+nộp|được\s+miễn|miễn\s+thuế|không\s+chịu\s+thuế|thuộc\s+(?:diện|đối tượng)\s+(?:không\s+)?(?:phải\s+)?nộp`)
)

// calculationToolsOffered lists the calculation tools among the round's
// tools, in their order.
func calculationToolsOffered(tools []chat.Tool) []string {
	var out []string
	for _, t := range tools {
		if agenttools.IsCalculationTool(t.Function.Name) {
			out = append(out, t.Function.Name)
		}
	}
	return out
}

// calculationToolCalled reports whether a step of the turn called one of
// names (a failed call counts: the model tried, its error says what is
// missing).
func calculationToolCalled(steps []types.AgentStep, names []string) bool {
	for _, s := range steps {
		for _, tc := range s.ToolCalls {
			for _, n := range names {
				if tc.ExecutionName() == n {
					return true
				}
			}
		}
	}
	return false
}

// markCalcRequired flags the round's ctx with the calculation tools to call
// first when the turn's question carries an amount, the round offers such a
// tool and none was called yet; ctx unchanged otherwise.
func markCalcRequired(ctx context.Context, tools []chat.Tool, steps []types.AgentStep) context.Context {
	names := calculationToolsOffered(tools)
	if len(names) == 0 || calculationToolCalled(steps, names) {
		return ctx
	}
	if !calcAmountRe.MatchString(types.UserQueryFromContext(ctx)) {
		return ctx
	}
	return context.WithValue(ctx, calcGateKey{}, names)
}

// calcRequiredTools is what markCalcRequired recorded; nil when the round
// is not gated.
func calcRequiredTools(ctx context.Context) []string {
	names, _ := ctx.Value(calcGateKey{}).([]string)
	return names
}

// concludesWithoutTool reports whether a plain answer states an amount or
// an obligation, which only a calculation tool may decide.
func concludesWithoutTool(answer string) bool {
	return calcAmountRe.MatchString(answer) || calcConclusionRe.MatchString(answer)
}

// calcToolNudge asks the model to call the tools before concluding.
func calcToolNudge(names []string) string {
	return "[Hệ thống] Câu hỏi có số tiền và trợ lý có công cụ tính (" + strings.Join(names, ", ") + "). " +
		"Chưa kết luận số thuế hay việc phải / không phải nộp: gọi công cụ đó trước, với các tham số tra được trong văn bản hiện hành. " +
		"Thiếu số liệu thì hỏi người dùng. Không tự tính, không trả lời lại khi chưa gọi công cụ."
}

// calcGateRound runs a gated round without streaming. Tool calls go ahead
// (shown as pending, as a streamed round does). A plain answer that
// concludes is held back once: the same messages plus calcToolNudge are
// sent again, and whatever comes back is used. analyzeResponse emits a
// plain answer, since it was not streamed.
func (e *AgentEngine) calcGateRound(
	ctx context.Context, messages []chat.Message, opts *chat.ChatOptions, iteration int, sessionID string,
) (*types.ChatResponse, error) {
	names := calcRequiredTools(ctx)
	encoded := e.modelContext.EncodeMessages(messages)
	reportModelContextLeaks(ctx, "Agent", e.modelContext, encoded)
	callCtx := types.WithLLMCallMetadata(ctx, "agent_round_calc_gate", chat.PromptPrefixFingerprint(encoded, opts))
	resp, err := e.chatModel.Chat(callCtx, encoded, opts)
	if err != nil {
		return nil, err
	}
	if len(resp.ToolCalls) == 0 && concludesWithoutTool(resp.Content) {
		logger.Warnf(ctx, "[Agent][CalcGate] Round %d concluded without %v (%d chars); asking for the tool",
			iteration+1, names, len(resp.Content))
		retry := append(append([]chat.Message{}, messages...), chat.Message{Role: "user", Content: calcToolNudge(names)})
		encoded = e.modelContext.EncodeMessages(retry)
		callCtx = types.WithLLMCallMetadata(ctx, "agent_round_calc_gate_retry", chat.PromptPrefixFingerprint(encoded, opts))
		again, err := e.chatModel.Chat(callCtx, encoded, opts)
		if err != nil {
			return nil, err
		}
		if len(again.ToolCalls) == 0 {
			logger.Warnf(ctx, "[Agent][CalcGate] Round %d answered again without %v; shown as it is", iteration+1, names)
		}
		resp = again
	}
	if len(resp.ToolCalls) > 0 {
		resp.Content = ""
		e.emitForcedToolCalls(ctx, resp.ToolCalls, iteration, sessionID)
		return resp, nil
	}
	resp.ReasoningContent = ""
	resp.AnswerStreamed = false
	resp.AnswerEventID = ""
	return resp, nil
}
