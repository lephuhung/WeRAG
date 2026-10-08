package tools

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// resetFormatChecks gives a test an empty format-check cache.
func resetFormatChecks(t *testing.T) {
	t.Helper()
	formatChecks.mu.Lock()
	formatChecks.entries = map[string]*formatCheckResult{}
	formatChecks.mu.Unlock()
	formatChecks.prewarmed = sync.Map{}
	formatChecks.states = sync.Map{}
	formatChecks.rdb = nil
}

// restartProcess drops what a server restart loses: the process caches.
func restartProcess() {
	formatChecks.mu.Lock()
	formatChecks.entries = map[string]*formatCheckResult{}
	formatChecks.mu.Unlock()
	formatChecks.prewarmed = sync.Map{}
	formatChecks.states = sync.Map{}
}

func useMiniRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close(); formatChecks.rdb = nil })
	UseFormatCheckRedis(rdb)
	return mr
}

func waitFormatCheck(t *testing.T, sessionID string) *types.DocumentFormatCheck {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st := SessionFormatCheck(context.Background(), sessionID); st != nil && !st.InProgress() {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	return SessionFormatCheck(context.Background(), sessionID)
}

// countingChat answers both format-check calls and counts the evaluations.
type countingChat struct {
	fakeChat
	mu    sync.Mutex
	evals int
}

func (c *countingChat) Chat(ctx context.Context, msgs []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	if opts.Thinking != nil && *opts.Thinking {
		c.mu.Lock()
		c.evals++
		c.mu.Unlock()
	}
	return c.fakeChat.Chat(ctx, msgs, opts)
}

func (c *countingChat) evalCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evals
}

func TestCheckDocumentFormatReusesEvaluation(t *testing.T) {
	resetFormatChecks(t)
	model := &countingChat{fakeChat: fakeChat{reply: "{}", evaluation: "## Kết luận\nĐạt."}}
	ws := newFakeWorkspace(docxFixture(t))
	tool := NewCheckDocumentFormatToolForWorkspace(ws, model, "sess-c")
	for i := 0; i < 2; i++ {
		res, err := tool.Execute(toolCtx(), json.RawMessage(`{}`))
		if err != nil || !res.Success || res.Data["evaluated"] != true || res.Data["document_revision"] != 3 {
			t.Fatalf("call %d: %+v %v", i, res, err)
		}
	}
	// naming the detected rule set gets the same evaluation
	res, _ := tool.Execute(toolCtx(), json.RawMessage(`{"document_type":"cong_van"}`))
	if !res.Success {
		t.Fatalf("named type: %+v", res)
	}
	if n := model.evalCount(); n != 1 {
		t.Fatalf("evaluations = %d, want 1", n)
	}
}

func TestCheckDocumentFormatPrewarmFeedsTheTool(t *testing.T) {
	resetFormatChecks(t)
	model := &countingChat{fakeChat: fakeChat{reply: "{}", evaluation: "## Kết luận\nĐạt."}}
	tool := NewCheckDocumentFormatToolForWorkspace(fakeWorkspaceWithID(docxFixture(t), "ws-sess-p"), model, "sess-p").ForDocument("ws-sess-p")
	tool.Prewarm(toolCtx())
	tool.Prewarm(toolCtx()) // once per document
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st := SessionFormatCheck(context.Background(), "ws-sess-p"); st != nil && st.Status == types.DocumentFormatCheckReady {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	st := SessionFormatCheck(context.Background(), "ws-sess-p")
	if st == nil || st.Status != types.DocumentFormatCheckReady || st.Revision != 3 ||
		st.DocumentType != "cong_van" || st.DocumentTypeLabel != "Công văn" || st.FinishedAt == nil {
		t.Fatalf("state = %+v", st)
	}
	res, err := tool.Execute(toolCtx(), json.RawMessage(`{}`))
	if err != nil || !res.Success || res.Data["evaluated"] != true {
		t.Fatalf("result: %+v %v", res, err)
	}
	if n := model.evalCount(); n != 1 {
		t.Fatalf("evaluations = %d, want 1 (prewarm shared with the tool call)", n)
	}
}

func TestCheckDocumentFormatDoesNotCacheFallback(t *testing.T) {
	resetFormatChecks(t)
	model := &countingChat{fakeChat: fakeChat{reply: "{}"}} // evaluation fails
	tool := NewCheckDocumentFormatToolForWorkspace(newFakeWorkspace(docxFixture(t)), model, "sess-f")
	for i := 0; i < 2; i++ {
		if res, _ := tool.Execute(toolCtx(), json.RawMessage(`{}`)); !res.Success || res.Data["evaluated"] != false {
			t.Fatalf("call %d: %+v", i, res)
		}
	}
	if n := model.evalCount(); n != 2 {
		t.Fatalf("evaluations = %d, want a retry after a failed one", n)
	}
}

func TestCheckDocumentFormatPrewarmReportsFailure(t *testing.T) {
	resetFormatChecks(t)
	model := &countingChat{fakeChat: fakeChat{reply: "{}"}} // evaluation fails
	tool := NewCheckDocumentFormatToolForWorkspace(fakeWorkspaceWithID(docxFixture(t), "ws-sess-x"), model, "sess-x").ForDocument("ws-sess-x")
	if SessionFormatCheck(context.Background(), "ws-sess-x") != nil {
		t.Fatal("no state before a check")
	}
	tool.Prewarm(toolCtx())
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st := SessionFormatCheck(context.Background(), "ws-sess-x"); st != nil && !st.InProgress() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st := SessionFormatCheck(context.Background(), "ws-sess-x"); st == nil || st.Status != types.DocumentFormatCheckFailed {
		t.Fatalf("state = %+v", st)
	}
}

func TestDocumentTypeLabel(t *testing.T) {
	if got := documentTypeLabel("Quy chế (NĐ30/2020, Phụ lục I)"); got != "Quy chế" {
		t.Fatalf("got %q", got)
	}
	if got := documentTypeLabel("Công văn"); got != "Công văn" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatCheckSurvivesRestartInRedis(t *testing.T) {
	resetFormatChecks(t)
	mr := useMiniRedis(t)
	model := &countingChat{fakeChat: fakeChat{reply: "{}", evaluation: "## Kết luận\nĐạt."}}
	tool := NewCheckDocumentFormatToolForWorkspace(fakeWorkspaceWithID(docxFixture(t), "ws-sess-r"), model, "sess-r").ForDocument("ws-sess-r")
	tool.Prewarm(toolCtx())
	if st := waitFormatCheck(t, "ws-sess-r"); st == nil || st.Status != types.DocumentFormatCheckReady || st.DocumentTypeLabel != "Công văn" {
		t.Fatalf("state = %+v", st)
	}
	if ttl := mr.TTL(formatCheckStateKeyPrefix + "ws-sess-r"); ttl < time.Hour {
		t.Fatalf("state TTL = %s, want at least an hour", ttl)
	}

	restartProcess()
	// the reloaded page and a new turn find the state and do not run again
	if st := SessionFormatCheck(context.Background(), "ws-sess-r"); st == nil || st.Status != types.DocumentFormatCheckReady {
		t.Fatalf("state after restart = %+v", st)
	}
	tool.Prewarm(toolCtx())
	res, err := tool.Execute(toolCtx(), json.RawMessage(`{}`))
	if err != nil || !res.Success || res.Data["evaluated"] != true || res.Data["file_name"] != "cong-van.docx" {
		t.Fatalf("result after restart: %+v %v", res, err)
	}
	if skills, _ := res.Data["skills"].([]string); len(skills) == 0 {
		t.Fatalf("skills lost in Redis: %v", res.Data["skills"])
	}
	time.Sleep(50 * time.Millisecond) // a second prewarm would have started by now
	if n := model.evalCount(); n != 1 {
		t.Fatalf("evaluations = %d, want 1 across the restart", n)
	}
}

func TestStaleRunningStateIsIgnored(t *testing.T) {
	resetFormatChecks(t)
	useMiniRedis(t)
	formatChecks.storeState(context.Background(), "sess-d", &types.DocumentFormatCheck{
		Status: types.DocumentFormatCheckRunning, StartedAt: time.Now().Add(-2 * formatCheckRunTimeout),
	})
	if st := SessionFormatCheck(context.Background(), "sess-d"); st != nil {
		t.Fatalf("a run that died with its process must not count: %+v", st)
	}
}
