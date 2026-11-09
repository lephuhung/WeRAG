package chatpipeline

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// quickStep answers one quick-answer model call: refused, or chunks (the
// channel is closed after them).
type quickStep struct {
	err    error
	chunks []types.StreamResponse
}

type quickScriptChat struct {
	mu    sync.Mutex
	steps []quickStep
	calls int
}

func (m *quickScriptChat) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	return nil, nil
}

func (m *quickScriptChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.calls
	m.calls++
	if i >= len(m.steps) {
		return nil, errors.New("dial tcp 10.0.0.1:8000: connect: connection refused")
	}
	if m.steps[i].err != nil {
		return nil, m.steps[i].err
	}
	ch := make(chan types.StreamResponse, len(m.steps[i].chunks))
	for _, c := range m.steps[i].chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (m *quickScriptChat) GetModelName() string { return "mock" }
func (m *quickScriptChat) GetModelID() string   { return "mock" }

func (m *quickScriptChat) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// quickEvent is one recorded event: "answer", "done" or "error".
type quickEvent struct {
	kind    string
	content string
	code    string
}

func (b *syncEventBus) quickLog() []quickEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []quickEvent
	for _, evt := range b.events {
		switch d := evt.Data.(type) {
		case event.AgentFinalAnswerData:
			if d.Content != "" {
				out = append(out, quickEvent{kind: "answer", content: d.Content})
			}
			if d.Done {
				out = append(out, quickEvent{kind: "done"})
			}
		case event.ErrorData:
			out = append(out, quickEvent{kind: "error", content: d.Error, code: d.ErrorCode})
		}
	}
	return out
}

func runQuickAnswer(t *testing.T, model chat.Chat, withResults bool) (*syncEventBus, *PluginError) {
	t.Helper()
	bus := &syncEventBus{}
	cm := &types.ChatManage{}
	cm.SessionID = "sess-quick"
	cm.UserContent = "Điều 5 quy định gì?"
	cm.EventBus = bus
	if withResults {
		cm.MergeResult = []*types.SearchResult{{ID: "c1", Content: "nội dung"}}
	}
	plugin := &PluginChatCompletionStream{modelService: &stubModelService{model: model}}
	perr := plugin.OnEvent(context.Background(), types.CHAT_COMPLETION_STREAM, cm, func() *PluginError { return nil })
	return bus, perr
}

func waitDone(t *testing.T, bus *syncEventBus) []quickEvent {
	t.Helper()
	require.Eventually(t, func() bool {
		for _, e := range bus.quickLog() {
			if e.kind == "done" {
				return true
			}
		}
		return false
	}, 3*time.Second, 5*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	return bus.quickLog()
}

func kinds(log []quickEvent) string {
	var k []string
	for _, e := range log {
		if len(k) > 0 && k[len(k)-1] == e.kind && e.kind == "answer" {
			continue
		}
		k = append(k, e.kind)
	}
	return strings.Join(k, ",")
}

// A refused call: one quick retry, then the same notice the agent path
// gives, before the Done that persists the turn; no plugin error, so no raw
// error event follows.
func TestQuickAnswerRefusedGetsNoticeAfterOneRetry(t *testing.T) {
	model := &quickScriptChat{}
	bus, perr := runQuickAnswer(t, model, false)
	require.Nil(t, perr)
	require.Equal(t, 1+quickAnswerModelRetries, model.count())
	log := bus.quickLog()
	require.Equal(t, "error,done", kinds(log))
	require.Equal(t, types.TurnFailureModelUnavailable, log[0].code)
	require.Equal(t, types.ModelUnavailableMessage, log[0].content)
	require.NotContains(t, log[0].content, "10.0.0.1")
}

// With retrieval results the notice says they were found.
func TestQuickAnswerRefusedAfterSearch(t *testing.T) {
	model := &quickScriptChat{steps: []quickStep{{err: errors.New("API request failed with status 400: bad")}}}
	bus, perr := runQuickAnswer(t, model, true)
	require.Nil(t, perr)
	require.Equal(t, 1, model.count(), "a 400 is not retried")
	log := bus.quickLog()
	require.Equal(t, types.TurnFailureModelUnavailableAfterSearch, log[0].code)
}

// A transient refusal then a healthy stream: the answer once, no error.
func TestQuickAnswerTransientThenSuccess(t *testing.T) {
	model := &quickScriptChat{steps: []quickStep{
		{err: errors.New("API request failed with status 503: overloaded")},
		{chunks: []types.StreamResponse{{ResponseType: types.ResponseTypeAnswer, Content: "Trả lời.", Done: true,
			Usage: &types.TokenUsage{TotalTokens: 3}}}},
	}}
	bus, perr := runQuickAnswer(t, model, false)
	require.Nil(t, perr)
	log := waitDone(t, bus)
	require.Equal(t, 2, model.count())
	require.Equal(t, "answer,done", kinds(log))
	require.Equal(t, "Trả lời.", log[0].content)
}

// Mid-stream: the partial text is kept, the note is appended, the turn is
// marked interrupted, then the Done persists it. No retry.
func TestQuickAnswerMidStreamKeepsPartialAndNote(t *testing.T) {
	model := &quickScriptChat{steps: []quickStep{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "Điều 5 quy định "},
		{ResponseType: types.ResponseTypeError, Content: "read: connection reset by peer", Done: true},
	}}}}
	bus, perr := runQuickAnswer(t, model, false)
	require.Nil(t, perr)
	log := waitDone(t, bus)
	require.Equal(t, 1, model.count(), "no retry once text was shown")
	require.Equal(t, "answer,error,done", kinds(log))
	var text strings.Builder
	for _, e := range log {
		if e.kind == "answer" {
			text.WriteString(e.content)
		}
	}
	require.Equal(t, "Điều 5 quy định "+types.ModelInterruptedNote, text.String())
	require.Equal(t, types.TurnFailureModelInterrupted, log[len(log)-2].code)
}

// Broken before any output: one retry on a fresh stream.
func TestQuickAnswerBrokenBeforeOutputRetriesOnce(t *testing.T) {
	model := &quickScriptChat{steps: []quickStep{
		{chunks: []types.StreamResponse{{ResponseType: types.ResponseTypeError, Content: "context deadline exceeded (Client.Timeout)", Done: true}}},
		{chunks: []types.StreamResponse{{ResponseType: types.ResponseTypeAnswer, Content: "Trả lời.", Done: true,
			Usage: &types.TokenUsage{TotalTokens: 3}}}},
	}}
	bus, perr := runQuickAnswer(t, model, false)
	require.Nil(t, perr)
	log := waitDone(t, bus)
	require.Equal(t, 2, model.count())
	require.Equal(t, "answer,done", kinds(log))
}

// An empty completion is no answer: the notice precedes the Done.
func TestQuickAnswerEmptyCompletionGetsNotice(t *testing.T) {
	model := &quickScriptChat{steps: []quickStep{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Done: true, Usage: &types.TokenUsage{TotalTokens: 5}},
	}}}}
	bus, perr := runQuickAnswer(t, model, false)
	require.Nil(t, perr)
	log := waitDone(t, bus)
	require.Equal(t, "error,done", kinds(log))
	require.Equal(t, types.TurnFailureModelUnavailable, log[0].code)
}

// Failed turns stay out of the next turn's history.
func TestLoadAndProcessHistorySkipsFailedTurns(t *testing.T) {
	now := time.Now()
	failed := &types.Message{RequestID: "r2", Role: "assistant", Content: "Một phần" + types.ModelInterruptedNote,
		CreatedAt: now.Add(3 * time.Second)}
	failed.ExecutionContext.TurnFailure = types.TurnFailureModelInterrupted
	svc := &summaryMessages{rows: []*types.Message{
		{RequestID: "r1", Role: "user", Content: "câu 1", CreatedAt: now},
		{RequestID: "r1", Role: "assistant", Content: "trả lời 1", CreatedAt: now.Add(time.Second)},
		{RequestID: "r2", Role: "user", Content: "câu 2", CreatedAt: now.Add(2 * time.Second)},
		failed,
	}}
	history, err := loadAndProcessHistory(context.Background(), svc, "s", 5, 20)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, "câu 1", history[0].Query)
}
