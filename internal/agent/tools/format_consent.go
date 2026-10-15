package tools

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// apply_format_fixes changes the document only after the user agreed. The
// prompt says so and the model ignored it ("Sửa các lỗi thể thức đó giúp
// tôi" went straight to apply=true, then force=true), so the tool reads
// the user's own words. Agreement is tied to a plan: a reply such as
// "đồng ý" counts only when an earlier turn of the conversation showed the
// plan of this document (a review), since a word like "được" or "áp dụng"
// also occurs in requests ("Kiểm tra thể thức văn bản được ban hành hôm
// qua"). Only an explicit "fix it now" needs no plan shown first.

// immediateConsentPhrases are folded (foldSearch) phrases that ask to fix
// without a review first.
var immediateConsentPhrases = []string{
	"sua luon", "sua ngay", "ap dung ngay", "tu sua", "cu sua", "lam luon",
}

// replyConsentPhrases agree to a plan shown in an earlier turn.
var replyConsentPhrases = []string{
	"dong y", "nhat tri", "chap nhan", "xac nhan", "ap dung", "tien hanh", "thuc hien di",
	"sua di", "lam di",
}

// shortReplyConsentWords agree only as a short reply ("OK", "được, làm
// đi"): in a longer message they are ordinary words.
var shortReplyConsentWords = []string{"ok", "oke", "okay", "duoc", "u", "uh", "vang", "co"}

// shortReplyWords is the longest message the short reply words count in.
const shortReplyWords = 4

// consentNegations before a phrase turn it into a refusal.
var consentNegations = map[string]bool{"khong": true, "chua": true, "dung": true, "khoan": true}

// notConsentMarkers make a message a request or a question rather than
// an answer to the plan ("kiểm tra lại…", "có áp dụng được không?").
var notConsentMarkers = []string{"kiem tra", "?"}

// questionEndings end a question.
var questionEndings = []string{"khong", "chua", "gi", "sao", "nao", "the nao", "duoc khong"}

// noConsentNote opens the output of a call that asked for apply=true
// without the user's agreement.
const noConsentNote = "Người dùng CHƯA đồng ý áp dụng kế hoạch sửa thể thức (chưa có câu trả lời \"đồng ý\" cho kế hoạch đã trình bày ở lượt trước, hoặc yêu cầu \"sửa luôn\"), nên lần này chỉ XEM TRƯỚC: apply bị bỏ qua, tài liệu chưa đổi. " +
	"Trình bày kế hoạch dưới đây và hỏi người dùng có áp dụng không; chỉ gọi lại với apply=true sau khi họ trả lời đồng ý."

// formatFixConsent reports whether the user's request agrees to applying
// the fixes. planShown: an earlier turn showed this document's plan. An
// unknown request (no query in ctx) is not blocked.
func formatFixConsent(query string, planShown bool) bool {
	q := strings.TrimSpace(query)
	if q == "" {
		return true
	}
	folded := foldSearch(q)
	words := strings.Fields(folded)
	text := " " + strings.Join(words, " ") + " "
	if hasConsentPhrase(text, immediateConsentPhrases) {
		return true
	}
	if !planShown {
		return false
	}
	for _, m := range notConsentMarkers {
		if strings.Contains(folded, m) {
			return false
		}
	}
	trimmed := strings.TrimRight(text, " .!,")
	for _, e := range questionEndings {
		if strings.HasSuffix(trimmed, " "+e) && len(words) > 1 {
			return false
		}
	}
	if hasConsentPhrase(text, replyConsentPhrases) {
		return true
	}
	return len(words) <= shortReplyWords && hasConsentPhrase(stripPunct(text), shortReplyConsentWords)
}

// hasConsentPhrase reports whether text (space-padded, folded) holds one of
// phrases not negated by the words before it.
func hasConsentPhrase(text string, phrases []string) bool {
	text = stripPunct(text)
	for _, p := range phrases {
		idx := strings.Index(text, " "+p+" ")
		for idx >= 0 {
			if !negatedBefore(text[:idx]) {
				return true
			}
			next := strings.Index(text[idx+1:], " "+p+" ")
			if next < 0 {
				break
			}
			idx += 1 + next
		}
	}
	return false
}

// stripPunct turns sentence punctuation into spaces so "OK," and "được."
// match as words.
func stripPunct(text string) string {
	return " " + strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		switch r {
		case ',', '.', '!', ';', ':':
			return ' '
		}
		return r
	}, text)), " ") + " "
}

// negatedBefore reports whether one of the last three words before the
// phrase is a negation ("không áp dụng", "chưa đồng ý").
func negatedBefore(prefix string) bool {
	words := strings.Fields(prefix)
	if len(words) > 3 {
		words = words[len(words)-3:]
	}
	for _, w := range words {
		if consentNegations[w] {
			return true
		}
	}
	return false
}

// formatPlanTTL is how long a reviewed plan waits for the user's answer.
const formatPlanTTL = 2 * time.Hour

// shownFormatPlan is a review that showed a document's fix plan.
type shownFormatPlan struct {
	turnID string
	at     time.Time
}

// shownFormatPlans remembers, per session and document, the last review
// that showed a plan. It lives in this process: after a restart the user
// is shown the plan again before anything is applied.
var shownFormatPlans = struct {
	sync.Mutex
	m map[string]shownFormatPlan
}{m: map[string]shownFormatPlan{}}

func formatPlanKey(sessionID, documentID string) string {
	return sessionID + "\x00" + documentID
}

// rememberFormatPlan records that this turn showed documentID's plan.
func rememberFormatPlan(ctx context.Context, sessionID, documentID string) {
	now := time.Now()
	shownFormatPlans.Lock()
	defer shownFormatPlans.Unlock()
	for k, p := range shownFormatPlans.m {
		if now.Sub(p.at) > formatPlanTTL {
			delete(shownFormatPlans.m, k)
		}
	}
	shownFormatPlans.m[formatPlanKey(sessionID, documentID)] = shownFormatPlan{turnID: types.TurnIDFromContext(ctx), at: now}
}

// formatPlanShownEarlier reports whether an earlier turn than ctx's showed
// documentID's plan, recently enough to be the one the user answers.
func formatPlanShownEarlier(ctx context.Context, sessionID, documentID string) bool {
	shownFormatPlans.Lock()
	defer shownFormatPlans.Unlock()
	p, ok := shownFormatPlans.m[formatPlanKey(sessionID, documentID)]
	if !ok || time.Since(p.at) > formatPlanTTL {
		return false
	}
	turn := types.TurnIDFromContext(ctx)
	return turn == "" || p.turnID != turn
}

// forgetFormatPlan drops documentID's plan once it was applied: the next
// "đồng ý" needs a new review.
func forgetFormatPlan(sessionID, documentID string) {
	shownFormatPlans.Lock()
	defer shownFormatPlans.Unlock()
	delete(shownFormatPlans.m, formatPlanKey(sessionID, documentID))
}
