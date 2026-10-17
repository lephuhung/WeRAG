package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func collectAnswer(t *testing.T, engine *AgentEngine) *string {
	t.Helper()
	var content string
	engine.eventBus.On(event.EventAgentFinalAnswer, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(event.AgentFinalAnswerData)
		require.True(t, ok)
		content += data.Content
		return nil
	})
	return &content
}

// The synthesis call once spent its whole budget reasoning (thinking left
// on) and answered nothing: thinking is off, and an empty answer is
// replaced by a code-built one.
func TestStreamFinalAnswerToEventBus_EmptySynthesisGetsFallback(t *testing.T) {
	mock := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeThinking, Content: "suy nghĩ rất dài…"},
		{ResponseType: types.ResponseTypeAnswer, Done: true, FinishReason: "length"},
	}}}}
	engine := newTestEngine(t, mock)
	engine.stopReason = event.TurnLimitTurnBudget
	content := collectAnswer(t, engine)
	state := &types.AgentState{RoundSteps: []types.AgentStep{
		{ToolCalls: []types.ToolCall{{Name: "check_legal_status", Result: &types.ToolResult{Success: true,
			Output: "## Luật Bảo vệ bí mật nhà nước 117/2025/QH15\n- Cơ quan ban hành: Quốc hội"}}}},
		{ToolCalls: []types.ToolCall{{Name: "read_document", Result: &types.ToolResult{Success: false, Error: "boom"}}}},
		{ToolCalls: []types.ToolCall{
			{Name: "search_knowledge", Result: &types.ToolResult{Success: true,
				Output: "<search_results>\n<result rank=\"1\">", Data: map[string]interface{}{"count": 10}}},
			{Name: "read_document", Result: &types.ToolResult{Success: true,
				Output: "Listed 17/41 chunks from Luật 117.html (content omitted from history)",
				Data:   map[string]interface{}{"knowledge_title": "Luật 117.html", "fetched_chunks": 17, "total_chunks": 41}}},
			{Name: "grep_chunks", Result: &types.ToolResult{Success: true, Output: "Keyword search found 3 matching chunks"}},
		}},
	}}

	err := engine.streamFinalAnswerToEventBus(context.Background(), "điểm mới của luật", state, "sess-1", emptyMessages())
	require.NoError(t, err)

	require.Len(t, mock.opts, 1)
	require.NotNil(t, mock.opts[0].Thinking, "the synthesis call must disable thinking explicitly")
	assert.False(t, *mock.opts[0].Thinking)

	assert.Contains(t, state.FinalAnswer, "đã dùng hết ngân sách xử lý sau 3 bước")
	assert.Contains(t, state.FinalAnswer, "- tra hiệu lực: Luật Bảo vệ bí mật nhà nước 117/2025/QH15")
	assert.Contains(t, state.FinalAnswer, "- tìm trong kho tri thức: 10 kết quả")
	assert.Contains(t, state.FinalAnswer, "- đọc «Luật 117.html» (17/41 đoạn)")
	assert.NotContains(t, state.FinalAnswer, "<", "tool markup never reaches the user")
	assert.NotContains(t, state.FinalAnswer, "Listed")
	assert.NotContains(t, state.FinalAnswer, "Keyword search", "English tool notes are skipped")
	assert.NotContains(t, state.FinalAnswer, "_", "tool identifiers are not shown")
	assert.Contains(t, state.FinalAnswer, "nêu đúng Điều cần xem", "a legal lookup asks for the Điều")
	assert.Equal(t, state.FinalAnswer, *content, "the fallback is streamed like an answer")
}

func TestHandleMaxIterations_FailedSynthesisGetsVietnameseFallback(t *testing.T) {
	engine := newTestEngine(t, &mockChat{}) // no response prepared: the call fails
	state := &types.AgentState{}
	engine.handleMaxIterations(context.Background(), "q", state, "sess-1", emptyMessages())
	assert.True(t, state.IsComplete)
	assert.True(t, strings.HasPrefix(state.FinalAnswer, "Lượt này đã dùng hết ngân sách xử lý sau 0 bước"), state.FinalAnswer)
	assert.NotContains(t, state.FinalAnswer, "Sorry")
	assert.Contains(t, state.FinalAnswer, "chia yêu cầu thành nhiều lượt", "no legal tool ran: no Điều wording")
}

func TestAppendSourceEditNote(t *testing.T) {
	engine := newTestEngine(t, &mockChat{})
	content := collectAnswer(t, engine)
	engine.SetSourceEditNote(func(_ context.Context, query string) string {
		if strings.Contains(query, "Viết lại") {
			return "Lưu ý: ke_hoach_nguon.docx là tài liệu nguồn (chỉ tra cứu)."
		}
		return ""
	})
	state := &types.AgentState{FinalAnswer: "Đề xuất: …"}
	engine.appendSourceEditNote(context.Background(), state, "sess-1", "answer-1", "Viết lại mục 2")
	assert.Equal(t, "Đề xuất: …\n\nLưu ý: ke_hoach_nguon.docx là tài liệu nguồn (chỉ tra cứu).\n", state.FinalAnswer)
	assert.Equal(t, "\n\nLưu ý: ke_hoach_nguon.docx là tài liệu nguồn (chỉ tra cứu).\n", *content)

	// an edit tool that succeeded landed on a target: no note
	state = &types.AgentState{FinalAnswer: "Đã đề xuất.", RoundSteps: []types.AgentStep{{ToolCalls: []types.ToolCall{
		{Name: "rewrite_paragraphs", Result: &types.ToolResult{Success: true}}}}}}
	engine.appendSourceEditNote(context.Background(), state, "sess-1", "answer-1", "Viết lại mục 2")
	assert.Equal(t, "Đã đề xuất.", state.FinalAnswer)
}
