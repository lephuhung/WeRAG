package abbreviation

import (
	"strings"
	"unicode"

	"github.com/Tencent/WeKnora/internal/types"
)

// TypoWords splits a spell-check finding into word-level corrections: a
// single wrong word maps to the whole correction (cascc → các, quyddinh →
// quy định); equal word counts pair up word by word and keep the words
// that differ. Anything else (merged or split phrases) yields nothing.
func TypoWords(wrong, correct string) [][2]string {
	ws, cs := strings.Fields(wrong), strings.Fields(correct)
	if len(ws) == 0 || len(cs) == 0 {
		return nil
	}
	var out [][2]string
	add := func(w, c string) {
		if termKey(w) == termKey(c) {
			return
		}
		if toks := extractWordTokens(w); len(toks) != 1 || toks[0] != w {
			return
		}
		out = append(out, [2]string{w, c})
	}
	switch {
	case len(ws) == 1:
		add(ws[0], strings.Join(cs, " "))
	case len(ws) == len(cs):
		for i := range ws {
			add(ws[i], cs[i])
		}
	}
	return out
}

// TypoSuggestions keeps the corrections that can label a "did you mean"
// choice for text: the word occurs on an expansion boundary, carries no
// digit, and a correction that repeats the words already following it is
// trimmed ("quys định" → "quy định" suggests "quy", not "quy định").
func TypoSuggestions(text string, pairs [][2]string) []types.AbbreviationTypo {
	spans := extractWordTokenSpans(text)
	var out []types.AbbreviationTypo
	seen := map[string]struct{}{}
	for _, p := range pairs {
		key := termKey(p[0])
		if _, dup := seen[key]; dup || strings.IndexFunc(p[0], unicode.IsDigit) >= 0 {
			continue
		}
		for i, s := range spans {
			if termKey(s.token) != key || !isExpansionBoundary(text, s.start, s.end) {
				continue
			}
			correct := strings.Fields(p[1])
			for n := len(correct) - 1; n >= 1; n-- {
				if i+n < len(spans) && sameWords(correct[n:], spans[i+1:]) {
					correct = correct[:n]
					break
				}
			}
			if c := strings.Join(correct, " "); termKey(c) != key {
				seen[key] = struct{}{}
				out = append(out, types.AbbreviationTypo{Wrong: s.token, Correct: c})
			}
			break
		}
	}
	return out
}

func sameWords(words []string, spans []wordTokenSpan) bool {
	if len(words) > len(spans) {
		return false
	}
	for i, w := range words {
		if termKey(w) != termKey(spans[i].token) {
			return false
		}
	}
	return true
}

// TypoSuggestionFor returns the spellcheck guess for an unknown term.
func TypoSuggestionFor(r types.AbbreviationResolution, short string) string {
	key := termKey(short)
	for _, t := range r.TypoSuggestions {
		if termKey(t.Wrong) == key {
			return t.Correct
		}
	}
	return ""
}

// ApplyTypoCorrections records the user's answer that query words are
// typos (or are kept as typed when Correct equals Wrong): each word leaves
// the candidates — its term, unknown entry and detected key — and the
// effective query reads the correction. A heuristic detection becomes an
// explicit key set first so coverage checks keep matching. Every word must
// name an existing term; otherwise the input is returned unchanged.
func ApplyTypoCorrections(r types.AbbreviationResolution, typos []types.AbbreviationTypo) (types.AbbreviationResolution, error) {
	if len(typos) == 0 {
		return r, nil
	}
	out := cloneResolution(r)
	if out.Detector == types.AbbreviationDetectorHeuristic {
		out.Detector = types.AbbreviationDetectorHeuristicSpellcheck
		out.DetectedKeys = out.DetectedKeys[:0]
		for i := range out.Terms {
			out.DetectedKeys = append(out.DetectedKeys, out.Terms[i].Key)
		}
	}
	for _, t := range typos {
		key := termKey(t.Wrong)
		if findTerm(&out, key) == nil || strings.TrimSpace(t.Correct) == "" {
			return r, types.ErrAbbreviationNotFound
		}
		terms := out.Terms[:0]
		for _, term := range out.Terms {
			if term.Key != key {
				terms = append(terms, term)
			}
		}
		out.Terms = terms
		unknown := out.UnknownTerms[:0]
		for _, u := range out.UnknownTerms {
			if termKey(u) != key {
				unknown = append(unknown, u)
			}
		}
		out.UnknownTerms = unknown
		keys := out.DetectedKeys[:0]
		for _, k := range out.DetectedKeys {
			if k != key {
				keys = append(keys, k)
			}
		}
		out.DetectedKeys = keys
		out.TypoCorrections = append(out.TypoCorrections, types.AbbreviationTypo{Wrong: t.Wrong, Correct: strings.TrimSpace(t.Correct)})
	}
	finalizeResolution(&out)
	return out, nil
}

// ValidRewrite reports whether rewrite may be offered as "did you mean"
// for original, where unclear are the words being asked about, keep the
// candidate short forms that must stay as written and known the active
// dictionary short forms. The model is only allowed to fix typing: every
// kept short form, ALL-CAPS word and word with a digit is still there, at
// least one unclear word was changed, the word count grows by at most one
// per unclear word (quyddinh → quy định) — an abbreviation spelled out
// (BMNN → bí mật nhà nước) fails — and a new abbreviation-shaped word must
// be a dictionary short form (ubnf → UBND, not vphc → VPCP).
func ValidRewrite(original, rewrite string, unclear, keep, known []string) bool {
	rewrite = strings.TrimSpace(rewrite)
	if rewrite == "" || strings.Join(strings.Fields(rewrite), " ") == strings.Join(strings.Fields(original), " ") {
		return false
	}
	orig, rw := extractWordTokenSpans(original), extractWordTokenSpans(rewrite)
	if len(rw) > len(orig)+len(unclear) || len(rw)+2 < len(orig) {
		return false
	}
	has := make(map[string]int, len(rw))
	exact := make(map[string]bool, len(rw))
	for _, s := range rw {
		has[termKey(s.token)]++
		exact[s.token] = true
	}
	changed := false
	for _, u := range unclear {
		if has[termKey(u)] == 0 {
			changed = true
		}
	}
	if !changed {
		return false
	}
	for _, k := range keep {
		if has[termKey(k)] == 0 {
			return false
		}
	}
	unclearKey := make(map[string]bool, len(unclear))
	for _, u := range unclear {
		unclearKey[termKey(u)] = true
	}
	inOriginal := make(map[string]bool, len(orig))
	for _, s := range orig {
		inOriginal[termKey(s.token)] = true
	}
	knownKey := make(map[string]bool, len(known))
	for _, k := range known {
		knownKey[termKey(k)] = true
	}
	for _, s := range rw {
		key := termKey(s.token)
		if !inOriginal[key] && !knownKey[key] && IsLikelyAbbreviation(s.token) {
			return false
		}
	}
	for _, s := range orig {
		if unclearKey[termKey(s.token)] {
			continue
		}
		if strings.IndexFunc(s.token, unicode.IsDigit) >= 0 || (isAllUpperLetters(s.token) && len([]rune(s.token)) > 1) {
			if !exact[s.token] {
				return false
			}
		}
	}
	return true
}
