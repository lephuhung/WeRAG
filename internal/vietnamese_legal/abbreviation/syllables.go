package abbreviation

import (
	_ "embed"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// vi_syllables.txt: Vietnamese syllables seen in clean corpora
// (training/spellcheck/gen_syllables.py), lower case, NFC.
//
//go:embed data/vi_syllables.txt
var viSyllablesData string

var viSyllables = func() map[string]struct{} {
	m := make(map[string]struct{}, 6000)
	for _, s := range strings.Split(viSyllablesData, "\n") {
		if s = strings.TrimSpace(s); s != "" {
			m[s] = struct{}{}
		}
	}
	return m
}()

// IsVietnameseSyllable reports whether word (any case) is a known
// Vietnamese syllable.
func IsVietnameseSyllable(word string) bool {
	_, ok := viSyllables[norm.NFC.String(strings.ToLower(word))]
	return ok
}

// SuspectWords lists the words of text the gate should look at more
// closely: not Vietnamese syllables, on an expansion boundary, without
// digits, distinct, in order. They are typos (cascc), abbreviations (ubnd)
// or foreign words and names (iphone); other sources tell which.
func SuspectWords(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range extractWordTokenSpans(text) {
		key := termKey(s.token)
		if seen[key] || utf8Len(s.token) < 2 || strings.IndexFunc(s.token, unicode.IsDigit) >= 0 {
			continue
		}
		seen[key] = true
		if !isExpansionBoundary(text, s.start, s.end) || IsVietnameseSyllable(s.token) {
			continue
		}
		out = append(out, s.token)
	}
	return out
}

func utf8Len(s string) int { return len([]rune(s)) }

const (
	toneAcute = '́'
	toneGrave = '̀'
	toneHook  = '̉'
	toneTilde = '̃'
	toneDot   = '̣'
)

var telexTones = map[rune]rune{'s': toneAcute, 'f': toneGrave, 'r': toneHook, 'x': toneTilde, 'j': toneDot}
var vniTones = map[rune]rune{'1': toneAcute, '2': toneGrave, '3': toneHook, '4': toneTilde, '5': toneDot}

func isBaseVowel(r rune) bool { return strings.ContainsRune("aăâeêioôơuưy", r) }

// DecodeTyping reads word as keys typed with the Vietnamese input method
// off (Telex: cacs, cascc, mootj, ddang; VNI: ca1c, la6p5) and returns the
// syllable they spell, when that is a known Vietnamese syllable different
// from word. One doubled key (cascc, cassc) is forgiven.
func DecodeTyping(word string) (string, bool) {
	w := strings.ToLower(word)
	if IsVietnameseSyllable(w) {
		return "", false
	}
	try := func(keys string) (string, bool) {
		if s, ok := decodeKeys(keys); ok && IsVietnameseSyllable(s) && s != w {
			return s, true
		}
		return "", false
	}
	if s, ok := try(w); ok {
		return s, true
	}
	rs := []rune(w)
	for i := 1; i < len(rs); i++ {
		if rs[i] == rs[i-1] {
			if s, ok := try(string(rs[:i]) + string(rs[i+1:])); ok {
				return s, true
			}
		}
	}
	return "", false
}

// decodeKeys applies Telex/VNI keys: tone keys after a vowel, aa/ee/oo
// (or 6) for the circumflex, w (or 7/8) for horn and breve, dd (or 9) for
// đ. The tone goes on the vowel that makes a known syllable.
func decodeKeys(keys string) (string, bool) {
	var base []rune
	var tone rune
	seenVowel := false
	for _, r := range keys {
		switch {
		case !seenVowel && r == 'd' && len(base) == 1 && base[0] == 'd',
			r == '9' && len(base) > 0 && base[0] == 'd':
			base[0] = 'đ'
		case seenVowel && tone == 0 && telexTones[r] != 0:
			tone = telexTones[r]
		case vniTones[r] != 0:
			if !seenVowel || tone != 0 {
				return "", false
			}
			tone = vniTones[r]
		case r == 'w' || r == '7' || r == '8':
			if !applyHorn(base, r) {
				return "", false
			}
		case r == '6':
			if !applyCircumflex(base, 0) {
				return "", false
			}
		case (r == 'a' || r == 'e' || r == 'o') && seenVowel && len(base) > 0 && lastVowelIs(base, r):
			applyCircumflex(base, r)
		case unicode.IsLetter(r):
			base = append(base, r)
			if isBaseVowel(r) {
				seenVowel = true
			}
		default:
			return "", false
		}
	}
	if !seenVowel {
		return "", false
	}
	plain := string(base)
	if tone == 0 {
		return plain, true
	}
	// try the tone on each vowel, last first
	for i := len(base) - 1; i >= 0; i-- {
		if !isBaseVowel(base[i]) {
			continue
		}
		s := norm.NFC.String(string(base[:i+1]) + string(tone) + string(base[i+1:]))
		if IsVietnameseSyllable(s) {
			return s, true
		}
	}
	return "", false
}

func lastVowelIs(base []rune, v rune) bool {
	for i := len(base) - 1; i >= 0; i-- {
		if isBaseVowel(base[i]) {
			return base[i] == v
		}
	}
	return false
}

// applyCircumflex turns the last a/e/o (or the last one equal to v) into
// â/ê/ô.
func applyCircumflex(base []rune, v rune) bool {
	for i := len(base) - 1; i >= 0; i-- {
		if v != 0 && base[i] != v {
			if isBaseVowel(base[i]) {
				return false
			}
			continue
		}
		switch base[i] {
		case 'a':
			base[i] = 'â'
			return true
		case 'e':
			base[i] = 'ê'
			return true
		case 'o':
			base[i] = 'ô'
			return true
		}
	}
	return false
}

// applyHorn turns uo into ươ, else the last u/o into ư/ơ (w, 7) or the
// last a into ă (w, 8).
func applyHorn(base []rune, key rune) bool {
	for i := len(base) - 1; i >= 0; i-- {
		switch {
		case base[i] == 'o' && key != '8':
			base[i] = 'ơ'
			if i > 0 && base[i-1] == 'u' {
				base[i-1] = 'ư'
			}
			return true
		case base[i] == 'u' && key != '8':
			base[i] = 'ư'
			return true
		case base[i] == 'a' && key != '7':
			base[i] = 'ă'
			return true
		}
	}
	return false
}
