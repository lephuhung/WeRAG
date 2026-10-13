package service

import (
	"context"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type recordingEventBus struct {
	mu     sync.Mutex
	events []types.Event
}

func (b *recordingEventBus) On(types.EventType, types.EventHandler) {}

func (b *recordingEventBus) Emit(_ context.Context, evt types.Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, evt)
	return nil
}

func (b *recordingEventBus) answers() []event.AgentFinalAnswerData {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []event.AgentFinalAnswerData
	for _, evt := range b.events {
		if d, ok := evt.Data.(event.AgentFinalAnswerData); ok {
			out = append(out, d)
		}
	}
	return out
}

func runFallbackStream(t *testing.T, chunks []types.StreamResponse) (*recordingEventBus, *types.ChatManage) {
	t.Helper()
	ch := make(chan types.StreamResponse, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	bus := &recordingEventBus{}
	cm := &types.ChatManage{}
	cm.SessionID = "sess-fallback"
	cm.EventBus = bus
	cm.FallbackResponse = "fixed"
	(&sessionService{}).consumeFallbackStream(context.Background(), cm, ch, modelcontext.NewRegistry(false))
	return bus, cm
}

// finish_reason arrives before the usage chunk; the Done event must carry
// the usage so the quick answer is persisted with its token cost.
func TestFallbackStreamHoldsDoneUntilUsage(t *testing.T) {
	usage := &types.TokenUsage{PromptTokens: 1520, CompletionTokens: 9, TotalTokens: 1529}
	bus, cm := runFallbackStream(t, []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "1 + 1 "},
		{ResponseType: types.ResponseTypeAnswer, Content: "bằng 2.", Done: true},
		{ResponseType: types.ResponseTypeAnswer, Done: true, Usage: usage},
	})
	require.Equal(t, []event.AgentFinalAnswerData{
		{Content: "1 + 1 ", IsFallback: true},
		{Content: "bằng 2.", IsFallback: true},
		{Done: true, IsFallback: true, Usage: usage},
	}, bus.answers())
	require.NotNil(t, cm.ChatResponse)
	require.Equal(t, "1 + 1 bằng 2.", cm.ChatResponse.Content)
}

// Usage on the finish chunk itself (Anthropic, Ollama) completes at once.
func TestFallbackStreamDoneWithUsageCompletesImmediately(t *testing.T) {
	usage := &types.TokenUsage{TotalTokens: 7}
	bus, _ := runFallbackStream(t, []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "ok", Done: true, Usage: usage},
	})
	require.Equal(t, []event.AgentFinalAnswerData{{Content: "ok", Done: true, IsFallback: true, Usage: usage}}, bus.answers())
}

// No usage ever: the held Done still goes out when the stream closes, and
// the fixed fallback text is not substituted for the real answer.
func TestFallbackStreamEmitsHeldDoneOnClose(t *testing.T) {
	bus, cm := runFallbackStream(t, []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "ok", Done: true},
	})
	require.Equal(t, []event.AgentFinalAnswerData{
		{Content: "ok", IsFallback: true},
		{Done: true, IsFallback: true},
	}, bus.answers())
	require.Equal(t, "ok", cm.ChatResponse.Content)
}
