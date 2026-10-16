package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
)

// The spell-check model also flags words that are spelled right ("có thẩm
// quyền qua rà soát": "qua" → quản / quá / quán / quan, a different
// correction each run). A finding whose flagged text is made of real
// Vietnamese syllables is therefore asked again of a verifier model (the
// platform's auxiliary model) with the sentence around it, and dropped
// when the verifier says it is no mistake. Typos that are no word at all
// ("triễn", "cascc"), doubled words and punctuation are not asked. The
// user's "not a mistake" verdicts (spelling list) drop a finding too.

// spellVerifyPrompt asks whether one flagged text is a genuine mistake.
const spellVerifyPrompt = `Bạn soát chính tả văn bản hành chính tiếng Việt. Một công cụ tự động đã đánh dấu một chỗ trong câu là lỗi chính tả và đề xuất cách sửa. Hãy xác định chỗ đó có thật sự viết sai không.
- Là lỗi thật: gõ nhầm, sai hoặc thiếu dấu làm thành một từ khác không hợp nghĩa, lặp từ, sai chính tả.
- KHÔNG phải lỗi: từ viết đúng và hợp nghĩa trong câu, kể cả khi có cách diễn đạt khác hay hơn; biến thể dấu (hoà/hòa) hay i/y (lí/lý).
Trả lời DUY NHẤT một JSON: {"real_error": true} hoặc {"real_error": false}`

const (
	// spellVerifyContextRunes is how much of the paragraph either side of
	// the flagged text the verifier sees.
	spellVerifyContextRunes = 300
	spellVerifyConcurrency  = 3
	spellVerifyMaxTokens    = 40
)

var spellVerdictRe = regexp.MustCompile(`"real_error"\s*:\s*(true|false)`)

// WithVerifier makes the tool ask model again about findings on real
// words (nil: no second opinion).
func (t *CheckSpellingTool) WithVerifier(model chat.Chat) *CheckSpellingTool {
	t.verifier = model
	return t
}

// WithFeedback lets the user's "not a mistake" verdicts drop findings and
// records the verifier's rejections as training data (nil: neither).
func (t *CheckSpellingTool) WithFeedback(store interfaces.DocumentSpellingFeedbackRepository) *CheckSpellingTool {
	if store != nil {
		t.feedback = store
	}
	return t
}

// ForDocument is a copy of the tool whose feedback rows name documentID.
func (t *CheckSpellingTool) ForDocument(documentID string) *CheckSpellingTool {
	c := *t
	c.documentID = documentID
	return &c
}

// SpellingParagraphHash identifies a paragraph's text for the spelling
// feedback (normalised like the editor anchors).
func SpellingParagraphHash(text string) string {
	sum := sha256.Sum256([]byte(normalizeDocText(text)))
	return hex.EncodeToString(sum[:])
}

// spellingDismissKey is the key of a "not a mistake" verdict.
func spellingDismissKey(paragraphHash, wrong string) string {
	return paragraphHash + "\x00" + strings.ToLower(normalizeDocText(wrong))
}

// needsVerification: the flagged text is all real syllables (a real-word
// error, or no error at all), neither a doubled word nor punctuation.
func needsVerification(f SpellingFinding) bool {
	if f.Reason == strayCharReason {
		return false
	}
	words := strings.Fields(f.Wrong)
	if len(words) == 0 {
		return false
	}
	prev := ""
	for _, w := range words {
		w = strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) })
		if w == "" {
			return false // punctuation
		}
		lw := strings.ToLower(w)
		if lw == prev {
			return false // doubled word
		}
		prev = lw
		if !abbreviation.IsVietnameseSyllable(w) {
			return false
		}
	}
	return true
}

// verifyContext is the part of text around wrong the verifier is shown.
func verifyContext(text, wrong string) string {
	s := strings.TrimSpace(text)
	runes := []rune(s)
	at := strings.Index(s, wrong)
	if at < 0 || len(runes) <= 2*spellVerifyContextRunes {
		return clipRunes(s, 2*spellVerifyContextRunes)
	}
	pos := utf8.RuneCountInString(s[:at])
	from := max(0, pos-spellVerifyContextRunes)
	to := min(len(runes), pos+len([]rune(wrong))+spellVerifyContextRunes)
	return "…" + string(runes[from:to]) + "…"
}

// verifyFindings asks the verifier about the real-word findings and drops
// those it says are no mistake; an unanswered or unclear one is kept.
// textOf is the paragraph text of a finding.
func (t *CheckSpellingTool) verifyFindings(ctx context.Context, findings []SpellingFinding, textOf func(SpellingFinding) string) []SpellingFinding {
	if t.verifier == nil || len(findings) == 0 {
		return findings
	}
	reject := make([]bool, len(findings))
	sem := make(chan struct{}, spellVerifyConcurrency)
	var wg sync.WaitGroup
	for i, f := range findings {
		if !needsVerification(f) {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, f SpellingFinding) {
			defer func() { <-sem; wg.Done() }()
			reject[i] = t.verifierRejects(ctx, verifyContext(textOf(f), f.Wrong), f)
		}(i, f)
	}
	wg.Wait()
	out := findings[:0:0]
	var rows []types.DocumentSpellingFeedback
	for i, f := range findings {
		if !reject[i] {
			out = append(out, f)
			continue
		}
		logger.Infof(ctx, "check_spelling: verifier rejected %q → %q", f.Wrong, f.Correct)
		rows = append(rows, t.feedbackRow(ctx, textOf(f), f, types.SpellingFeedbackSourceVerifier))
	}
	t.recordFeedback(ctx, rows)
	return out
}

// verifierRejects is true when the verifier answers that f is no mistake.
func (t *CheckSpellingTool) verifierRejects(ctx context.Context, sentence string, f SpellingFinding) bool {
	msgs := []chat.Message{
		{Role: "system", Content: spellVerifyPrompt},
		{Role: "user", Content: "Câu: \"" + sentence + "\"\nChỗ bị đánh dấu: \"" + f.Wrong + "\"\nĐề xuất sửa thành: \"" + f.Correct + "\""},
	}
	thinking := false
	resp, err := t.verifier.Chat(ctx, msgs, &chat.ChatOptions{Temperature: 0.01, MaxTokens: spellVerifyMaxTokens, Thinking: &thinking})
	if err != nil || resp == nil {
		logger.Warnf(ctx, "check_spelling: verifier unavailable, keeping %q: %v", f.Wrong, err)
		return false
	}
	m := spellVerdictRe.FindStringSubmatch(resp.Content)
	return m != nil && m[1] == "false"
}

// dropDismissed removes the findings the user marked "not a mistake" in a
// paragraph with the same text.
func (t *CheckSpellingTool) dropDismissed(ctx context.Context, findings []SpellingFinding, textOf func(SpellingFinding) string) []SpellingFinding {
	if t.feedback == nil || len(findings) == 0 {
		return findings
	}
	texts := make([]string, len(findings))
	for i, f := range findings {
		texts[i] = textOf(f)
	}
	dismissed := t.dismissedIn(ctx, texts)
	if len(dismissed) == 0 {
		return findings
	}
	out := findings[:0:0]
	for i, f := range findings {
		if !dismissed[spellingDismissKey(SpellingParagraphHash(texts[i]), f.Wrong)] {
			out = append(out, f)
		}
	}
	return out
}

// dismissedIn is the set of "not a mistake" verdicts (spellingDismissKey)
// on paragraphs with these texts; empty when unknown.
func (t *CheckSpellingTool) dismissedIn(ctx context.Context, texts []string) map[string]bool {
	if t.feedback == nil || len(texts) == 0 {
		return nil
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var hashes []string
	for _, text := range texts {
		if h := SpellingParagraphHash(text); !seen[h] {
			seen[h] = true
			hashes = append(hashes, h)
		}
	}
	rows, err := t.feedback.Dismissed(ctx, tenantID, hashes)
	if err != nil {
		logger.Warnf(ctx, "check_spelling: dismissed verdicts unavailable: %v", err)
		return nil
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[spellingDismissKey(r.ParagraphHash, r.Wrong)] = true
	}
	return out
}

func (t *CheckSpellingTool) feedbackRow(ctx context.Context, paragraph string, f SpellingFinding, source string) types.DocumentSpellingFeedback {
	row := types.DocumentSpellingFeedback{
		SessionID:      t.sessionID,
		DocumentID:     t.documentID,
		Paragraph:      paragraph,
		ParagraphHash:  SpellingParagraphHash(paragraph),
		Wrong:          clipRunes(f.Wrong, 255),
		Suggestion:     clipRunes(f.Correct, 255),
		TextOccurrence: max(1, f.occurrence),
		Verdict:        types.SpellingVerdictNotError,
		Source:         source,
		Note:           f.Reason,
	}
	row.TenantID, _ = types.TenantIDFromContext(ctx)
	row.UserID, _ = types.UserIDFromContext(ctx)
	if t.model != nil {
		row.Model = t.model.GetModelName()
	}
	if source == types.SpellingFeedbackSourceVerifier && t.verifier != nil {
		row.Note = "verifier " + t.verifier.GetModelName() + ": " + f.Reason
	}
	return row
}

func (t *CheckSpellingTool) recordFeedback(ctx context.Context, rows []types.DocumentSpellingFeedback) {
	if t.feedback == nil || len(rows) == 0 || rows[0].TenantID == 0 {
		return
	}
	if err := t.feedback.Record(ctx, rows); err != nil {
		logger.Warnf(ctx, "check_spelling: recording verifier verdicts failed: %v", err)
	}
}
