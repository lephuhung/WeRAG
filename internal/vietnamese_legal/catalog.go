package vietnamese_legal

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// FoldNumberLookalikes returns the NFC form of a số hiệu with its
// look-alike runes replaced by the ones số hiệu are written with (Eth "Ð"
// U+00D0 → "Đ", Cyrillic capitals → Latin, dash variants → "-", the
// no-break space → " "), the same table FindCitations reads text with.
func FoldNumberLookalikes(num string) string {
	var b strings.Builder
	for _, r := range norm.NFC.String(num) {
		if f, ok := citationFold[r]; ok {
			r = f
		}
		b.WriteRune(r)
	}
	return b.String()
}

// CatalogNumberKey is the lookup key of a số ký hiệu in the legal
// catalogue: look-alikes folded (FoldNumberLookalikes), upper-case, no
// whitespace and Đ folded to D, so "30/2020/nđ-cp", "30/2020/ND-CP",
// "30/2020/NÐ-CP" (Eth, ~750 old vbpl rows), "51/2025/TT-BTС" (Cyrillic
// С) and "30 / 2020 / NĐ–CP" share one key. Every catalogue key — import,
// review, lookups — is built here.
func CatalogNumberKey(num string) string {
	var b strings.Builder
	for _, r := range FoldNumberLookalikes(num) {
		r = unicode.ToUpper(r)
		if f, ok := citationFold[r]; ok { // a lower-case Cyrillic look-alike
			r = f
		}
		switch {
		case unicode.IsSpace(r):
			continue
		case r == 'Đ':
			b.WriteRune('D')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// CatalogTitleKey is a title reduced for telling two catalogue rows of
// one document apart from two documents: look-alikes folded, lower-case,
// letters and digits only ("Quyết định số 02/2000/QÐ-BTC Về việc …" and
// "Quyết định số 02/2000/QĐ-BTC về việc …" agree).
func CatalogTitleKey(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(FoldNumberLookalikes(title)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// DocTypeClass folds a document type for comparison: a code (bộ luật)
// is a law, a joint circular / resolution (liên tịch) a circular /
// resolution, as catalogues and uploads spell them either way.
func DocTypeClass(t string) string {
	t = strings.ToLower(strings.Join(strings.Fields(t), " "))
	switch t {
	case "bộ luật":
		return "luật"
	case "thông tư liên tịch":
		return "thông tư"
	case "nghị quyết liên tịch":
		return "nghị quyết"
	}
	return t
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
