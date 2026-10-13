package tools

import "strings"

// apply_format_fixes changes the document only after the user agreed. The
// prompt says so and the model ignored it ("Sửa các lỗi thể thức đó giúp
// tôi" went straight to apply=true, then force=true), so the tool reads
// the user's own words: without a consent phrase the call is a review.

// consentPhrases are folded (foldSearch) phrases that mean "go ahead".
var consentPhrases = []string{
	"dong y", "nhat tri", "chap nhan", "xac nhan", "ap dung", "sua luon", "sua ngay",
	"tu sua", "cu sua", "sua di", "lam di", "tien hanh", "thuc hien di", "lam luon",
	"ok", "oke", "okay", "duoc", "dong y sua",
}

// consentNegations before a phrase turn it into a refusal.
var consentNegations = map[string]bool{"khong": true, "chua": true, "dung": true, "khoan": true, "khong can": true}

// noConsentNote opens the output of a call that asked for apply=true
// without the user's agreement.
const noConsentNote = "Người dùng CHƯA đồng ý áp dụng trong câu này (không có \"đồng ý\", \"áp dụng\", \"sửa luôn\"…), nên lần này chỉ XEM TRƯỚC: apply bị bỏ qua, tài liệu chưa đổi. " +
	"Trình bày kế hoạch dưới đây và hỏi người dùng có áp dụng không; chỉ gọi lại với apply=true sau khi họ trả lời đồng ý."

// formatFixConsent reports whether the user's request agrees to applying
// the fixes. An unknown request (no query in ctx) is not blocked.
func formatFixConsent(query string) bool {
	q := strings.TrimSpace(query)
	if q == "" {
		return true
	}
	words := strings.Fields(foldSearch(q))
	text := " " + strings.Join(words, " ") + " "
	for _, p := range consentPhrases {
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
