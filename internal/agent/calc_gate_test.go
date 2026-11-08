package agent

import (
	"context"
	"strings"
	"testing"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func calcTools(names ...string) []chat.Tool {
	var out []chat.Tool
	for _, n := range names {
		out = append(out, chat.Tool{Type: "function", Function: chat.FunctionDef{Name: n}})
	}
	return out
}

func TestMarkCalcRequired(t *testing.T) {
	tax := calcTools(agenttools.ToolSearchKnowledge, agenttools.ToolCalculateHouseholdTax)
	q := types.WithUserQuery(context.Background(), "Doanh thu năm 2,4 tỷ đồng thì nộp bao nhiêu thuế?")
	require.Equal(t, []string{agenttools.ToolCalculateHouseholdTax}, calcRequiredTools(markCalcRequired(q, tax, nil)))
	require.Nil(t, calcRequiredTools(markCalcRequired(q, calcTools(agenttools.ToolSearchKnowledge), nil)), "no calculation tool")
	noAmount := types.WithUserQuery(context.Background(), "Thủ tục đăng ký hộ kinh doanh?")
	require.Nil(t, calcRequiredTools(markCalcRequired(noAmount, tax, nil)), "no amount in the question")
	refused := stepWith(agenttools.ToolCalculateHouseholdTax, &types.ToolResult{Success: false, Error: "Văn bản 68/2026/NĐ-CP mới hơn…"})
	require.NotNil(t, calcRequiredTools(markCalcRequired(q, tax, refused)), "a refused call still gates: its figures were wrong")
	called := stepWith(agenttools.ToolCalculateHouseholdTax, &types.ToolResult{Success: true})
	require.Nil(t, calcRequiredTools(markCalcRequired(q, tax, called)), "computed this turn")
	noTax := types.WithUserQuery(context.Background(), "Vốn điều lệ 2 tỷ đồng thì lệ phí môn bài bao nhiêu?")
	require.Nil(t, calcRequiredTools(markCalcRequired(noTax, tax, nil)), "an amount without a tax subject")
	require.True(t, calcAmountRe.MatchString("doanh thu 2 tỉ"))
	require.True(t, calcAmountRe.MatchString("doanh thu 900tr"))
	require.True(t, calcAmountRe.MatchString("900 triệu một năm"))
	require.True(t, calcAmountRe.MatchString("1.000.000.000 đ"))
	require.False(t, calcAmountRe.MatchString("Điều 4 khoản 1"))
}

func TestConcludesWithoutTool(t *testing.T) {
	require.True(t, concludesWithoutTool("Bạn KHÔNG phải nộp thuế GTGT nhưng VẪN PHẢI nộp thuế TNCN."))
	require.True(t, concludesWithoutTool("Tổng thuế là 31.000.000 đồng."))
	require.True(t, concludesWithoutTool("Hộ của bạn được miễn thuế."))
	require.False(t, concludesWithoutTool("Bạn kinh doanh ngành nghề gì để tôi xác định tỷ lệ?"))
}

// A conclusion without the tool is held back once; the tool call that
// follows the nudge goes ahead, and nothing is shown.
func TestCalcGateRoundAsksForTheTool(t *testing.T) {
	call := types.LLMToolCall{ID: "c1", Type: "function", Function: types.FunctionCall{Name: agenttools.ToolCalculateHouseholdTax, Arguments: "{}"}}
	model := &scriptedChat{id: "calc-gate", replies: []*types.ChatResponse{
		{Content: "Bạn VẪN PHẢI nộp thuế TNCN 13.500.000 đồng.", FinishReason: "stop"},
		{ToolCalls: []types.LLMToolCall{call}, FinishReason: "tool_calls"},
	}}
	engine := newTestEngine(t, model)
	ctx := context.WithValue(context.Background(), calcGateKey{}, []string{agenttools.ToolCalculateHouseholdTax})
	resp, err := engine.calcGateRound(ctx, []chat.Message{{Role: "user", Content: "900 triệu"}}, &chat.ChatOptions{}, 1, "s1")
	require.NoError(t, err)
	require.Len(t, resp.ToolCalls, 1)
	require.Equal(t, call.ID, resp.ToolCalls[0].ID)
	require.Equal(t, call.Function, resp.ToolCalls[0].Function)
	require.Empty(t, resp.Content)
	require.Len(t, model.msgs, 2)
	require.Equal(t, 1, engine.calcNudges)
	last := model.msgs[1][len(model.msgs[1])-1]
	require.Equal(t, "user", last.Role)
	require.True(t, strings.HasPrefix(last.Content, "[Hệ thống]"), last.Content)
	require.Contains(t, last.Content, agenttools.ToolCalculateHouseholdTax)
}

// A second answer without the tool is shown (not streamed: analyzeResponse
// emits it); a clarifying question passes at once.
func TestCalcGateRoundShowsWhatComesBack(t *testing.T) {
	model := &scriptedChat{id: "calc-gate-2", replies: []*types.ChatResponse{
		{Content: "Bạn phải nộp thuế.", FinishReason: "stop"},
		{Content: "Bạn cho biết doanh thu cả năm?", FinishReason: "stop", AnswerStreamed: true, AnswerEventID: "a"},
	}}
	engine := newTestEngine(t, model)
	ctx := context.WithValue(context.Background(), calcGateKey{}, []string{agenttools.ToolCalculateHouseholdTax})
	resp, err := engine.calcGateRound(ctx, []chat.Message{{Role: "user", Content: "600 triệu quý I"}}, &chat.ChatOptions{}, 1, "s1")
	require.NoError(t, err)
	require.Equal(t, "Bạn cho biết doanh thu cả năm?", resp.Content)
	require.False(t, resp.AnswerStreamed)

	ask := &scriptedChat{id: "calc-gate-3", replies: []*types.ChatResponse{{Content: "Bạn kinh doanh ngành gì?", FinishReason: "stop"}}}
	engine = newTestEngine(t, ask)
	resp, err = engine.calcGateRound(ctx, []chat.Message{{Role: "user", Content: "900 triệu"}}, &chat.ChatOptions{}, 1, "s1")
	require.NoError(t, err)
	require.Equal(t, "Bạn kinh doanh ngành gì?", resp.Content)
	require.Len(t, ask.msgs, 1, "no nudge for a question back")
}

// The held-back call's tokens are spent: the round reports both calls.
// Past calcNudgeLimit the gate no longer holds answers back.
func TestCalcGateRoundUsageAndLimit(t *testing.T) {
	model := &scriptedChat{id: "calc-gate-usage", replies: []*types.ChatResponse{
		{Content: "Bạn phải nộp 31.000.000 đồng.", FinishReason: "stop", Usage: types.TokenUsage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110}},
		{Content: "Bạn cho biết ngành nghề?", FinishReason: "stop", Usage: types.TokenUsage{PromptTokens: 120, CompletionTokens: 5, TotalTokens: 125}},
	}}
	engine := newTestEngine(t, model)
	ctx := context.WithValue(context.Background(), calcGateKey{}, []string{agenttools.ToolCalculateHouseholdTax})
	resp, err := engine.calcGateRound(ctx, []chat.Message{{Role: "user", Content: "thuế 2,4 tỷ"}}, &chat.ChatOptions{}, 1, "s1")
	require.NoError(t, err)
	require.Equal(t, 220, resp.Usage.PromptTokens)
	require.Equal(t, 235, resp.Usage.TotalTokens)

	spent := &scriptedChat{id: "calc-gate-limit", replies: []*types.ChatResponse{{Content: "Bạn phải nộp thuế.", FinishReason: "stop"}}}
	engine = newTestEngine(t, spent)
	engine.calcNudges = calcNudgeLimit
	resp, err = engine.calcGateRound(ctx, []chat.Message{{Role: "user", Content: "thuế 2,4 tỷ"}}, &chat.ChatOptions{}, 1, "s1")
	require.NoError(t, err)
	require.Equal(t, "Bạn phải nộp thuế.", resp.Content)
	require.Len(t, spent.msgs, 1, "no further nudge")
}
