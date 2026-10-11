package vietnamese_legal

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// CatalogNumberKey is the lookup key of a số ký hiệu in the legal
// catalogue: NFC, upper-case, no whitespace, Đ folded to D and dash
// variants unified, so "30/2020/nđ-cp", "30/2020/ND-CP" and
// "30 / 2020 / NĐ–CP" share one key.
func CatalogNumberKey(num string) string {
	var b strings.Builder
	for _, r := range norm.NFC.String(num) {
		switch {
		case unicode.IsSpace(r):
			continue
		case r == 'đ' || r == 'Đ':
			b.WriteRune('D')
		case r == '–' || r == '—' || r == '‐' || r == '−':
			b.WriteRune('-')
		default:
			b.WriteRune(unicode.ToUpper(r))
		}
	}
	return b.String()
}
