package abbreviation

import "strings"

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
