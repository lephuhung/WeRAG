// Package abbreviation ports AIRAG's Vietnamese abbreviation handling:
// candidate detection heuristics plus the bounded expander that refuses to
// guess when a short form has several active meanings.
package abbreviation

import (
	"unicode"
	"unicode/utf8"
)

// viStopWords mirrors AIRAG _VI_STOP_WORDS — common Vietnamese words that are
// never abbreviations.
var viStopWords = map[string]struct{}{
	"là": {}, "và": {}, "của": {}, "có": {}, "cho": {}, "này": {}, "đó": {}, "với": {},
	"các": {}, "được": {}, "theo": {}, "trong": {}, "về": {}, "từ": {}, "đến": {},
	"khi": {}, "nào": {}, "như": {}, "hay": {}, "hoặc": {}, "nếu": {}, "thì": {},
	"sẽ": {}, "đã": {}, "đang": {}, "tôi": {}, "bạn": {}, "anh": {}, "chị": {},
	"gì": {}, "sao": {}, "thế": {}, "nên": {}, "nhưng": {}, "mà": {},
	"ra": {}, "vào": {}, "lên": {}, "xuống": {}, "qua": {}, "lại": {},
	"một": {}, "hai": {}, "ba": {}, "rất": {}, "cũng": {}, "vẫn": {}, "chỉ": {},
	"không": {}, "phải": {}, "biết": {}, "thân": {}, "hỏi": {},
	// common 2-letter words that look like abbreviations
	"bộ": {}, "bạ": {}, "mì": {}, "tả": {}, "tấ": {},
}

// viVowels mirrors AIRAG _VI_VOWELS (includes the toned vowels).
var viVowels = func() map[rune]struct{} {
	m := make(map[rune]struct{})
	for _, r := range "aeiouy" +
		"àáảãạăắằẳẵặâấầẩẫậ" +
		"èéẻẽẹêếềểễệ" +
		"ìíỉĩị" +
		"òóỏõọôốồổỗộơớờởỡợ" +
		"ùúủũụưứừửữự" +
		"ỳýỷỹỵ" {
		m[r] = struct{}{}
	}
	return m
}()

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// extractWordTokens returns word tokens (letter/digit/underscore runs of
// length >= 2) in order of appearance, de-duplicated.
func extractWordTokens(text string) []string {
	seen := map[string]struct{}{}
	var tokens []string
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		tok := text[start:end]
		start = -1
		if utf8.RuneCountInString(tok) < 2 {
			return
		}
		if _, ok := seen[tok]; ok {
			return
		}
		seen[tok] = struct{}{}
		tokens = append(tokens, tok)
	}
	for i, r := range text {
		if isWordRune(r) {
			if start < 0 {
				start = i
			}
		} else {
			flush(i)
		}
	}
	flush(len(text))
	return tokens
}

// isAllUpperLetters reports whether every rune is an uppercase letter —
// the Go port of Python's `word.isupper() and word.isalpha()`.
func isAllUpperLetters(word string) bool {
	for _, r := range word {
		if !unicode.IsLetter(r) || !unicode.IsUpper(r) {
			return false
		}
	}
	return word != ""
}

// wordTokenSpan is a word token with its UTF-8 byte offsets into the source
// text. Unlike extractWordTokens it keeps every occurrence (no dedupe) so
// callers can record per-occurrence spans.
type wordTokenSpan struct {
	token      string
	start, end int
}

// extractWordTokenSpans returns word tokens (same word definition as
// extractWordTokens: letter/digit/underscore runs of length >= 2) with byte
// offsets, in order of appearance.
func extractWordTokenSpans(text string) []wordTokenSpan {
	var spans []wordTokenSpan
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		tok := text[start:end]
		s := start
		start = -1
		if utf8.RuneCountInString(tok) < 2 {
			return
		}
		spans = append(spans, wordTokenSpan{token: tok, start: s, end: end})
	}
	for i, r := range text {
		if isWordRune(r) {
			if start < 0 {
				start = i
			}
		} else {
			flush(i)
		}
	}
	flush(len(text))
	return spans
}

func FindCandidates(text string) []string {
	var candidates []string
	for _, token := range extractWordTokens(text) {
		if IsLikelyAbbreviation(token) {
			candidates = append(candidates, token)
		}
	}
	return candidates
}

// IsLikelyAbbreviation ports AIRAG _is_likely_abbreviation:
//  1. all-uppercase letters (BMNN, TTGT) → definitely an abbreviation
//  2. Vietnamese stop words → never
//  3. all-lowercase, <= 6 runes, vowel ratio < 0.20 (bmnn, ttgt) → likely
func IsLikelyAbbreviation(word string) bool {
	n := utf8.RuneCountInString(word)
	if n < 2 {
		return false
	}
	if isAllUpperLetters(word) {
		return true
	}
	lower := []rune(word)
	isLower := true
	for i, r := range lower {
		if unicode.IsLetter(r) && !unicode.IsLower(r) {
			isLower = false
		}
		lower[i] = unicode.ToLower(r)
	}
	if _, stop := viStopWords[string(lower)]; stop {
		return false
	}
	if isLower && n <= 6 {
		vowels := 0
		for _, r := range lower {
			if _, ok := viVowels[r]; ok {
				vowels++
			}
		}
		if float64(vowels)/float64(n) < 0.20 {
			return true
		}
	}
	return false
}
