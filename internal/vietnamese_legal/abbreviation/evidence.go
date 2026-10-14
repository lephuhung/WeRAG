package abbreviation

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// TypoEvidence is what each source read a suspect word as. Empty means the
// source saw no typo (or did not run).
type TypoEvidence struct {
	Word string
	// Spellcheck: the spell-check model's correction.
	Spellcheck string
	// Rewrite: the word in the chat model's rewritten question; RewriteRan
	// whether that model answered at all; RewriteAbbreviation that it
	// spelled the word out or made it another abbreviation — a sign of an
	// abbreviation, not a typo.
	Rewrite             string
	RewriteRan          bool
	RewriteAbbreviation bool
	// Decoded: the syllable the keys spell with the input method off.
	Decoded string
	// Dictionary: an active short form one key away (ubnf → UBND).
	Dictionary string
	// Memory: what users of the tenant answered before for this word.
	Memory     string
	MemoryKeep bool
	MemoryAbbr bool
	// Ask: the user asked to be asked (they rejected an automatic reading).
	Ask bool
}

// TypoVerdict is the gate's decision for a suspect word.
type TypoVerdict int

const (
	// TypoNone: no typo evidence; the word stays whatever the detector says.
	TypoNone TypoVerdict = iota
	// TypoCorrect: read the word as the correction without asking.
	TypoCorrect
	// TypoAsk: ask the user, offering the readings.
	TypoAsk
	// TypoAbbreviation: treat as an abbreviation candidate (dictionary or
	// ask its meaning).
	TypoAbbreviation
	// TypoKeep: the word is right as typed; not a candidate.
	TypoKeep
)

// DecideTypo fuses the evidence. The gate corrects on its own only when
// two independent sources give the same reading (or users of the tenant
// confirmed it before); it asks when the sources disagree or only one
// sees a typo; and it treats the word as an abbreviation when the chat
// model spells it out. The chat model's silence vetoes a lone spell-check
// guess (it flags abbreviations such as ttcp → "tập").
func DecideTypo(e TypoEvidence) (TypoVerdict, string, []string) {
	if !e.Ask {
		switch {
		case e.MemoryKeep:
			return TypoKeep, "", nil
		case e.Memory != "":
			return TypoCorrect, e.Memory, nil
		case e.MemoryAbbr:
			return TypoAbbreviation, "", nil
		}
	}
	type reading struct {
		text  string
		votes int
	}
	var readings []*reading
	byKey := map[string]*reading{}
	for _, s := range []string{e.Dictionary, e.Rewrite, e.Spellcheck, e.Decoded, e.Memory} {
		s = strings.TrimSpace(s)
		if s == "" || termKey(s) == termKey(e.Word) {
			continue
		}
		k := norm.NFC.String(strings.ToLower(s))
		if r := byKey[k]; r != nil {
			r.votes++
			continue
		}
		r := &reading{text: s, votes: 1}
		byKey[k] = r
		readings = append(readings, r)
	}
	if !e.Ask {
		for _, r := range readings {
			if r.votes >= 2 {
				return TypoCorrect, r.text, nil
			}
		}
	}
	options := make([]string, 0, len(readings))
	for _, r := range readings {
		options = append(options, r.text)
	}
	switch {
	case e.Ask:
		return TypoAsk, "", options
	case e.RewriteAbbreviation && e.Decoded == "" && e.Dictionary == "":
		return TypoAbbreviation, "", nil
	case len(readings) == 0:
		return TypoNone, "", nil
	case e.RewriteRan && e.Rewrite == "" && e.Decoded == "" && e.Dictionary == "":
		// only the spell-check model sees a typo; the chat model kept the word
		return TypoNone, "", nil
	}
	return TypoAsk, "", options
}

// RewriteReadings aligns the original question with the chat model's
// rewrite and returns, per word of words, what the rewrite has in its
// place: "" when unchanged (or only re-cased), the replacement (one or
// two words), or abbr=true when it became three words or more — the word
// was spelled out.
func RewriteReadings(original, rewrite string, words []string) map[string]RewriteReading {
	a, b := extractWordTokenSpans(original), extractWordTokenSpans(rewrite)
	// longest common subsequence on word keys
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if termKey(a[i].token) == termKey(b[j].token) {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	// replacement segment of every unmatched original word
	repl := map[int][]string{}
	i, j := 0, 0
	for i < n {
		if j < m && termKey(a[i].token) == termKey(b[j].token) {
			i++
			j++
			continue
		}
		// gather the unmatched run on both sides
		si, sj := i, j
		for i < n && (j >= m || termKey(a[i].token) != termKey(b[j].token)) && (j >= m || lcs[i+1][j] >= lcs[i][j+1]) {
			i++
		}
		for j < m && (i >= n || termKey(a[i].token) != termKey(b[j].token)) {
			j++
		}
		var seg []string
		for _, s := range b[sj:j] {
			seg = append(seg, s.token)
		}
		for k := si; k < i; k++ {
			if k == si {
				repl[k] = seg
			} else {
				repl[k] = nil // folded into the first word of the run
			}
		}
	}
	want := map[string]bool{}
	for _, w := range words {
		want[termKey(w)] = true
	}
	out := map[string]RewriteReading{}
	for k, s := range a {
		key := termKey(s.token)
		if !want[key] {
			continue
		}
		seg, changed := repl[k]
		if _, done := out[key]; done {
			continue
		}
		switch {
		case !changed:
			out[key] = RewriteReading{}
		case len(seg) >= 3:
			out[key] = RewriteReading{Abbreviation: true}
		default:
			out[key] = RewriteReading{Text: strings.Join(seg, " ")}
		}
	}
	return out
}

// RewriteReading is the rewrite's reading of one word.
type RewriteReading struct {
	Text         string
	Abbreviation bool
}

// NearShortForm returns an active short form one key away from word
// (one substituted, missing, extra or swapped character), case-insensitive;
// "" when none or several.
func NearShortForm(word string, shortForms []string) string {
	w := []rune(strings.ToLower(word))
	found := ""
	for _, sf := range shortForms {
		s := []rune(strings.ToLower(sf))
		if string(s) == string(w) || !oneEditApart(w, s) {
			continue
		}
		if found != "" && !strings.EqualFold(found, sf) {
			return ""
		}
		found = sf
	}
	return found
}

func oneEditApart(a, b []rune) bool {
	if len(a) < 3 || len(b) < 3 {
		return false
	}
	switch len(a) - len(b) {
	case 0:
		diff := []int{}
		for i := range a {
			if a[i] != b[i] {
				diff = append(diff, i)
			}
		}
		if len(diff) == 1 {
			return true
		}
		return len(diff) == 2 && diff[1] == diff[0]+1 && a[diff[0]] == b[diff[1]] && a[diff[1]] == b[diff[0]]
	case 1, -1:
		if len(a) < len(b) {
			a, b = b, a
		}
		for i := range b {
			if a[i] != b[i] {
				return string(a[i+1:]) == string(b[i:])
			}
		}
		return true
	}
	return false
}
