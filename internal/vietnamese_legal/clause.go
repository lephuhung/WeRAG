// clause.go scopes clause references (Điều/Khoản/Điểm) to the document they
// belong to, so the graph merge key of "Điều 5" in one document never
// collides with "Điều 5" in another document of the same knowledge base.
package vietnamese_legal

import (
	"regexp"
	"strings"
)

// clauseSegRe matches one structural segment at the start of a reference:
// the keyword and its label ("Điều 17", "Điều 5a", "Khoản 2", "Điểm đ",
// "Chương II"). The label must end at a separator or the end of the string
// so "Điều 17a" is one label and "Điều 1 Luật" stops after "1". RE2 \b is
// ASCII-only, hence the explicit terminator class.
var clauseSegRe = regexp.MustCompile(
	`(?i)^(điểm|khoản|điều|mục|chương|phần)\s+(\d+[a-zđ]?|[a-zđ]|[ivx]+)(?:[\s,;.)]|$)`,
)

// clauseSepRe skips the connector between two segments or between the lead
// and the document name ("của", "thuộc", "tại", punctuation).
var clauseSepRe = regexp.MustCompile(`(?i)^[\s,;.]*(?:(?:của|thuộc|tại)\s+)?`)

// SplitClauseRef splits a clause reference into its canonical structural
// lead and the document part that follows it:
//
//	"Khoản 3 Điều 49 của Luật An ninh mạng" → ("Khoản 3 Điều 49", "Luật An ninh mạng")
//	"điểm a khoản 2 Điều 17"                → ("Điểm a Khoản 2 Điều 17", "")
//	"Bộ Công an"                             → ("", "Bộ Công an")
//
// The lead keeps the keyword capitalised and the label lower-cased so the
// same clause spelled differently converges on one key.
func SplitClauseRef(name string) (lead, doc string) {
	rest := strings.TrimSpace(name)
	var parts []string
	for {
		m := clauseSegRe.FindStringSubmatchIndex(rest)
		if m == nil {
			break
		}
		keyword := capitalizeWord(strings.ToLower(rest[m[2]:m[3]]))
		label := strings.ToLower(rest[m[4]:m[5]])
		parts = append(parts, keyword+" "+label)
		rest = rest[m[5]:]
		if sep := clauseSepRe.FindStringIndex(rest); sep != nil {
			rest = rest[sep[1]:]
		}
	}
	return strings.Join(parts, " "), strings.TrimSpace(rest)
}

// ScopeClauseName returns the graph merge key for a clause reference and
// whether the reference points at another document.
//
// A reference to the current document — unqualified ("Điều 5"), through a
// self-reference ("Điều 5 của Nghị định này") or by the document's own số
// hiệu / title — is scoped to the root: "Điều 5 (53/2022/NĐ-CP)". A reference
// that names another document ("Khoản 3 Điều 49 Luật An ninh mạng") keeps
// its full normalized name and is reported external, so the caller does not
// attach it to the current document's structure.
//
// Names that are not clause references fall back to NormalizeOrgName.
func ScopeClauseName(name, rootName, title string) (key string, external bool) {
	lead, doc := SplitClauseRef(name)
	if lead == "" {
		return NormalizeOrgName(name), false
	}
	switch {
	case doc == "":
	case IsSelfReferenceName(doc) || HasTrailingSelfRef(doc):
	case rootName != "" && docRefersToRoot(doc, rootName, title):
	default:
		return NormalizeOrgName(name), true
	}
	if rootName == "" {
		return lead, false
	}
	return lead + " (" + rootName + ")", false
}

// docRefersToRoot reports whether the document part of a clause reference
// names the current document: by số hiệu when it carries one, else by an
// exact (normalized) match on the root name or the title.
func docRefersToRoot(doc, rootName, title string) bool {
	if m := DocNumberPattern.FindString(doc); m != "" {
		return NormalizeDocumentNumber(m) == rootName
	}
	return IsDocRootAlias(doc, rootName, title)
}

// clauseArticleRe reads the Điều number of a canonical clause lead.
var clauseArticleRe = regexp.MustCompile(`Điều (\d+[a-zđ]?)`)

// ScopeAmendedClause scopes a clause reference written inside quoted
// amending text to the amended document: in a chunk of Điều 1 whose quoted
// text rewrites Điều 9 of Luật Phòng, chống thiên tai, an unqualified
// "Khoản 2 Điều 9" is "Khoản 2 Điều 9 Luật Phòng, chống thiên tai" — an
// external reference, not this document's Điều 9. ok is false when the
// reference names a document, names one of the chunk's own Điều, or no
// amendment of the chunk covers it.
func ScopeAmendedClause(name string, ownArticles []string, amends []AmendRef) (key string, ok bool) {
	if len(amends) == 0 {
		return "", false
	}
	lead, doc := SplitClauseRef(name)
	if lead == "" || doc != "" {
		return "", false
	}
	m := clauseArticleRe.FindStringSubmatch(lead)
	if m == nil || containsFold(ownArticles, m[1]) {
		return "", false
	}
	for _, a := range amends {
		if a.Document != "" && strings.EqualFold(a.Article, m[1]) {
			return NormalizeOrgName(lead + " " + a.Document), true
		}
	}
	return "", false
}
