package service

import (
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// Quoted amending text in B's chunks. The vbpl converter renders a quoted
// provision as "> " lines, the first opening with “ (or ") and the last
// closing with ” (or ") and maybe "." / ";":
//
//	2. Sửa đổi, bổ sung khoản 5 Điều 8 Nghị định số 68/2026/NĐ-CP như sau:
//
//	> "5. Sử dụng hóa đơn điện tử
//
//	> a) …".
//
// One lead-in may introduce several quotes, one per target ("Sửa đổi khoản
// 1 và khoản 3 Điều 5 như sau: “1. …” “3. …”"): a group holds them all.

// legalQuoteGroup is a lead-in and the quoted blocks after it.
type legalQuoteGroup struct {
	leadIn string
	// parent is the nearest earlier lead-in naming a Điều, when leadIn
	// names none ("17. Sửa đổi khoản 1, khoản 4 Điều 27 như sau:" over
	// "b) Sửa đổi, bổ sung khoản 4 như sau:")
	parent     string
	leadInSpan LegalRuneSpan
	leadInByte int
	blocks     []legalQuoteBlock
}

// legalQuoteBlock is one quoted text: span covers the runes between the
// quote marks (the "> " markers inside included), text is the clean text.
type legalQuoteBlock struct {
	span  LegalRuneSpan
	text  string
	label string // "điều 5." / "2." / "a)" / ""
}

var (
	quoteOpenRe         = regexp.MustCompile(`^>\s?\s*[“"]`)
	legalNamesArticleRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}])Điều\s+\d`)
	quoteMarkRe         = regexp.MustCompile(`[“"]`)
	quoteCloseRe        = regexp.MustCompile(`[”"]\s*[.;,]*\s*$`)
	// quoteLabelRe reads the label a quoted text starts with.
	quoteLabelRe = regexp.MustCompile(`(?i)^\s*(?:(Điều)\s+(\d+[a-zđ]?)\s*[.:]|(\d+[a-zđ]?)\.(?:\s|$)|([a-zđ])\)(?:\s|$))`)
)

// legalQuoteGroups splits content into lead-ins and their quoted blocks.
func legalQuoteGroups(content string) []legalQuoteGroup {
	var groups []legalQuoteGroup
	var cur *legalQuoteGroup
	type openBlock struct {
		startByte int
		lines     []string
	}
	var open *openBlock
	parent := ""
	runeAt := func(b int) int { return utf8.RuneCountInString(content[:b]) }
	pos := 0
	for pos <= len(content) {
		end := strings.IndexByte(content[pos:], '\n')
		if end < 0 {
			end = len(content)
		} else {
			end += pos
		}
		line := content[pos:end]
		trimmed := strings.TrimSpace(line)
		switch {
		case open != nil:
			if trimmed != "" {
				body := strings.TrimPrefix(strings.TrimPrefix(trimmed, ">"), " ")
				if m := quoteCloseRe.FindStringIndex(line); m != nil && strings.HasPrefix(trimmed, ">") {
					inner := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, ">"), " "))
					inner = quoteCloseRe.ReplaceAllString(inner, "")
					open.lines = append(open.lines, inner)
					cur.blocks = append(cur.blocks, newQuoteBlock(open.lines, runeAt(open.startByte), runeAt(pos+m[0])))
					open = nil
				} else if strings.HasPrefix(trimmed, ">") {
					open.lines = append(open.lines, strings.TrimSpace(body))
				} else {
					// the quote never closed: drop it
					open = nil
					cur = nil
				}
			}
		case trimmed == "":
		case quoteOpenRe.MatchString(trimmed):
			if cur == nil {
				// a quote without a lead-in of its own
				groups = append(groups, legalQuoteGroup{leadInByte: pos, leadInSpan: LegalRuneSpan{runeAt(pos), runeAt(pos)}})
				cur = &groups[len(groups)-1]
			}
			m := quoteMarkRe.FindStringIndex(line)
			startByte := pos + m[1]
			rest := line[m[1]:]
			if c := quoteCloseRe.FindStringIndex(rest); c != nil && strings.TrimSpace(rest[:c[0]]) != "" {
				cur.blocks = append(cur.blocks, newQuoteBlock([]string{strings.TrimSpace(rest[:c[0]])}, runeAt(startByte), runeAt(startByte+c[0])))
			} else {
				open = &openBlock{startByte: startByte, lines: []string{strings.TrimSpace(rest)}}
			}
		case strings.HasPrefix(trimmed, ">"):
			// a quoted line outside a block (continuation of nothing)
		default:
			g := legalQuoteGroup{leadIn: trimmed, leadInByte: pos, leadInSpan: LegalRuneSpan{runeAt(pos), runeAt(end)}}
			if legalNamesArticleRe.MatchString(trimmed) {
				parent = trimmed
			} else {
				g.parent = parent
			}
			groups = append(groups, g)
			cur = &groups[len(groups)-1]
		}
		if end == len(content) {
			break
		}
		pos = end + 1
	}
	out := groups[:0]
	for _, g := range groups {
		if len(g.blocks) > 0 {
			out = append(out, g)
		}
	}
	return out
}

func newQuoteBlock(lines []string, start, end int) legalQuoteBlock {
	var kept []string
	for _, l := range lines {
		if l != "" {
			kept = append(kept, l)
		}
	}
	text := strings.Join(kept, "\n\n")
	return legalQuoteBlock{span: LegalRuneSpan{start, end}, text: text, label: quoteLabel(text)}
}

// quoteLabel is the normalised label a quoted text starts with.
func quoteLabel(text string) string {
	m := quoteLabelRe.FindStringSubmatch(text)
	switch {
	case m == nil:
		return ""
	case m[1] != "":
		return "điều " + strings.ToLower(m[2]) + "."
	case m[3] != "":
		return strings.ToLower(m[3]) + "."
	default:
		return strings.ToLower(m[4]) + ")"
	}
}

// legalQuoteFor finds the quoted text written for an operation on prov
// (bo_sung: the new label under prov) among B's groups. A block counts
// when its label is the expected one; the group whose lead-in names the
// target wins. ok is false when none, or more than one, fits; reason says
// which.
func legalQuoteFor(groups []legalQuoteGroup, prov LegalProvisionRef, newLabel string) (legalQuoteGroup, legalQuoteBlock, string, bool) {
	want := prov
	if newLabel != "" {
		switch {
		case prov.Article == "":
			want = LegalProvisionRef{Article: newLabel}
		case prov.Khoan == "":
			want = LegalProvisionRef{Article: prov.Article, Khoan: newLabel}
		default:
			want = LegalProvisionRef{Article: prov.Article, Khoan: prov.Khoan, Diem: newLabel}
		}
	}
	label := want.label()
	type hit struct {
		g     legalQuoteGroup
		b     legalQuoteBlock
		score int
	}
	var best []hit
	for _, g := range groups {
		lead := g.leadIn
		if g.parent != "" {
			lead = g.parent + " " + g.leadIn
		}
		named := 0
		if prov.Article != "" && legalNamesArticleRe.MatchString(lead) {
			if !legalMentions(lead, "điều", prov.Article) {
				continue // a lead-in about another Điều
			}
			named++
		}
		if want.Khoan != "" && legalMentions(lead, "khoản", want.Khoan) {
			named++
		}
		if want.Diem != "" && legalMentions(lead, "điểm", want.Diem) {
			named++
		}
		for _, b := range g.blocks {
			score := named
			switch {
			case b.label == label:
				score += 10
			case b.label == "" && len(g.blocks) == 1 && named > 0:
				score += 1
			default:
				continue
			}
			if len(best) == 0 || score > best[0].score {
				best = []hit{{g, b, score}}
			} else if score == best[0].score {
				best = append(best, hit{g, b, score})
			}
		}
	}
	switch {
	case len(best) == 0:
		return legalQuoteGroup{}, legalQuoteBlock{}, "không tìm thấy lời văn trích cho " + want.String(), false
	case len(best) > 1:
		return legalQuoteGroup{}, legalQuoteBlock{}, "nhiều đoạn lời văn trích có thể ứng với " + want.String(), false
	}
	return best[0].g, best[0].b, "", true
}

// legalMentions reports whether t names "<keyword> <label>" ("khoản 6"),
// not "khoản 60".
func legalMentions(t, keyword, label string) bool {
	key := keyword + "\x00" + label
	re, ok := legalMentionsCache.Load(key)
	if !ok {
		c, err := regexp.Compile(`(?i)(?:^|[^\p{L}])` + keyword + `\s+` + regexp.QuoteMeta(label) + `(?:[^\p{L}\p{N}]|$)`)
		if err != nil {
			return false
		}
		re, _ = legalMentionsCache.LoadOrStore(key, c)
	}
	return re.(*regexp.Regexp).MatchString(t)
}

// legalMentionsCache holds the compiled legalMentions patterns; keywords
// are few and labels short, so it stays small.
var legalMentionsCache sync.Map
