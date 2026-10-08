package tools

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// blockingChat holds every model call until gate is closed; entered gets
// one value per call that started.
type blockingChat struct {
	chatBase
	gate    chan struct{}
	entered chan struct{}
	mu      sync.Mutex
	evals   int
}

func (b *blockingChat) Chat(_ context.Context, _ []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	b.entered <- struct{}{}
	<-b.gate
	if opts.Thinking != nil && *opts.Thinking {
		b.mu.Lock()
		b.evals++
		b.mu.Unlock()
		return &types.ChatResponse{Content: "## Kết luận\nĐạt."}, nil
	}
	return &types.ChatResponse{Content: "{}"}, nil
}

func (b *blockingChat) GetModelName() string { return "Qwen/Qwen3.6-35B-A3B-FP8" }

// useFormatCheckSlots runs a test with n check slots and a fast queue poll.
func useFormatCheckSlots(t *testing.T, n int) {
	t.Helper()
	slots, poll := formatCheckSlots, formatCheckQueuePoll
	formatCheckSlots, formatCheckQueuePoll = make(chan struct{}, n), 10*time.Millisecond
	t.Cleanup(func() { formatCheckSlots, formatCheckQueuePoll = slots, poll })
}

func TestBackgroundFormatChecksQueueBeyondTheSlots(t *testing.T) {
	resetFormatChecks(t)
	useFormatCheckSlots(t, 1)
	model := &blockingChat{gate: make(chan struct{}), entered: make(chan struct{}, 16)}
	released := false
	release := func() {
		if !released {
			released = true
			close(model.gate)
		}
	}
	t.Cleanup(release)

	toolA := NewCheckDocumentFormatToolForWorkspace(fakeWorkspaceWithID(docxFixture(t), "ws-q-a"), model, "sess-q").ForDocument("ws-q-a")
	toolB := NewCheckDocumentFormatToolForWorkspace(fakeWorkspaceWithID(testCongVan(t, [4]int{20, 15, 30, 20}), "ws-q-b"), model, "sess-q").ForDocument("ws-q-b")

	toolA.Prewarm(toolCtx())
	select {
	case <-model.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first check never called the model")
	}
	toolB.Prewarm(toolCtx())
	waitState(t, "ws-q-b", func(st *types.DocumentFormatCheck) bool { return st.Status == types.DocumentFormatCheckQueued })
	if st := SessionFormatCheck(context.Background(), "ws-q-a"); st == nil || st.Status != types.DocumentFormatCheckRunning {
		t.Fatalf("first document: %+v, want running", st)
	}
	select {
	case <-model.entered:
		t.Fatal("the second check called the model while the only slot was taken")
	case <-time.After(100 * time.Millisecond):
	}
	if FormatCheckNeedsRecheck(context.Background(), "ws-q-b", ptrTime(time.Now().Add(time.Hour))) {
		t.Fatal("a queued check must not be re-checked")
	}

	// the user's own check of the queued document does not wait for a slot
	done := make(chan *types.ToolResult, 1)
	go func() {
		res, _ := toolB.Execute(toolCtx(), json.RawMessage(`{"document":"ws-q-b"}`))
		done <- res
	}()
	select {
	case <-model.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the agent's check waited behind the background queue")
	}

	release()
	if res := <-done; res == nil || !res.Success || res.Data["evaluated"] != true {
		t.Fatalf("agent check: %+v", res)
	}
	for _, id := range []string{"ws-q-a", "ws-q-b"} {
		waitState(t, id, func(st *types.DocumentFormatCheck) bool { return st.Status == types.DocumentFormatCheckReady })
	}
	model.mu.Lock()
	defer model.mu.Unlock()
	if model.evals != 2 {
		t.Fatalf("evaluations = %d, want 2 (the queued prewarm reuses the agent's result)", model.evals)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
