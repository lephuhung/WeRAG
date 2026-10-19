package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/compaction"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// catalogue of the tests: 59/2020/QH14 and 168/2025/NĐ-CP exist
func testCatalog(_ context.Context, key string) (bool, error) {
	switch key {
	case "59/2020/QH14", "168/2025/ND-CP":
		return true, nil
	case "99/2099/QH99":
		return false, errors.New("db down")
	}
	return false, nil
}

func numberCtx(t *testing.T, steps []types.AgentStep, query string) context.Context {
	t.Helper()
	e := &AgentEngine{}
	e.SetNumberCatalog(testCatalog)
	return e.withNumberCheck(context.Background(), &types.AgentState{RoundSteps: steps}, query, nil)
}

func kbStep(text string) types.AgentStep {
	return types.AgentStep{ToolCalls: []types.ToolCall{{Name: "search_knowledge",
		Result: &types.ToolResult{Success: true, Output: "…", Data: map[string]interface{}{
			"results": []interface{}{map[string]interface{}{"content": text}}}}}}}
}

const answerWithNumbers = "Theo Luật 59/2020/QH14 và Nghị định 132/2023/NĐ-CP, công ty phải …\n" +
	"- Quyết định 123/QĐ-UBND của tỉnh (trong KB) vẫn áp dụng.\n" +
	"- Nghị định số 168/2025/NĐ–CP thay thế.\n" +
	"Thông tư 45/2024/TT-BTC hướng dẫn; xem thêm số 7/2021/QH15. Ngày 15/11/2018 không đổi.\n" +
	"**Nghị định 132/2023/NĐ-CP** nhắc lại."

const answerCleaned = "Theo Luật 59/2020/QH14 và một nghị định " + RemovedNumberNote + ", công ty phải …\n" +
	"- Quyết định 123/QĐ-UBND của tỉnh (trong KB) vẫn áp dụng.\n" +
	"- Nghị định số 168/2025/NĐ–CP thay thế.\n" +
	"Một thông tư " + RemovedNumberNote + " hướng dẫn; xem thêm " + RemovedNumberNote + ". Ngày 15/11/2018 không đổi.\n" +
	"**Một nghị định " + RemovedNumberNote + "** nhắc lại."

func TestCleanAnswerNumbers(t *testing.T) {
	ctx := numberCtx(t, []types.AgentStep{kbStep("Quyết định 123/QĐ-UBND ngày …")}, "")
	assert.Equal(t, answerCleaned, cleanAnswerNumbers(ctx, answerWithNumbers))
}

func TestCleanAnswerNumbersSources(t *testing.T) {
	// the user's own question and a tool output are evidence
	ctx := numberCtx(t, nil, "Nghị định 132/2023/NĐ-CP còn hiệu lực không?")
	assert.Equal(t, "Nghị định 132/2023/NĐ-CP …", cleanAnswerNumbers(ctx, "Nghị định 132/2023/NĐ-CP …"))
	// a pack resource (read_file) is not
	ctx = numberCtx(t, []types.AgentStep{{ToolCalls: []types.ToolCall{{Name: "read_file",
		Result: &types.ToolResult{Success: true, Output: "Nghị định 132/2023/NĐ-CP"}}}}}, "")
	assert.Equal(t, "Một nghị định "+RemovedNumberNote+".", cleanAnswerNumbers(ctx, "Nghị định 132/2023/NĐ-CP."))
	// a failed lookup is not shown
	ctx = numberCtx(t, nil, "")
	assert.Equal(t, "Theo một luật "+RemovedNumberNote+".", cleanAnswerNumbers(ctx, "Theo Luật 99/2099/QH99."))
	// off: untouched
	assert.Equal(t, "Luật 1/2099/QH1", cleanAnswerNumbers(context.Background(), "Luật 1/2099/QH1"))
}

// Every way of cutting the answer into chunks streams the same text as the
// cleaning of the whole answer, and no removed number is ever emitted, even
// in part.
func TestNumberGuardStreamsLikeTheCleanedAnswer(t *testing.T) {
	ctx := numberCtx(t, []types.AgentStep{kbStep("Quyết định 123/QĐ-UBND")}, "")
	for _, size := range []int{1, 2, 3, 5, 7, 11, 64} {
		var out strings.Builder
		var emitted []string
		g := newNumberGuard(ctx, func(s string) { out.WriteString(s); emitted = append(emitted, s) })
		require.NotNil(t, g)
		b := []byte(answerWithNumbers)
		for i := 0; i < len(b); i += size {
			end := i + size
			if end > len(b) {
				end = len(b)
			}
			g.Write(string(b[i:end])) // may split a UTF-8 rune
		}
		g.Flush()
		assert.Equal(t, answerCleaned, out.String(), "chunk size %d", size)
		for _, s := range emitted {
			for _, banned := range []string{"132/", "45/2024", "7/2021", "/2023/N"} {
				assert.NotContains(t, s, banned, "chunk size %d leaked %q in %q", size, banned, s)
			}
		}
	}
}

func TestSafeCutHoldsOnlyWhatMayStillChange(t *testing.T) {
	for in, want := range map[string]string{
		"Theo Luật":            "Theo ",
		"Theo Nghị đ":          "Theo ",
		"Theo Nghị định số ":   "Theo ",
		"Theo Nghị định 13":    "Theo ",
		"văn 132/2023/NĐ-":     "văn ",
		"Xin chào bạn.":        "Xin chào bạn.",
		"Doanh nghiệp tư nhân": "Doanh nghiệp tư nhân",
		"Theo bộ":              "Theo ",
	} {
		assert.Equal(t, want, in[:safeCut(in)], "%q", in)
	}
	// an incomplete UTF-8 rune at the end
	s := "Xin chào " + string([]byte("đ")[:1])
	assert.Equal(t, "Xin chào ", s[:safeCut(s)])
}

func TestNumberCheckOnBothAnswerPaths(t *testing.T) {
	chunks := []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "Theo Nghị định 13"},
		{ResponseType: types.ResponseTypeAnswer, Content: "2/2023/NĐ"},
		{ResponseType: types.ResponseTypeAnswer, Content: "-CP và Luật 59/2020/QH14."},
	}
	want := "Theo một nghị định " + RemovedNumberNote + " và Luật 59/2020/QH14."

	t.Run("natural stop", func(t *testing.T) {
		last := chunks[len(chunks)-1]
		last.Done, last.FinishReason = true, "stop"
		model := &mockChat{responses: []mockResponse{{chunks: append(append([]types.StreamResponse{}, chunks[:2]...), last)}}}
		engine := newTestEngine(t, model)
		engine.toolRegistry = agentRegistryForTest(t, "counting_tool")
		engine.SetNumberCatalog(testCatalog)
		events := recordAnswerEvents(t, engine)
		state := &types.AgentState{}
		messages := []chat.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "hỏi"}}
		_, err := engine.executeLoop(context.Background(), state, "hỏi", messages, engine.buildToolsForLLM(), "sess", "msg")
		require.NoError(t, err)
		assert.Equal(t, want, state.FinalAnswer)
		var streamed strings.Builder
		for _, e := range events() {
			streamed.WriteString(e.content)
		}
		assert.Equal(t, want, streamed.String(), "the stream equals the stored answer")
	})
	t.Run("synthesis", func(t *testing.T) {
		model := &mockChat{responses: []mockResponse{{chunks: append(append([]types.StreamResponse{}, chunks...),
			types.StreamResponse{ResponseType: types.ResponseTypeAnswer, Done: true, FinishReason: "stop"})}}}
		engine := newTestEngine(t, model)
		engine.SetNumberCatalog(testCatalog)
		events := recordAnswerEvents(t, engine)
		state := &types.AgentState{}
		require.NoError(t, engine.streamFinalAnswerToEventBus(context.Background(), "q", state, "sess", emptyMessages()))
		assert.Equal(t, want, state.FinalAnswer)
		var streamed strings.Builder
		for _, e := range events() {
			streamed.WriteString(e.content)
		}
		assert.Equal(t, want, streamed.String())
	})
	t.Run("code-appended blocks are not checked", func(t *testing.T) {
		model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
			{ResponseType: types.ResponseTypeAnswer, Content: "Kết luận.", Done: true, FinishReason: "stop"},
		}}}}
		engine := newTestEngine(t, model)
		engine.toolRegistry = agentRegistryForTest(t, "counting_tool")
		engine.SetNumberCatalog(testCatalog)
		engine.SetDomainFooter(func(context.Context, string) string { return "\n\n---\nXem 132/2023/NĐ-CP.\n" })
		state := &types.AgentState{}
		messages := []chat.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "hỏi"}}
		_, err := engine.executeLoop(context.Background(), state, "hỏi", messages, engine.buildToolsForLLM(), "sess", "msg")
		require.NoError(t, err)
		assert.Equal(t, "Kết luận.\n\n---\nXem 132/2023/NĐ-CP.\n", state.FinalAnswer)
	})
}

// Turn 2 answers from history: a tenant's own number found in turn 1 (an
// earlier answer and tool result) is evidence; the system prompt is not.
func TestNumberCheckUsesConversationHistory(t *testing.T) {
	e := &AgentEngine{}
	e.SetNumberCatalog(testCatalog)
	history := []chat.Message{
		{Role: "system", Content: "Gói: Nghị định 777/2025/NĐ-CP"},
		{Role: "user", Content: "Quyết định của tỉnh về phí là gì?"},
		{Role: "tool", Content: "… Quyết định 123/QĐ-UBND ngày 2/3/2025 …"},
		{Role: "assistant", Content: "Đó là Quyết định 456/QĐ-UBND." + "\n\n---\n**Lưu ý về cẩm nang chuyên môn**\nXem 888/2025/NĐ-CP"},
		{Role: "user", Content: "Văn bản đó còn hiệu lực không?"},
	}
	ctx := e.withNumberCheck(context.Background(), &types.AgentState{}, "Văn bản đó còn hiệu lực không?", history)
	assert.Equal(t, "Quyết định 123/QĐ-UBND và Quyết định 456/QĐ-UBND còn áp dụng.",
		cleanAnswerNumbers(ctx, "Quyết định 123/QĐ-UBND và Quyết định 456/QĐ-UBND còn áp dụng."))
	assert.Equal(t, "Một nghị định "+RemovedNumberNote+"; một nghị định "+RemovedNumberNote+".",
		cleanAnswerNumbers(ctx, "Nghị định 777/2025/NĐ-CP; nghị định 888/2025/NĐ-CP."),
		"the system prompt and a code-appended footer are not evidence")
}

// Data of any shape counts: check_legal_status reports are typed structs.
func TestNumberCheckReadsTypedToolData(t *testing.T) {
	type report struct {
		Number string `json:"number"`
	}
	step := types.AgentStep{ToolCalls: []types.ToolCall{{Name: "check_legal_status", Result: &types.ToolResult{
		Success: true, Output: "…", Data: map[string]interface{}{"reports": []report{{Number: "76/2025/QH15"}}}}}}}
	ctx := numberCtx(t, []types.AgentStep{step}, "")
	assert.Equal(t, "Luật 76/2025/QH15 sửa đổi.", cleanAnswerNumbers(ctx, "Luật 76/2025/QH15 sửa đổi."))
}

func TestLookupErrorPolicy(t *testing.T) {
	ctx := numberCtx(t, nil, "")
	got := cleanAnswerNumbers(ctx, "Luật 99/2099/QH99.")
	if RemoveNumberOnLookupError {
		assert.Equal(t, "Một luật "+RemovedNumberNote+".", got)
	} else {
		assert.Equal(t, "Luật 99/2099/QH99.", got)
	}
}

// A read_file result and a compaction summary are not evidence.
func TestNumberCheckHistoryExclusions(t *testing.T) {
	e := &AgentEngine{}
	e.SetNumberCatalog(testCatalog)
	history := []chat.Message{
		compaction.SummaryMessage("Người dùng hỏi về Nghị định 555/2025/NĐ-CP."),
		{Role: "assistant", ToolCalls: []chat.ToolCall{{ID: "c1", Function: chat.FunctionCall{Name: "read_file"}}}},
		{Role: "tool", ToolCallID: "c1", Content: "Bình luận: Nghị định 666/2025/NĐ-CP"},
		{Role: "tool", Name: "read_file", Content: "Nghị định 444/2025/NĐ-CP"},
		{Role: "assistant", ToolCalls: []chat.ToolCall{{ID: "c2", Function: chat.FunctionCall{Name: "search_knowledge"}}}},
		{Role: "tool", ToolCallID: "c2", Content: "Quyết định 123/QĐ-UBND"},
	}
	ctx := e.withNumberCheck(context.Background(), &types.AgentState{}, "", history)
	assert.Equal(t, "Một nghị định "+RemovedNumberNote+", một nghị định "+RemovedNumberNote+
		", một nghị định "+RemovedNumberNote+" và Quyết định 123/QĐ-UBND.",
		cleanAnswerNumbers(ctx, "Nghị định 555/2025/NĐ-CP, nghị định 666/2025/NĐ-CP, nghị định 444/2025/NĐ-CP và Quyết định 123/QĐ-UBND."))
}
