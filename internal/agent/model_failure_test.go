package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// scriptStep is one model call of failingChat: a refused call (startErr) or
// a stream of chunks, which may end on an error chunk.
type scriptStep struct {
	startErr error
	chunks   []types.StreamResponse
	// onCall runs before the call is answered (a user stop, say).
	onCall func()
}

// failingChat answers model calls from a script; a call past the script is
// refused like a dead endpoint.
type failingChat struct {
	mu    sync.Mutex
	steps []scriptStep
	calls int
}

func (m *failingChat) ChatStream(ctx context.Context, _ []chat.Message, _ *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	m.mu.Lock()
	i := m.calls
	m.calls++
	m.mu.Unlock()
	if i >= len(m.steps) {
		return nil, errors.New(`send request: dial tcp 10.0.0.1:8000: connect: connection refused`)
	}
	st := m.steps[i]
	if st.onCall != nil {
		st.onCall()
	}
	if st.startErr != nil {
		return nil, st.startErr
	}
	ch := make(chan types.StreamResponse, len(st.chunks))
	for _, c := range st.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (m *failingChat) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	return nil, errors.New("not used")
}
func (m *failingChat) GetModelName() string { return "failing-model" }
func (m *failingChat) GetModelID() string   { return "failing-id" }

func (m *failingChat) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// busLog records the turn's answer, error and completion events in order.
type busLog struct {
	mu     sync.Mutex
	kinds  []string
	errs   []event.ErrorData
	answer strings.Builder
	done   int
	final  *event.AgentCompleteData
}

func (l *busLog) attach(bus *event.EventBus) {
	bus.On(event.EventAgentFinalAnswer, func(_ context.Context, evt event.Event) error {
		d := evt.Data.(event.AgentFinalAnswerData)
		l.mu.Lock()
		defer l.mu.Unlock()
		l.answer.WriteString(d.Content)
		if d.Done {
			l.done++
			l.kinds = append(l.kinds, "answer_done")
		} else {
			l.kinds = append(l.kinds, "answer")
		}
		return nil
	})
	bus.On(event.EventError, func(_ context.Context, evt event.Event) error {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.errs = append(l.errs, evt.Data.(event.ErrorData))
		l.kinds = append(l.kinds, "error")
		return nil
	})
	bus.On(event.EventAgentComplete, func(_ context.Context, evt event.Event) error {
		d := evt.Data.(event.AgentCompleteData)
		l.mu.Lock()
		defer l.mu.Unlock()
		l.final = &d
		l.kinds = append(l.kinds, "complete")
		return nil
	})
}

// tail is the order of the last kinds, collapsing repeated answer chunks.
func (l *busLog) order() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, k := range l.kinds {
		if len(out) > 0 && out[len(out)-1] == k && k == "answer" {
			continue
		}
		out = append(out, k)
	}
	return out
}

func refused() error {
	return errors.New(`send request: Post "http://10.0.0.1:8000/v1/chat/completions": dial tcp 10.0.0.1:8000: connect: connection refused`)
}

func status400() error {
	return errors.New(`API request failed with status 400: {"error":"max_tokens is too large"}`)
}

func toolCallStep() scriptStep {
	return scriptStep{chunks: []types.StreamResponse{{
		ResponseType: types.ResponseTypeAnswer,
		ToolCalls: []types.LLMToolCall{{ID: "tc1", Type: "function",
			Function: types.FunctionCall{Name: "counting_tool", Arguments: "{}"}}},
		Done: true, FinishReason: "tool_calls",
	}}}
}

func failureEngine(t *testing.T, model chat.Chat) (*AgentEngine, *busLog) {
	t.Helper()
	engine := newTestEngine(t, model)
	engine.toolRegistry = agentRegistryForTest(t, "counting_tool")
	log := &busLog{}
	log.attach(engine.eventBus)
	return engine, log
}

func execute(t *testing.T, engine *AgentEngine) (*types.AgentState, error) {
	t.Helper()
	return engine.Execute(context.Background(), "sess-1", "msg-1", "Điều 5 Luật Đất đai quy định gì?", nil)
}

// noRawCause asserts the user-facing event text carries no provider detail.
func noRawCause(t *testing.T, d event.ErrorData) {
	t.Helper()
	for _, leak := range []string{"10.0.0.1", "http", "status", "refused", "max_tokens", "dial"} {
		assert.NotContains(t, d.Error, leak)
	}
}

// Round 1, endpoint dead: the existing transient retry runs (2 retries), then
// the notice goes out as an error event BEFORE the completion event, and
// Execute returns no error, so no raw error event follows the completion.
func TestModelFailure_Round1_RefusedAfterRetries(t *testing.T) {
	model := &failingChat{steps: []scriptStep{{startErr: refused()}, {startErr: refused()}, {startErr: refused()}}}
	engine, log := failureEngine(t, model)

	state, err := execute(t, engine)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, 1+maxLLMRetries, model.callCount(), "the existing retry is kept")

	assert.Equal(t, []string{"error", "complete"}, log.order())
	require.Len(t, log.errs, 1)
	assert.Equal(t, types.TurnFailureModelUnavailable, log.errs[0].ErrorCode)
	assert.Equal(t, types.ModelUnavailableMessage, log.errs[0].Error)
	noRawCause(t, log.errs[0])
	require.NotNil(t, log.final)
	assert.Empty(t, log.final.FinalAnswer, "no invented answer")
	assert.Zero(t, log.done)
	assert.Error(t, engine.turnFailureCause, "the raw cause is kept for the span")
}

// A 4xx is the request's fault: never retried.
func TestModelFailure_Round1_ClientErrorNotRetried(t *testing.T) {
	model := &failingChat{steps: []scriptStep{{startErr: status400()}}}
	engine, log := failureEngine(t, model)

	_, err := execute(t, engine)
	require.NoError(t, err)
	assert.Equal(t, 1, model.callCount(), "a 400 is not retried")
	assert.Equal(t, []string{"error", "complete"}, log.order())
	assert.Equal(t, types.TurnFailureModelUnavailable, log.errs[0].ErrorCode)
	noRawCause(t, log.errs[0])
}

// After a tool round: the synthesis is tried (it fails too), the notice says
// results were found, and the tool step stays on the completion event.
func TestModelFailure_AfterToolRound(t *testing.T) {
	model := &failingChat{steps: []scriptStep{toolCallStep(), {startErr: status400()}, {startErr: status400()}}}
	engine, log := failureEngine(t, model)

	_, err := execute(t, engine)
	require.NoError(t, err)
	assert.Equal(t, 3, model.callCount(), "round 1, round 2, one synthesis attempt")
	assert.Equal(t, []string{"error", "complete"}, log.order())
	assert.Equal(t, types.TurnFailureModelUnavailableAfterSearch, log.errs[0].ErrorCode)
	assert.Equal(t, types.ModelUnavailableAfterSearchMessage, log.errs[0].Error)
	require.NotNil(t, log.final)
	assert.Empty(t, log.final.FinalAnswer)
	steps, _ := log.final.AgentSteps.([]types.AgentStep)
	require.Len(t, steps, 1, "the tool step stays visible")
	assert.Len(t, steps[0].ToolCalls, 1)
}

// The synthesis after the iteration limit fails: the failure notice, not the
// "budget ran out" fallback.
func TestModelFailure_DuringSynthesis(t *testing.T) {
	model := &failingChat{steps: []scriptStep{{startErr: status400()}}}
	engine, log := failureEngine(t, model)
	state := &types.AgentState{RoundSteps: []types.AgentStep{{ToolCalls: []types.ToolCall{{
		Name: "counting_tool", Result: &types.ToolResult{Success: true, Output: "kết quả"}}}}}}

	engine.handleMaxIterations(context.Background(), "q", state, "sess-1", emptyMessages())
	assert.True(t, state.IsComplete)
	assert.Empty(t, state.FinalAnswer)
	require.Len(t, log.errs, 1)
	assert.Equal(t, types.TurnFailureModelUnavailableAfterSearch, log.errs[0].ErrorCode)
}

// The synthesis breaks off after part of its answer: the part is kept and
// the note is appended to the same answer stream.
func TestModelFailure_SynthesisMidStream(t *testing.T) {
	model := &failingChat{steps: []scriptStep{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "Theo kết quả tra cứu, "},
		{ResponseType: types.ResponseTypeError, Content: "unexpected EOF", Done: true},
	}}}}
	engine, log := failureEngine(t, model)
	state := &types.AgentState{RoundSteps: []types.AgentStep{{ToolCalls: []types.ToolCall{{
		Name: "counting_tool", Result: &types.ToolResult{Success: true, Output: "kết quả"}}}}}}

	engine.handleMaxIterations(context.Background(), "q", state, "sess-1", emptyMessages())
	require.Len(t, log.errs, 1)
	assert.Equal(t, types.TurnFailureModelInterrupted, log.errs[0].ErrorCode)
	assert.True(t, strings.HasSuffix(state.FinalAnswer, types.ModelInterruptedNote), state.FinalAnswer)
	assert.Equal(t, log.answer.String(), state.FinalAnswer, "the saved answer is what was streamed")
	assert.Equal(t, 1, model.callCount())
}

// Mid-stream in round 1: the shown text is kept, the note follows on the
// same stream, no retry repeats the text, and the turn is marked.
func TestModelFailure_MidStreamKeepsPartialWithoutRetry(t *testing.T) {
	model := &failingChat{steps: []scriptStep{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "Điều 5 quy định về người sử dụng đất. "},
		{ResponseType: types.ResponseTypeAnswer, Content: "Cụ thể gồm "},
		// transient wording: still not retried, text was shown
		{ResponseType: types.ResponseTypeError, Content: "read: connection reset by peer", Done: true},
	}}}}
	engine, log := failureEngine(t, model)

	state, err := execute(t, engine)
	require.NoError(t, err)
	assert.Equal(t, 1, model.callCount(), "no retry once answer text was shown")

	assert.Equal(t, []string{"answer", "answer_done", "error", "complete"}, log.order())
	assert.Equal(t, types.TurnFailureModelInterrupted, log.errs[0].ErrorCode)
	shown := log.answer.String()
	assert.True(t, strings.HasPrefix(shown, "Điều 5 quy định về người sử dụng đất."), shown)
	assert.True(t, strings.HasSuffix(shown, types.ModelInterruptedNote), shown)
	assert.Equal(t, 1, strings.Count(shown, "Điều 5 quy định"), "nothing duplicated")
	assert.Equal(t, shown, state.FinalAnswer, "the saved answer equals the streamed one")
	assert.Equal(t, shown, log.final.FinalAnswer)
}

// A transient refusal, then a healthy call: the retry answers, the text
// appears once and no error event is sent.
func TestModelFailure_TransientThenSuccess(t *testing.T) {
	model := &failingChat{steps: []scriptStep{
		{startErr: errors.New("API request failed with status 503: overloaded")},
		{chunks: []types.StreamResponse{{ResponseType: types.ResponseTypeAnswer, Content: "Câu trả lời đầy đủ.", Done: true}}},
	}}
	engine, log := failureEngine(t, model)

	state, err := execute(t, engine)
	require.NoError(t, err)
	assert.Equal(t, 2, model.callCount())
	assert.Empty(t, log.errs)
	assert.Empty(t, engine.turnFailure)
	assert.Equal(t, "Câu trả lời đầy đủ.", state.FinalAnswer)
	assert.Equal(t, 1, strings.Count(log.answer.String(), "Câu trả lời đầy đủ."))
	assert.Equal(t, []string{"answer", "answer_done", "complete"}, log.order())
}

// A user stop stays a stop: no failure notice, no failure mark.
func TestModelFailure_UserStopIsNotAFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := &failingChat{steps: []scriptStep{{
		onCall:   cancel,
		startErr: fmt.Errorf("send request: %w", context.Canceled),
	}}}
	engine, log := failureEngine(t, model)

	_, _ = engine.Execute(ctx, "sess-1", "msg-1", "q", nil)
	assert.Equal(t, 1, model.callCount(), "a stop is not retried")
	assert.Empty(t, engine.turnFailure)
	for _, e := range log.errs {
		assert.Empty(t, e.ErrorCode, "a stop carries no failure code")
	}
	require.NotNil(t, log.final, "the stop still completes the turn's bookkeeping")
	assert.Empty(t, log.final.FinalAnswer)
}

func TestRetryableModelError(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{refused(), true},
		{errors.New("API request failed with status 502: bad gateway"), true},
		{errors.New("API request failed with status 500: EngineCore encountered an issue"), true},
		{errors.New("LLM stream stalled: no output for 1m0s"), true},
		{errors.New("API request failed with status 429: rate limit"), true},
		{status400(), false},
		{errors.New("error, status code: 401, message: invalid api key timeout"), false},
		{withPartialAnswer(refused(), "a1", "đã hiện"), false},
	} {
		assert.Equal(t, tc.want, retryableModelError(tc.err), tc.err.Error())
	}
}
