package agent

import (
	"regexp"
	"strings"
	"unicode"
)

// Not a button but the same failure: after a legal basis check the model
// said the citations "đã được gạch chân" while nothing was (its card
// underlines on request). In such a turn — no tool sent underlines — the
// claim is cut from the answer:
//   - in a list item or a table row only the claim goes (its parenthesis
//     or its clause), the item and the other cells stay;
//   - in prose the clause claiming it goes, or the whole sentence when the
//     claim is its main clause.
// "đã đánh dấu hết hiệu lực" (a status), "đã tô màu bảng" (no citation)
// are not claims.

// underlineClaimRe finds a candidate claim; underlineClaimAt confirms it.
var underlineClaimRe = regexp.MustCompile(`(?i)đã (?:được )?(gạch chân|highlight|đánh dấu|tô (?:vàng|màu|sáng|đỏ))`)

// markStatusWords after "đã (được) đánh dấu" make it a status ("được đánh
// dấu hết hiệu lực trong danh mục"), not something done to the document.
var markStatusWords = map[string]bool{"là": true, "hết": true, "còn": true, "cần": true, "chưa": true}

// citationObjectRe: what a "tô màu" claim must be about.
var citationObjectRe = regexp.MustCompile(`(?i)chỗ|viện dẫn|văn bản|căn cứ|số hiệu|vị trí|trích dẫn|điều khoản`)

// clauseSepRe splits a sentence into clauses.
var clauseSepRe = regexp.MustCompile(`\s*[,;]\s*|\s+[–—]\s+`)

// listItemRe opens a list item.
var listItemRe = regexp.MustCompile(`^\s*(?:[-*•+]|\d+[.)])\s+`)

// underlineClaimAt is the first claim of text as [start, end), nil when
// none.
func underlineClaimAt(text string) []int {
	for _, m := range underlineClaimRe.FindAllStringSubmatchIndex(text, -1) {
		verb := strings.ToLower(text[m[2]:m[3]])
		switch {
		case verb == "đánh dấu":
			if next := strings.Fields(text[m[1]:]); len(next) > 0 && markStatusWords[strings.ToLower(strings.Trim(next[0], ".,;:!?"))] {
				continue
			}
		case strings.HasPrefix(verb, "tô"):
			if !citationObjectRe.MatchString(text) {
				continue
			}
		}
		return []int{m[0], m[1]}
	}
	return nil
}

func hasLetter(s string) bool { return strings.ContainsFunc(s, unicode.IsLetter) }

// cutClaim removes the claims of a list item's text or a table cell: a
// parenthesis holding one, else the clause holding one.
func cutClaim(text string) string {
	for i := 0; i < 8; i++ {
		at := underlineClaimAt(text)
		if at == nil {
			break
		}
		open := strings.LastIndex(text[:at[0]], "(")
		if open >= 0 && !strings.Contains(text[open:at[0]], ")") {
			if close := strings.Index(text[at[1]:], ")"); close >= 0 {
				end := at[1] + close + 1
				text = strings.TrimRight(text[:open], " ") + text[end:]
				continue
			}
		}
		start, end := 0, len(text)
		for _, loc := range clauseSepRe.FindAllStringIndex(text, -1) {
			if loc[1] <= at[0] {
				start = loc[0]
			} else if loc[0] >= at[1] {
				end = loc[0]
				break
			}
		}
		if start == 0 {
			// the claim opens the text: drop its clause and the separator after it
			if loc := clauseSepRe.FindStringIndex(text[end:]); loc != nil && loc[0] == 0 {
				end += loc[1]
			}
		}
		text = text[:start] + text[end:]
	}
	return text
}

// stripUnderlineClaim drops the underline claims of unit (see above) when
// the turn ran the legal basis check and no tool sent underlines; "" when
// nothing is left. changed reports a cut.
func stripUnderlineClaim(unit string, b turnButtons) (out string, changed bool) {
	if !b.legalBasis || b.marks || underlineClaimAt(unit) == nil {
		return unit, false
	}
	body := strings.TrimRight(unit, " \n")
	tail := unit[len(body):]
	// a table row: cut inside the cells, keep them all
	if strings.Count(body, "|") >= 2 {
		cells := strings.Split(body, "|")
		for i, c := range cells {
			if underlineClaimAt(c) != nil {
				cut := cutClaim(c)
				if strings.HasPrefix(c, " ") && !strings.HasPrefix(cut, " ") {
					cut = " " + cut
				}
				if strings.HasSuffix(c, " ") && !strings.HasSuffix(cut, " ") {
					cut += " "
				}
				cells[i] = cut
			}
		}
		return strings.Join(cells, "|") + tail, true
	}
	// a list item: keep the item, cut the claim
	if loc := listItemRe.FindStringIndex(body); loc != nil {
		rest := strings.TrimRight(cutClaim(body[loc[1]:]), " ,;")
		if !hasLetter(rest) {
			return "", true
		}
		return body[:loc[1]] + rest + tail, true
	}
	// prose
	end := ""
	if n := len(body); n > 0 && strings.ContainsRune(".!?", rune(body[n-1])) {
		body, end = body[:n-1], body[n-1:]
	}
	cuts := clauseSepRe.FindAllStringIndex(body, -1)
	var clauses []clause
	last, sep := 0, ""
	for _, c := range cuts {
		clauses = append(clauses, clause{body[last:c[0]], sep})
		sep, last = body[c[0]:c[1]], c[1]
	}
	clauses = append(clauses, clause{body[last:], sep})
	var kept []clause
	for i, c := range clauses {
		if underlineClaimAt(c.text) == nil {
			kept = append(kept, c)
			continue
		}
		if i == 0 || mainClauseClaim(c.text, clauses[:i]) {
			// the claim is what the sentence says
			return "", true
		}
	}
	var sb strings.Builder
	for i, c := range kept {
		if i > 0 {
			sb.WriteString(c.sep)
		}
		sb.WriteString(c.text)
	}
	rest := strings.TrimSpace(sb.String())
	if !hasLetter(rest) {
		return "", true
	}
	return rest + end + tail, true
}

// clause is one clause of a sentence and the separator before it.
type clause struct{ text, sep string }

// mainClauseClaim reports a claim clause that is the predicate of the
// clauses before it ("Luật A, Luật B, đã được gạch chân"): it opens with
// "đã" and those are short list items. Cutting it would leave a fragment.
func mainClauseClaim(claim string, before []clause) bool {
	l := strings.ToLower(strings.TrimSpace(claim))
	if !strings.HasPrefix(l, "đã ") && !strings.HasPrefix(l, "đều đã ") && !strings.HasPrefix(l, "và đã ") && !strings.HasPrefix(l, "cũng đã ") {
		return false
	}
	for _, c := range before {
		if len(strings.Fields(c.text)) > 4 {
			return false
		}
	}
	return true
}
