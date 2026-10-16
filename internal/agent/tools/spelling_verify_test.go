package tools

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// verifierChat answers real_error false for the flagged texts in notErrors.
type verifierChat struct {
	chatBase
	mu        sync.Mutex
	notErrors []string
	asked     []string
}

func (v *verifierChat) Chat(_ context.Context, msgs []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	u := msgs[len(msgs)-1].Content
	v.asked = append(v.asked, u)
	for _, w := range v.notErrors {
		if strings.Contains(u, "Chỗ bị đánh dấu: \""+w+"\"") {
			return &types.ChatResponse{Content: `{"real_error": false}`}, nil
		}
	}
	return &types.ChatResponse{Content: `{"real_error": true}`}, nil
}

func (v *verifierChat) GetModelName() string { return "verifier" }
func (v *verifierChat) GetModelID() string   { return "verifier" }

type memSpellingFeedback struct {
	mu   sync.Mutex
	rows []types.DocumentSpellingFeedback
}

func (m *memSpellingFeedback) Record(_ context.Context, rows []types.DocumentSpellingFeedback) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, rows...)
	return nil
}

func (m *memSpellingFeedback) Dismissed(_ context.Context, tenantID uint64, hashes []string) ([]types.DocumentSpellingFeedback, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []types.DocumentSpellingFeedback
	for _, r := range m.rows {
		for _, h := range hashes {
			if r.TenantID == tenantID && r.ParagraphHash == h && r.Source == types.SpellingFeedbackSourceUser {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

const quaParagraph = "c) Các cơ quan quản lý nhà nước có thẩm quyền qua rà soát, thanh kiểm tra phát hiện khách hàng không đủ điều kiện."

func TestNeedsVerification(t *testing.T) {
	cases := map[string]bool{
		"qua":        true,  // a real word: maybe no mistake
		"quyết đinh": true,  // a real-word error, asked too
		"triễn khai": false, // no such syllable: a plain typo
		"các các":    false, // doubled word
		" ,":         false, // punctuation
		"p`hủ":       false,
	}
	for wrong, want := range cases {
		if got := needsVerification(SpellingFinding{Wrong: wrong, Correct: "x", Reason: "gõ nhầm"}); got != want {
			t.Errorf("needsVerification(%q) = %v, want %v", wrong, got, want)
		}
	}
	if needsVerification(SpellingFinding{Wrong: "qua", Reason: strayCharReason}) {
		t.Error("stray characters are never asked")
	}
}

func TestLiveMarksVerifierDropsRealWordFalsePositive(t *testing.T) {
	spellPasses.reset()
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	model := &spellChat{replies: []string{`{"findings":[` +
		`{"paragraph":0,"wrong":"qua","correct":"quan","reason":"gõ nhầm"},` +
		`{"paragraph":1,"wrong":"đinh","correct":"định","reason":"thiếu dấu"}]}`}}
	verifier := &verifierChat{notErrors: []string{"qua"}}
	store := &memSpellingFeedback{}
	tool := NewCheckSpellingTool(nil, model, "sess-1").WithVerifier(verifier).WithFeedback(store).ForDocument("doc-1")
	paras := []LiveParagraph{{Text: quaParagraph}, {Text: "Căn cứ quyết đinh số 349/QĐ-QĐTPT."}}
	ops, found, missed := tool.LiveMarks(ctx, paras)
	if len(missed) != 0 || found != 1 || len(ops) != 1 || ops[0].Text != "đinh" {
		t.Fatalf("only the real mistake is underlined: found %d missed %v ops %+v", found, missed, ops)
	}
	if len(verifier.asked) != 2 || !strings.Contains(verifier.asked[0]+verifier.asked[1], "thẩm quyền qua rà soát") {
		t.Fatalf("both real-word findings are asked with their sentence: %q", verifier.asked)
	}
	if len(store.rows) != 1 || store.rows[0].Source != types.SpellingFeedbackSourceVerifier ||
		store.rows[0].Wrong != "qua" || store.rows[0].Suggestion != "quan" || store.rows[0].DocumentID != "doc-1" ||
		store.rows[0].TenantID != 7 || store.rows[0].Paragraph != quaParagraph {
		t.Fatalf("the rejection is kept as training data: %+v", store.rows)
	}
}

func TestLiveMarksSkipsDismissedMistakes(t *testing.T) {
	spellPasses.reset()
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	model := &spellChat{replies: []string{`{"findings":[{"paragraph":0,"wrong":"qua","correct":"quan","reason":"gõ nhầm"}]}`}}
	store := &memSpellingFeedback{rows: []types.DocumentSpellingFeedback{{
		TenantID: 7, ParagraphHash: SpellingParagraphHash(quaParagraph), Wrong: "qua",
		Source: types.SpellingFeedbackSourceUser, Verdict: types.SpellingVerdictNotError,
	}}}
	tool := NewCheckSpellingTool(nil, model, "sess-1").WithFeedback(store)
	ops, found, _ := tool.LiveMarks(ctx, []LiveParagraph{{Text: quaParagraph, Edited: true}})
	if found != 0 || len(ops) != 1 || ops[0].Op != OpUnmark {
		t.Fatalf("a text the user said is no mistake is not underlined again: found %d ops %+v", found, ops)
	}
	// another tenant's verdict does not count
	ctx8 := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(8))
	if _, found, _ = tool.LiveMarks(ctx8, []LiveParagraph{{Text: quaParagraph}}); found != 1 {
		t.Fatalf("verdicts are per tenant: found %d", found)
	}
}

func TestVerifyContextClipsAroundTheWord(t *testing.T) {
	long := strings.Repeat("một hai ba bốn năm ", 100) + "chữ qua đây " + strings.Repeat("sáu bảy tám ", 100)
	got := verifyContext(long, "chữ qua")
	if !strings.Contains(got, "chữ qua đây") || len([]rune(got)) > 2*spellVerifyContextRunes+20 {
		t.Fatalf("context: %d runes, %q…", len([]rune(got)), got[:40])
	}
}
