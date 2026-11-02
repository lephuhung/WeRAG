package vietnamese_legal

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// CatalogNumberKey is the lookup key of a số ký hiệu in the legal
// catalogue: NFC, upper-case, no whitespace, Đ and its look-alike Ð
// (U+00D0, in ~750 old vbpl rows) folded to D and dash variants unified,
// so "30/2020/nđ-cp", "30/2020/ND-CP", "30/2020/NÐ-CP" and
// "30 / 2020 / NĐ–CP" share one key.
func CatalogNumberKey(num string) string {
	var b strings.Builder
	for _, r := range norm.NFC.String(num) {
		switch {
		case unicode.IsSpace(r):
			continue
		case r == 'đ' || r == 'Đ' || r == 'ð' || r == 'Ð':
			b.WriteRune('D')
		case r == '–' || r == '—' || r == '‐' || r == '−':
			b.WriteRune('-')
		default:
			b.WriteRune(unicode.ToUpper(r))
		}
	}
	return b.String()
}

// GraphSearchTerms returns names for a graph node search: each name, plus
// the canonical số hiệu of each one that cites a document — the key legal
// document nodes are stored under ("Nghị định 53/2022/nđ-cp" adds
// "53/2022/NĐ-CP"). Order kept, duplicates dropped case-insensitively.
func GraphSearchTerms(names []string) []string {
	seen := map[string]bool{}
	var terms []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if key := strings.ToLower(s); s != "" && !seen[key] {
			seen[key] = true
			terms = append(terms, s)
		}
	}
	for _, name := range names {
		add(name)
		if num := ParseReference(name).DocumentNumber; strings.Contains(num, "/") {
			add(NormalizeDocumentNumber(num))
		}
	}
	return terms
}
