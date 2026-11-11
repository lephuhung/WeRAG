package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

func viCtx() context.Context {
	return context.WithValue(context.Background(), types.LanguageContextKey, "vi-VN")
}

// The shapes seen in real turns, reworded: the plan written as the answer,
// a plan paragraph before the Vietnamese answer, a translation request.
const (
	englishPlan = "The user wants to shorten section 3 of `bao_cao.docx`.\nFirst, I need to read the document to find the section they mean."
	leadIn      = "Now I have all the figures from both documents. Let me compile the answer for the user."
	viAnswer    = "Theo **Nghị định 30/2020/NĐ-CP**, văn bản hành chính phải trình bày đúng thể thức và kỹ thuật quy định tại Phụ lục I."
)

func TestJudgeAnswer(t *testing.T) {
	cases := []struct {
		name, query, answer string
		want                degenerateKind
	}{
		{"echo of a code", "VP", "VP", answerEcho},
		{"echo with a question mark", "VP", "VP?", answerEcho},
		{"echo in other case", "tthc", "TTHC.", answerEcho},
		{"only dots", "TTHC", "...", answerBlank},
		{"only spaces", "TTHC", "   ", answerBlank},
		{"English plan", "Rút gọn mục 3", englishPlan, answerEnglish},
		{"Vietnamese answer", "Thể thức văn bản?", viAnswer, answerOK},
		{"short Vietnamese answer", "VP", "VP là viết tắt của Văn phòng hay Vi phạm?", answerOK},
		{"greeting mirrored", "Xin chào", "Xin chào!", answerOK},
		{"greeting answered", "hi", "Xin chào! Tôi có thể giúp gì cho bạn?", answerOK},
		{"English question", "What is the fine for not wearing a helmet?", englishPlan, answerOK},
		{"English asked for", "Dịch đoạn này sang tiếng Anh", "The agency shall send the report to the Ministry before the end of the month.", answerOK},
		{"code only", "Viết câu SQL lấy người dùng đang hoạt động", "```sql\nSELECT name FROM users WHERE id IN (1, 2) AND active IS NOT NULL\n```", answerOK},
		{"English clause quoted", "Điều khoản này nghĩa là gì?", "> The Seller shall deliver the goods to the Buyer within thirty days of the order.\n\n" + viAnswer, answerOK},
		{"bilingual notice", "Soạn thông báo song ngữ", "The office will be closed on Monday for the holiday and will open again on Tuesday.\n\n" + viAnswer, answerOK},
		{"English list answer", "Rút gọn mục 3", "1. The user should read the document first and then check it for me.", answerOK},
		{"English lead-in then Vietnamese", "So sánh hai văn bản", leadIn + "\n\n---\n\n" + viAnswer, answerOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, got := cleanFinalAnswer(viCtx(), c.query, c.answer)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestEnglishIsNotJudgedForAnEnglishUI(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.LanguageContextKey, "en-US")
	_, got := cleanFinalAnswer(ctx, "Rút gọn mục 3", englishPlan)
	assert.Equal(t, answerOK, got)
}

func TestStripEnglishLeadIn(t *testing.T) {
	rest, dropped, all := stripEnglishLeadIn(leadIn+"\n\n---\n\n"+viAnswer, true)
	assert.True(t, dropped)
	assert.False(t, all)
	assert.Equal(t, viAnswer, rest)

	// a Vietnamese answer quoting English later keeps everything
	text := viAnswer + "\n\n" + leadIn
	rest, dropped, _ = stripEnglishLeadIn(text, true)
	assert.False(t, dropped)
	assert.Equal(t, text, rest)

	// code names and file names are not English
	assert.False(t, englishText("Mở tệp `ke_hoach_nguon.docx` và `bao_cao_dai.docx` để đối chiếu số liệu"))
}

// streamThrough writes text to a lead guard in chunks of n bytes (rune
// boundaries kept) and returns what reached the client.
func streamThrough(t *testing.T, query, text string, n int) (string, *leadGuard) {
	t.Helper()
	var out strings.Builder
	g := newLeadGuard(viCtx(), query, func(s string) { out.WriteString(s) })
	for len(text) > 0 {
		cut := n
		if cut > len(text) {
			cut = len(text)
		}
		for cut < len(text) && !utf8RuneStart(text[cut]) {
			cut++
		}
		g.Write(text[:cut])
		text = text[cut:]
	}
	g.Flush()
	return out.String(), g
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

// The stream must show exactly the answer the turn saves.
func TestLeadGuardStreamMatchesSavedAnswer(t *testing.T) {
	long := viAnswer + "\n\n" + strings.Repeat("Cơ quan ban hành phải ghi rõ số, ký hiệu và ngày tháng năm. ", 8)
	cases := []struct{ query, answer string }{
		{"VP", "VP"},
		{"TTHC", "..."},
		{"Rút gọn mục 3", englishPlan},
		{"So sánh hai văn bản", leadIn + "\n\n---\n\n" + long},
		{"So sánh hai văn bản", leadIn + "\n\n" + leadIn + "\n\n" + viAnswer},
		{"Thể thức văn bản?", long},
		{"Dịch đoạn này sang tiếng Anh", englishPlan},
		{"Thể thức văn bản?", "\n\n" + viAnswer},
		{"Điều khoản này nghĩa là gì?", "> The Seller shall deliver the goods to the Buyer within thirty days of the order.\n\n" + viAnswer},
		{"Soạn thông báo song ngữ", "The office will be closed on Monday for the holiday and will open again on Tuesday.\n\n" + viAnswer},
		{"Viết câu SQL", "```sql\nSELECT name FROM users WHERE id IN (1, 2) AND active IS NOT NULL\n```"},
	}
	for _, c := range cases {
		saved, kind := cleanFinalAnswer(viCtx(), c.query, c.answer)
		for _, n := range []int{1, 7, 64, 4096} {
			shown, g := streamThrough(t, c.query, c.answer, n)
			assert.Equal(t, kind, g.Kind(), "%q chunk %d", c.query, n)
			if kind == answerOK {
				assert.Equal(t, saved, shown, "%q chunk %d", c.query, n)
			} else {
				assert.Empty(t, shown, "%q chunk %d", c.query, n)
			}
		}
	}
}

// A normal answer is released after leadJudgeRunes, not held to the end.
func TestLeadGuardReleasesEarly(t *testing.T) {
	var out strings.Builder
	g := newLeadGuard(viCtx(), "Thể thức văn bản?", func(s string) { out.WriteString(s) })
	g.Write(strings.Repeat("Văn bản hành chính phải ghi rõ thể thức. ", 6))
	assert.NotEmpty(t, out.String())
}

// Text before a tool call goes out as it did without the check.
func TestLeadGuardReleaseSendsAPreamble(t *testing.T) {
	var out strings.Builder
	g := newLeadGuard(viCtx(), "VP", func(s string) { out.WriteString(s) })
	g.Write("VP")
	g.Release()
	g.Flush()
	assert.Equal(t, "VP", out.String())
	assert.Equal(t, answerOK, g.Kind())
}

func TestExecuteLoop_EchoIsRerunWithThinking(t *testing.T) {
	mock := &mockChat{responses: []mockResponse{
		{chunks: []types.StreamResponse{{Content: "VP", Done: true}}},
		{chunks: []types.StreamResponse{{Content: "Bạn muốn hỏi gì về “VP”: Văn phòng hay Vi phạm?", Done: true}}},
	}}
	engine := newTestEngine(t, mock)
	recorder := &terminalAnswerRecorder{}
	recorder.attach(engine.eventBus)

	state := &types.AgentState{}
	_, err := engine.executeLoop(viCtx(), state, "VP", emptyMessages(), emptyTools(), "sess-1", "msg-1")
	require.NoError(t, err)

	assert.Equal(t, "Bạn muốn hỏi gì về “VP”: Văn phòng hay Vi phạm?", state.FinalAnswer)
	require.Len(t, mock.opts, 2)
	require.NotNil(t, mock.opts[1].Thinking)
	assert.True(t, *mock.opts[1].Thinking, "the rerun has thinking on")
	done, content := recorder.snapshot()
	assert.Equal(t, 1, done)
	assert.Equal(t, "Bạn muốn hỏi gì về “VP”: Văn phòng hay Vi phạm?", content, "the echo never reached the client")
}

func TestExecuteLoop_SecondEchoGetsTheFixedSentence(t *testing.T) {
	mock := &mockChat{responses: []mockResponse{
		{chunks: []types.StreamResponse{{Content: "VP", Done: true}}},
		{chunks: []types.StreamResponse{{Content: "VP?", Done: true}}},
	}}
	engine := newTestEngine(t, mock)
	recorder := &terminalAnswerRecorder{}
	recorder.attach(engine.eventBus)

	state := &types.AgentState{}
	_, err := engine.executeLoop(viCtx(), state, "VP", emptyMessages(), emptyTools(), "sess-1", "msg-1")
	require.NoError(t, err)

	assert.True(t, state.IsComplete)
	assert.Equal(t, degenerateFallback("VP", answerEcho), state.FinalAnswer)
	assert.Equal(t, 2, mock.callCount, "one rerun only")
	done, content := recorder.snapshot()
	assert.Equal(t, 1, done)
	assert.Equal(t, state.FinalAnswer, content)
}

// A blank answer twice is no answer: it ends as a model failure, like an
// empty one, instead of a fixed sentence kept in history.
func TestExecuteLoop_SecondBlankEndsAsModelFailure(t *testing.T) {
	mock := &mockChat{responses: []mockResponse{
		{chunks: []types.StreamResponse{{Content: "...", Done: true}}},
		{chunks: []types.StreamResponse{{Content: "  ...", Done: true}}},
	}}
	engine := newTestEngine(t, mock)
	recorder := &terminalAnswerRecorder{}
	recorder.attach(engine.eventBus)

	state := &types.AgentState{}
	_, err := engine.executeLoop(viCtx(), state, "Thủ tục hành chính?", emptyMessages(), emptyTools(), "sess-1", "msg-1")
	require.NoError(t, err)

	assert.True(t, state.IsComplete)
	assert.Empty(t, state.FinalAnswer)
	assert.Equal(t, types.TurnFailureModelUnavailable, engine.turnFailure)
	assert.Equal(t, 2, mock.callCount, "one rerun only")
	_, content := recorder.snapshot()
	assert.Empty(t, content, "nothing blank reached the client")
}

// The synthesis gets the lead check too: an English plan is never shown or
// saved; the code-built summary of what was gathered answers instead.
func TestSynthesisEnglishPlanIsHeldBack(t *testing.T) {
	mock := &mockChat{responses: []mockResponse{
		{chunks: []types.StreamResponse{{Content: leadIn, Done: true}}},
	}}
	engine := newTestEngine(t, mock)
	recorder := &terminalAnswerRecorder{}
	recorder.attach(engine.eventBus)
	state := &types.AgentState{RoundSteps: []types.AgentStep{{ToolCalls: []types.ToolCall{{
		Name: "counting_tool", Result: &types.ToolResult{Success: true, Output: "kết quả"}}}}}}

	// the loop ctx carries no withAnswerCheckQuery: the question comes as
	// the parameter, as in production
	engine.handleMaxIterations(viCtx(), "Thể thức văn bản?", state, "sess-1", emptyMessages())

	assert.NotContains(t, state.FinalAnswer, "Let me compile")
	assert.NotEmpty(t, state.FinalAnswer)
	_, content := recorder.snapshot()
	assert.NotContains(t, content, "Let me compile", "the plan never reached the client")
}

// An English question on a Vietnamese UI: the English synthesis at the cap is
// the answer, not a plan to hold back.
func TestSynthesisInEnglishForAnEnglishQuestionIsKept(t *testing.T) {
	answer := "The decree requires administrative documents to follow the format set out in Appendix I."
	mock := &mockChat{responses: []mockResponse{
		{chunks: []types.StreamResponse{{Content: answer, Done: true}}},
	}}
	engine := newTestEngine(t, mock)
	recorder := &terminalAnswerRecorder{}
	recorder.attach(engine.eventBus)
	state := &types.AgentState{RoundSteps: []types.AgentStep{{ToolCalls: []types.ToolCall{{
		Name: "counting_tool", Result: &types.ToolResult{Success: true, Output: "kết quả"}}}}}}

	engine.handleMaxIterations(viCtx(), "What format must administrative documents follow?", state, "sess-1", emptyMessages())

	assert.True(t, strings.HasPrefix(state.FinalAnswer, answer), state.FinalAnswer)
	_, content := recorder.snapshot()
	assert.Contains(t, content, answer)
}

func TestExecuteLoop_EnglishLeadInIsDroppedFromTheSavedAnswer(t *testing.T) {
	mock := &mockChat{responses: []mockResponse{
		{chunks: []types.StreamResponse{{Content: leadIn + "\n\n---\n\n"}, {Content: viAnswer, Done: true}}},
	}}
	engine := newTestEngine(t, mock)
	recorder := &terminalAnswerRecorder{}
	recorder.attach(engine.eventBus)

	state := &types.AgentState{}
	_, err := engine.executeLoop(viCtx(), state, "So sánh hai văn bản", emptyMessages(), emptyTools(), "sess-1", "msg-1")
	require.NoError(t, err)

	assert.Equal(t, viAnswer, state.FinalAnswer)
	assert.Equal(t, 1, mock.callCount)
	_, content := recorder.snapshot()
	assert.Equal(t, viAnswer, content)
}

func TestQuotedEnglishClauseIsKept(t *testing.T) {
	text := "> The Seller shall deliver the goods to the Buyer within thirty days of the order.\n\n" + viAnswer
	rest, _ := cleanFinalAnswer(viCtx(), "Điều khoản này nghĩa là gì?", text)
	assert.Equal(t, text, rest)
}

// A text that went out as a tool-call preamble is saved as it was sent.
func TestPreambleIsNotCleaned(t *testing.T) {
	g := newLeadGuard(viCtx(), "So sánh", func(string) {})
	g.Write(leadIn + "\n\n")
	g.Release()
	assert.True(t, g.Preamble())
}

// The first text sent drops the stray line breaks Qwen opens with.
func TestLeadGuardTrimsLeadingBreaks(t *testing.T) {
	shown, _ := streamThrough(t, "Thể thức văn bản?", "\n\n"+viAnswer, 5)
	assert.Equal(t, viAnswer, shown)
}

func TestDegenerateFallbackSkipsMarkdownQueries(t *testing.T) {
	assert.Contains(t, degenerateFallback("VP", answerEcho), "“VP”")
	assert.NotContains(t, degenerateFallback("*VP*", answerEcho), "*VP*")
}

func TestExecuteLoop_ThinkingRetryLastsOneRound(t *testing.T) {
	mock := &mockChat{responses: []mockResponse{
		{chunks: []types.StreamResponse{{Content: "VP", Done: true}}},
		{chunks: []types.StreamResponse{{Content: "Bạn muốn hỏi gì về “VP”?", Done: true}}},
	}}
	engine := newTestEngine(t, mock)
	state := &types.AgentState{}
	_, err := engine.executeLoop(viCtx(), state, "VP", emptyMessages(), emptyTools(), "sess-1", "msg-1")
	require.NoError(t, err)
	assert.True(t, engine.roundThinking, "the rerun round runs with thinking")
	assert.False(t, engine.thinkingRetry, "nothing is left for a later round")
}
