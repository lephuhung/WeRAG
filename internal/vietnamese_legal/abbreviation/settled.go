package abbreviation

import (
	_ "embed"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/Tencent/WeKnora/internal/types"
)

// common_abbreviations.txt: short forms every reader of Vietnamese
// administrative text knows (UBND, TNHH, BHXH, HIV, MW).
//
//go:embed data/common_abbreviations.txt
var commonAbbreviationsData string

var commonAbbreviations = func() map[string]struct{} {
	m := map[string]struct{}{}
	for _, s := range strings.Split(commonAbbreviationsData, "\n") {
		if s = strings.TrimSpace(s); s != "" && !strings.HasPrefix(s, "#") {
			m[termKey(norm.NFC.String(s))] = struct{}{}
		}
	}
	return m
}()

// IsCommonAbbreviation reports whether word is a widely known short form.
func IsCommonAbbreviation(word string) bool {
	_, ok := commonAbbreviations[termKey(norm.NFC.String(word))]
	return ok
}

var romanNumeral = regexp.MustCompile(`^(?:X{0,3})(?:IX|IV|V?I{0,3})$`)

// isRomanNumeral reports upper-case roman numerals up to XXXIX (cấp III,
// Chương IV, Phụ lục XII).
func isRomanNumeral(w string) bool {
	return w != "" && w == strings.ToUpper(w) && romanNumeral.MatchString(w)
}

// hasLetter reports whether w has at least one letter (2025 has none).
func hasLetter(w string) bool { return strings.IndexFunc(w, unicode.IsLetter) >= 0 }

// hasVowelMark reports a toned or modified vowel (ầ, ư, ơ): written
// Vietnamese words, never short forms, which are bare capitals or bare
// consonant runs (HĐND, hđnd — đ alone is not a vowel mark).
func hasVowelMark(w string) bool {
	for _, r := range norm.NFD.String(strings.ToLower(w)) {
		if unicode.Is(unicode.Mn, r) {
			return true
		}
	}
	return false
}

// definedInText reports whether the question spells the short form out
// itself: "Tập đoàn Dầu khí Việt Nam (PVN)" or "PVN (Tập đoàn Dầu khí …)".
func definedInText(text, short string) bool {
	q := regexp.QuoteMeta(short)
	after := regexp.MustCompile(`(?i)[\p{L}\p{N}][\p{L}\p{N}\s,\-–]{3,}\(\s*` + q + `\s*\)`)
	before := regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])` + q + `\s*\(\s*\p{L}+(?:[\s\-]+\p{L}+)+[^)]*\)`)
	return after.MatchString(text) || before.MatchString(text)
}

// SettledWithoutAsking reports words the gate must not ask about as
// abbreviations: numbers, roman numerals, written Vietnamese words (luật,
// and with detector keys any lower-case word with vowel marks, e.g. tầu),
// short forms spelled out in the question, and widely known short forms the
// tenant dictionary does not define (a dictionary entry still expands them).
// For typo suspects (detectorKey false) a marked word stays open: mưcs is a
// typo to read, not a word to keep.
func SettledWithoutAsking(text, word string, dictionary map[string]bool, detectorKey bool) bool {
	switch {
	case !hasLetter(word), isRomanNumeral(word):
		return true
	case dictionary[termKey(word)]:
		return false
	case IsVietnameseSyllable(word):
		return true
	case detectorKey && hasVowelMark(word) && word == strings.ToLower(word):
		return true
	case IsCommonAbbreviation(word), definedInText(text, word):
		return true
	}
	return false
}

// DropSettled removes the detection's settled words (SettledWithoutAsking)
// from its candidate keys and their typo options. A heuristic detection
// becomes an explicit key set first, as in the typo merge.
func DropSettled(text string, det types.AbbreviationDetection, dictionary map[string]bool) types.AbbreviationDetection {
	keys := det.Keys
	if det.Detector == types.AbbreviationDetectorHeuristic {
		keys = nil
		for _, tok := range FindCandidates(text) {
			keys = append(keys, termKey(tok))
		}
	}
	surface := map[string]string{}
	for _, s := range extractWordTokenSpans(text) {
		if _, ok := surface[termKey(s.token)]; !ok {
			surface[termKey(s.token)] = s.token
		}
	}
	var kept []string
	dropped := map[string]bool{}
	for _, k := range keys {
		w := surface[k]
		if w == "" {
			w = k
		}
		if SettledWithoutAsking(text, w, dictionary, true) {
			dropped[k] = true
			continue
		}
		kept = append(kept, k)
	}
	if len(dropped) == 0 {
		return det
	}
	out := det
	if det.Detector == types.AbbreviationDetectorHeuristic {
		out.Detector = types.AbbreviationDetectorHeuristicSpellcheck
	}
	out.Keys = kept
	out.Typos = nil
	for _, t := range det.Typos {
		if !dropped[termKey(t.Wrong)] {
			out.Typos = append(out.Typos, t)
		}
	}
	return out
}
