package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// longDoc is a 120-paragraph document: head and tail paragraphs, and body
// paragraph 60 whose wording and font the tests vary.
func longDoc(t *testing.T, head, body60, body60Font string, margins [4]int) []byte {
	t.Helper()
	var b strings.Builder
	for i := 0; i < 120; i++ {
		text, font := fmt.Sprintf("Đoạn %d của văn bản.", i), "Times New Roman"
		switch i {
		case 0:
			text = head
		case 60:
			text, font = body60, body60Font
		}
		b.WriteString(testPara(text, "left", font, 14, false, false))
	}
	return buildTestDocx(t, b.String(), margins)
}

func TestFormatFingerprintIgnoresBodyWording(t *testing.T) {
	m := [4]int{20, 15, 30, 20}
	base := formatFingerprint(longDoc(t, "UBND TỈNH", "Nội dung cũ.", "Times New Roman", m))
	if got := formatFingerprint(longDoc(t, "UBND TỈNH", "Nội dung đã viết lại.", "Times New Roman", m)); got != base {
		t.Fatal("rewording a body paragraph must keep the fingerprint")
	}
	for name, doc := range map[string][]byte{
		"body font":   longDoc(t, "UBND TỈNH", "Nội dung cũ.", "Arial", m),
		"head text":   longDoc(t, "SỞ NỘI VỤ", "Nội dung cũ.", "Times New Roman", m),
		"page margin": longDoc(t, "UBND TỈNH", "Nội dung cũ.", "Times New Roman", [4]int{25, 15, 30, 20}),
	} {
		if formatFingerprint(doc) == base {
			t.Errorf("%s change must change the fingerprint", name)
		}
	}
}

// recheckWorkspace is a workspace whose content and save time a test moves.
type recheckWorkspace struct {
	*fakeWorkspace
	mu sync.Mutex
}

func (w *recheckWorkspace) set(content []byte, savedAt time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.content = content
	w.ws.LastSavedAt = &savedAt
}

func (w *recheckWorkspace) GetBySession(ctx context.Context, tenantID uint64, sessionID string) (*types.DocumentWorkspace, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fakeWorkspace.GetBySession(ctx, tenantID, sessionID)
}

func waitState(t *testing.T, sessionID string, ok func(*types.DocumentFormatCheck) bool) *types.DocumentFormatCheck {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st := SessionFormatCheck(context.Background(), sessionID); st != nil && ok(st) {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("state never matched: %+v", SessionFormatCheck(context.Background(), sessionID))
	return nil
}

func TestRecheckOnlyWhenTheFormatChanged(t *testing.T) {
	resetFormatChecks(t)
	quiet, poll := formatRecheckQuiet, formatRecheckPoll
	formatRecheckQuiet, formatRecheckPoll = 50*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { formatRecheckQuiet, formatRecheckPoll = quiet, poll })

	m := [4]int{20, 15, 30, 20}
	ws := &recheckWorkspace{fakeWorkspace: newFakeWorkspace(longDoc(t, "UBND TỈNH", "Nội dung cũ.", "Times New Roman", m))}
	model := &countingChat{fakeChat: fakeChat{reply: "{}", evaluation: "## Kết luận\nĐạt."}}
	tool := NewCheckDocumentFormatToolForWorkspace(ws, model, "sess-rc")
	tool.Prewarm(toolCtx())
	first := waitState(t, "sess-rc", func(st *types.DocumentFormatCheck) bool { return st.Status == types.DocumentFormatCheckReady })
	if first.Fingerprint == "" {
		t.Fatal("the check must record its fingerprint")
	}

	// body wording only: the evaluation is kept and covers the save
	saved := time.Now().Add(time.Second)
	ws.set(longDoc(t, "UBND TỈNH", "Nội dung đã viết lại.", "Times New Roman", m), saved)
	tool.Recheck(toolCtx())
	kept := waitState(t, "sess-rc", func(st *types.DocumentFormatCheck) bool {
		return st.CheckedSavedAt != nil && !st.CheckedSavedAt.Before(saved)
	})
	if kept.Status != types.DocumentFormatCheckReady || model.evalCount() != 1 {
		t.Fatalf("wording edit re-evaluated: state %+v, evaluations %d", kept, model.evalCount())
	}
	if FormatCheckNeedsRecheck(context.Background(), "sess-rc", &saved) {
		t.Fatal("a covered save needs no re-check")
	}

	// a font change: checked again after the quiet period
	ws.set(longDoc(t, "UBND TỈNH", "Nội dung đã viết lại.", "Arial", m), time.Now().Add(2*time.Second))
	tool.Recheck(toolCtx())
	waitState(t, "sess-rc", func(st *types.DocumentFormatCheck) bool {
		return st.Status == types.DocumentFormatCheckReady && st.Fingerprint != first.Fingerprint
	})
	if n := model.evalCount(); n != 2 {
		t.Fatalf("evaluations = %d, want 2", n)
	}
	res, _ := tool.Execute(toolCtx(), json.RawMessage(`{}`))
	if !res.Success || res.Data["evaluated"] != true || model.evalCount() != 2 {
		t.Fatalf("the agent call must reuse the new evaluation: %+v (%d)", res.Data, model.evalCount())
	}
}
