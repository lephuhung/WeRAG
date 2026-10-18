package agent

import (
	"context"
	"testing"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func stepWith(name string, r *types.ToolResult) []types.AgentStep {
	return []types.AgentStep{{ToolCalls: []types.ToolCall{{Name: name, Result: r}}}}
}

func TestHasEvidence(t *testing.T) {
	require.False(t, hasEvidence(nil))
	require.False(t, hasEvidence(stepWith(agenttools.ToolSearchKnowledge,
		&types.ToolResult{Success: true, Output: "No matching chunks", Data: map[string]interface{}{"count": 0}})))
	require.True(t, hasEvidence(stepWith(agenttools.ToolSearchKnowledge,
		&types.ToolResult{Success: true, Data: map[string]interface{}{"count": 3}})))
	require.True(t, hasEvidence(stepWith(agenttools.ToolWebSearch,
		&types.ToolResult{Success: true, Data: map[string]interface{}{"count": float64(2)}})))
	require.False(t, hasEvidence(stepWith(agenttools.ToolSearchKnowledge,
		&types.ToolResult{Success: false, Data: map[string]interface{}{"count": 3}})))
	require.True(t, hasEvidence(stepWith(agenttools.ToolReadDocument, &types.ToolResult{Success: true, Output: "Điều 1…"})))
	require.False(t, hasEvidence(stepWith("check_spelling", &types.ToolResult{Success: true, Output: "ok"})),
		"an editing tool is not evidence")
}

func TestMarkUngrounded(t *testing.T) {
	require.False(t, isUngrounded(markUngrounded(context.Background(), nil)), "not required")
	req := types.WithGroundingRequired(context.Background())
	require.True(t, isUngrounded(markUngrounded(req, nil)))
	require.False(t, isUngrounded(markUngrounded(req, stepWith(agenttools.ToolSearchKnowledge,
		&types.ToolResult{Success: true, Data: map[string]interface{}{"count": 1}}))))
}

// "iPhone 18 Pro chưa ra mắt" from memory never reaches the user.
func TestUngroundedRoundReplacesAnAnswerFromMemory(t *testing.T) {
	model := &scriptedChat{id: "ungrounded-test", replies: []*types.ChatResponse{
		{Content: "iPhone 18 Pro chưa ra mắt, mới nhất là iPhone 15.", FinishReason: "stop", AnswerStreamed: true, AnswerEventID: "a"},
	}}
	engine := newTestEngine(t, model)
	resp, err := engine.ungroundedRound(context.Background(), []chat.Message{{Role: "user", Content: "iPhone 18 Pro"}},
		&chat.ChatOptions{}, 1, "s1")
	require.NoError(t, err)
	require.Equal(t, ungroundedAnswer, resp.Content)
	require.False(t, resp.AnswerStreamed, "analyzeResponse must emit the replacement")
}

func TestUngroundedRoundKeepsAToolCall(t *testing.T) {
	call := types.LLMToolCall{ID: "c1", Type: "function", Function: types.FunctionCall{Name: "web_search", Arguments: "{}"}}
	model := &scriptedChat{id: "ungrounded-tool-test", replies: []*types.ChatResponse{
		{Content: "để tôi tìm", ToolCalls: []types.LLMToolCall{call}, FinishReason: "tool_calls"},
	}}
	engine := newTestEngine(t, model)
	resp, err := engine.ungroundedRound(context.Background(), []chat.Message{{Role: "user", Content: "x"}},
		&chat.ChatOptions{}, 1, "s1")
	require.NoError(t, err)
	require.Equal(t, []types.LLMToolCall{call}, resp.ToolCalls)
	require.Empty(t, resp.Content)
}

func TestUngroundedAnswerNamesARecordedRequest(t *testing.T) {
	require.Equal(t, ungroundedAnswer, ungroundedAnswerFor(context.Background()))
	require.NotContains(t, ungroundedAnswer, "tìm kiếm web")
	ctx := types.WithLegalRequestRecorded(context.Background(), "12/2024/QĐ-UBND")
	require.Equal(t, ungroundedAnswer+" Đã ghi nhận đề nghị bổ sung văn bản 12/2024/QĐ-UBND tới quản trị viên.",
		ungroundedAnswerFor(ctx))
}
