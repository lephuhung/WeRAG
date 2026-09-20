package abbreviation

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
)

// AppliedAbbr records one expansion that was applied to the text.
type AppliedAbbr struct {
	ShortForm string `json:"short_form"`
	FullForm  string `json:"full_form"`
}

// Meaning is one active interpretation of an ambiguous short form.
type Meaning struct {
	FullForm    string `json:"full_form"`
	Description string `json:"description,omitempty"`
}

// ExpandResult is the outcome of Expand over a user text.
type ExpandResult struct {
	Original  string               `json:"original_text"`
	Expanded  string               `json:"expanded_text"`
	Applied   []AppliedAbbr        `json:"applied"`
	Ambiguous map[string][]Meaning `json:"ambiguous,omitempty"`
	// Potential lists tokens that look like abbreviations but have no DB row —
	// callers may surface them to the user for a future suggestion.
	Potential []string `json:"potential_abbreviations"`
}

// Expand rewrites single-meaning abbreviations in text using the active
// dictionary, ports AIRAG _expand_abbreviations_in_message semantics:
//   - candidates come from the heuristic (IsLikelyAbbreviation), not from a
//     blanket scan, so ordinary words colliding with a short form stay intact;
//   - a short form with exactly one active row expands in place;
//   - a short form with several active rows is reported under Ambiguous and
//     left untouched — choosing a meaning is the caller's (or the user's) job;
//   - candidates with no row land in Potential.
func Expand(text string, actives []*types.Abbreviation) *ExpandResult {
	res := &ExpandResult{Original: text, Expanded: text}
	if text == "" {
		return res
	}
	// Note: an empty dictionary must NOT return early — candidates are still
	// detected and reported under Potential (AIRAG surfaces them the same way).

	byShort := map[string][]*types.Abbreviation{}
	for _, a := range actives {
		key := strings.ToLower(a.ShortForm)
		byShort[key] = append(byShort[key], a)
	}

	for _, token := range extractWordTokens(text) {
		if !IsLikelyAbbreviation(token) {
			continue
		}
		matches := byShort[strings.ToLower(token)]
		switch {
		case len(matches) == 0:
			res.Potential = append(res.Potential, token)
		case len(matches) == 1:
			full := matches[0].FullForm
			if expanded := replaceAbbreviation(res.Expanded, token, full); expanded != res.Expanded {
				res.Expanded = expanded
				res.Applied = append(res.Applied, AppliedAbbr{ShortForm: token, FullForm: full})
			}
		default:
			if res.Ambiguous == nil {
				res.Ambiguous = map[string][]Meaning{}
			}
			meanings := make([]Meaning, 0, len(matches))
			for _, m := range matches {
				meanings = append(meanings, Meaning{FullForm: m.FullForm, Description: m.Description})
			}
			res.Ambiguous[token] = meanings
		}
	}
	return res
}

// replaceAbbreviation substitutes whole-word, case-insensitive occurrences of
// short with full. It ports the AIRAG pattern
//
//	(?<!/)(?<!-)\b<short>\b(?!/)(?!-)
//
// which Go's regexp package cannot express (no lookaround): instead we find
// case-insensitive occurrences and check the surrounding runes ourselves.
// A match is kept only when both neighbours are absent or non-word runes AND
// neither is '/' or '-' — so document numbers like "172/GM-UBND" never get
// their segments expanded.
func replaceAbbreviation(text, short, full string) string {
	re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(short))
	locs := re.FindAllStringIndex(text, -1)
	if len(locs) == 0 {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + len(locs)*(len(full)-len(short)))
	cursor := 0
	for _, loc := range locs {
		start, end := loc[0], loc[1]
		if !isExpansionBoundary(text, start, end) {
			continue
		}
		b.WriteString(text[cursor:start])
		b.WriteString(full)
		cursor = end
	}
	if cursor == 0 {
		return text
	}
	b.WriteString(text[cursor:])
	return b.String()
}

// isExpansionBoundary reports whether the byte range [start,end) sits on a
// safe expansion boundary: the rune immediately before/after (when present)
// must be a non-word rune other than '/' or '-'.
func isExpansionBoundary(text string, start, end int) bool {
	if start > 0 {
		prev, _ := utf8.DecodeLastRuneInString(text[:start])
		if isWordRune(prev) || prev == '/' || prev == '-' {
			return false
		}
	}
	if end < len(text) {
		next, _ := utf8.DecodeRuneInString(text[end:])
		if isWordRune(next) || next == '/' || next == '-' {
			return false
		}
	}
	return true
}

// SortByLengthDesc orders the dictionary longest-short-form first so callers
// processing overlapping entries (e.g. "AI" vs "AIE") prefer the longer one.
// Kept for parity with AIRAG's expand_ab_in_text ordering.
func SortByLengthDesc(actives []*types.Abbreviation) {
	sort.SliceStable(actives, func(i, j int) bool {
		return len(actives[i].ShortForm) > len(actives[j].ShortForm)
	})
}
