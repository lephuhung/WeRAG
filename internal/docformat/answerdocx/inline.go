package answerdocx

import (
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/docformat/docxwrite"
)

// Inline Markdown of one paragraph, in linear time:
//
//  1. one pass finds the code spans (codeSpans), then one pass matches
//     brackets and parentheses outside them with stacks, so a link is
//     recognised in O(1) and a failed "[" is never rescanned;
//  2. the text is cut into tokens: plain text, code, links (their text
//     parsed recursively over its own range, at most maxLinkNesting deep)
//     and delimiter runs of *, _ and ~~;
//  3. emphasis pairs the delimiter runs with the CommonMark "process
//     emphasis" algorithm (openers_bottom, rule of three) on a linked list,
//     recording on each run how many bold/italic/strike spans it opens or
//     closes; a last pass walks the tokens keeping depth counters.

// maxLinkNesting caps links inside link text; deeper brackets are text.
const maxLinkNesting = 4

// knownTagRe matches the HTML tags dropped from text (their content kept):
// a known name and attributes of the name=value shape only, so prose such
// as "a <b and c> d" is left alone.
var knownTagRe = regexp.MustCompile(`^<(/?)(?i:(b|strong|i|em|u|s|strike|del|ins|sup|sub|span|div|p|br|small|mark|font|center|details|summary|kbd|abbr|cite|q|hr|think|people_lookup))` +
	`((?:\s+[a-zA-Z_:][-a-zA-Z0-9_:.]*\s*=\s*(?:"[^"<>]*"|'[^'<>]*'|[^\s"'<>=` + "`" + `]+))*)\s*/?>`)

// maxTagLen bounds the text a tag match looks at.
const maxTagLen = 512

const (
	stBold = iota
	stItalic
	stStrike
)

type token struct {
	text string
	code bool
	// sub holds a link's text, already parsed
	sub   []docxwrite.Run
	isSub bool
	// delimiter runs: count is what is left unpaired, orig the run length
	delim             byte
	count, orig       int
	canOpen, canClose bool
	opens, closes     [3]int
}

type inlineParser struct {
	s       string
	bud     *budget
	spans   map[int]int // code span start → index past its end
	bracket map[int]int // "[" → its "]"
	paren   map[int]int // "(" → its ")"
}

// inline converts the inline Markdown of one paragraph to runs.
func (c *converter) inline(s string) []docxwrite.Run {
	if !strings.ContainsAny(s, "\\`![<*_~&") {
		if s == "" {
			return nil
		}
		return []docxwrite.Run{{Text: s}}
	}
	p := &inlineParser{s: s, bud: c.bud, spans: codeSpans(s, true)}
	p.matchBrackets()
	return mergeRuns(p.parse(0, len(s), 0))
}

// inlineRuns is inline without a context (tests, captions).
func inlineRuns(s string) []docxwrite.Run {
	return (&converter{}).inline(s)
}

// codeSpans returns, for each backtick run that opens a code span when s is
// read left to right, the index just past its closing run (the next run of
// the same length). Backslash-escaped backticks are skipped when escapes.
func codeSpans(s string, escapes bool) map[int]int {
	type run struct{ pos, n int }
	var runs []run
	for i := 0; i < len(s); {
		switch {
		case escapes && s[i] == '\\':
			i += 2
		case s[i] == '`':
			n := runLen(s, i, '`')
			runs = append(runs, run{i, n})
			i += n
		default:
			i++
		}
	}
	if len(runs) < 2 {
		return nil
	}
	out := map[int]int{}
	next := make([]int, len(runs))
	last := map[int]int{}
	for k := len(runs) - 1; k >= 0; k-- {
		next[k] = -1
		if j, ok := last[runs[k].n]; ok {
			next[k] = j
		}
		last[runs[k].n] = k
	}
	for k := 0; k < len(runs); {
		if j := next[k]; j >= 0 {
			out[runs[k].pos] = runs[j].pos + runs[j].n
			k = j + 1
			continue
		}
		k++
	}
	return out
}

func runLen(s string, i int, ch byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == ch {
		n++
	}
	return n
}

// matchBrackets pairs "[" with "]" and "(" with ")" outside code spans and
// escapes; parentheses do not pair across lines (a link destination cannot
// span one).
func (p *inlineParser) matchBrackets() {
	p.bracket, p.paren = map[int]int{}, map[int]int{}
	var bs, ps []int
	s := p.s
	for i := 0; i < len(s); {
		switch s[i] {
		case '\\':
			i += 2
			continue
		case '`':
			if e, ok := p.spans[i]; ok {
				i = e
				continue
			}
			i += runLen(s, i, '`')
			continue
		case '[':
			bs = append(bs, i)
		case ']':
			if n := len(bs); n > 0 {
				p.bracket[bs[n-1]] = i
				bs = bs[:n-1]
			}
		case '(':
			ps = append(ps, i)
		case ')':
			if n := len(ps); n > 0 {
				p.paren[ps[n-1]] = i
				ps = ps[:n-1]
			}
		case '\n':
			ps = ps[:0]
		}
		i++
	}
}

// link reports a link "[text](dest)" whose "[" is at i, inside [i, end).
func (p *inlineParser) link(i, end int) (textEnd int, dest string, next int, ok bool) {
	j, ok := p.bracket[i]
	if !ok || j+1 >= end || p.s[j+1] != '(' {
		return 0, "", 0, false
	}
	k, ok := p.paren[j+1]
	if !ok || k >= end {
		return 0, "", 0, false
	}
	dest = strings.TrimSpace(p.s[j+2 : k])
	if sp := strings.IndexAny(dest, " \t\n"); sp >= 0 {
		dest = dest[:sp]
	}
	dest = strings.TrimSuffix(strings.TrimPrefix(dest, "<"), ">")
	return j, dest, k + 1, true
}

const escapable = "\\`*_{}[]()#+-.!|~<>\"'&"

// parse tokenises s[a:b] and resolves its emphasis.
func (p *inlineParser) parse(a, b, linkDepth int) []docxwrite.Run {
	s := p.s
	var toks []token
	var plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			toks = append(toks, token{text: plain.String()})
			plain.Reset()
		}
	}
	for i := a; i < b; {
		if p.bud.out() {
			break
		}
		ch := s[i]
		switch {
		case ch == '\\' && i+1 < b && strings.IndexByte(escapable, s[i+1]) >= 0:
			plain.WriteByte(s[i+1])
			i += 2
			continue
		case ch == '`':
			n := runLen(s, i, '`')
			if n > b-i {
				n = b - i
			}
			if e, ok := p.spans[i]; ok && e <= b {
				code := s[i+n : e-n]
				if len(code) > 1 && code[0] == ' ' && code[len(code)-1] == ' ' && strings.TrimSpace(code) != "" {
					code = code[1 : len(code)-1]
				}
				flush()
				toks = append(toks, token{text: strings.ReplaceAll(code, "\n", " "), code: true})
				i = e
				continue
			}
			plain.WriteString(s[i : i+n])
			i += n
			continue
		case ch == '!' && i+1 < b && s[i+1] == '[':
			if te, _, next, ok := p.link(i+1, b); ok {
				flush()
				var alt strings.Builder
				for _, r := range p.parse(i+2, te, maxLinkNesting) {
					alt.WriteString(r.Text)
				}
				if a := strings.TrimSpace(alt.String()); a != "" {
					plain.WriteString("[Hình: " + a + "]")
				} else {
					plain.WriteString("[Hình]")
				}
				i = next
				continue
			}
		case ch == '[' && linkDepth < maxLinkNesting:
			if te, dest, next, ok := p.link(i, b); ok {
				flush()
				sub := p.parse(i+1, te, linkDepth+1)
				toks = append(toks, token{sub: sub, isSub: true})
				var shown strings.Builder
				for _, r := range sub {
					shown.WriteString(r.Text)
				}
				if dest != "" && dest != "#" && strings.TrimSpace(shown.String()) != dest {
					plain.WriteString(" (" + dest + ")")
				}
				i = next
				continue
			}
		case ch == '<':
			// autolink: no space or "<" before the ">"
			k := i + 1
			for k < b && k-i < maxTagLen && s[k] != '>' && s[k] != '<' && s[k] != ' ' && s[k] != '\n' {
				k++
			}
			if k < b && s[k] == '>' {
				inner := s[i+1 : k]
				if strings.HasPrefix(inner, "http://") || strings.HasPrefix(inner, "https://") || strings.HasPrefix(inner, "mailto:") {
					plain.WriteString(strings.TrimPrefix(inner, "mailto:"))
					i = k + 1
					continue
				}
			}
			if m := knownTagRe.FindStringSubmatch(s[i:min(b, i+maxTagLen)]); m != nil {
				if strings.EqualFold(m[2], "br") {
					plain.WriteByte('\n')
				}
				i += len(m[0])
				continue
			}
		case ch == '*' || ch == '_' || ch == '~':
			n := runLen(s, i, ch)
			if i+n > b {
				n = b - i
			}
			if ch == '~' && n != 2 {
				plain.WriteString(s[i : i+n])
				i += n
				continue
			}
			flush()
			toks = append(toks, delimiterToken(s, a, b, i, n))
			i += n
			continue
		default:
			// copy up to the next character that may start markup
			k := i + 1
			for k < b && strings.IndexByte("\\`![<*_~", s[k]) < 0 {
				k++
			}
			plain.WriteString(html.UnescapeString(s[i:k]))
			i = k
			continue
		}
		plain.WriteByte(ch)
		i++
	}
	flush()
	processEmphasis(toks, p.bud)
	return renderTokens(toks)
}

func isPunct(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) }

// delimiterToken classifies the run s[i:i+n] by the CommonMark flanking
// rules, the range [a, b) bounding what counts as before and after.
func delimiterToken(s string, a, b, i, n int) token {
	before, after := ' ', ' '
	if i > a {
		before, _ = utf8.DecodeLastRuneInString(s[a:i])
	}
	if i+n < b {
		after, _ = utf8.DecodeRuneInString(s[i+n : b])
	}
	left := !unicode.IsSpace(after) && (!isPunct(after) || unicode.IsSpace(before) || isPunct(before))
	right := !unicode.IsSpace(before) && (!isPunct(before) || unicode.IsSpace(after) || isPunct(after))
	t := token{text: s[i : i+n], delim: s[i], count: n, orig: n, canOpen: left, canClose: right}
	if s[i] == '_' {
		t.canOpen = left && (!right || isPunct(before))
		t.canClose = right && (!left || isPunct(after))
	}
	return t
}

// processEmphasis pairs delimiter runs (CommonMark §6.2 "process
// emphasis"). The delimiters form a linked list; openersBottom bounds each
// backward search so the whole pass stays linear.
func processEmphasis(toks []token, bud *budget) {
	var idx []int
	for k := range toks {
		if toks[k].delim != 0 {
			idx = append(idx, k)
		}
	}
	if len(idx) < 2 {
		return
	}
	prev := make([]int, len(idx))
	next := make([]int, len(idx))
	for d := range idx {
		prev[d], next[d] = d-1, d+1
	}
	removed := make([]bool, len(idx))
	remove := func(d int) {
		if removed[d] {
			return
		}
		removed[d] = true
		if prev[d] >= 0 {
			next[prev[d]] = next[d]
		}
		if next[d] < len(idx) {
			prev[next[d]] = prev[d]
		}
	}
	type key struct {
		ch      byte
		mod     int
		canOpen bool
	}
	bottom := map[key]int{}
	for d := 0; d < len(idx); d = next[d] {
		if bud.out() {
			return
		}
		c := &toks[idx[d]]
		if !c.canClose {
			continue
		}
		for c.count > 0 {
			k := key{c.delim, c.orig % 3, c.canOpen}
			lb, ok := bottom[k]
			if !ok {
				lb = -1
			}
			found := -1
			for o := prev[d]; o > lb; o = prev[o] {
				ot := &toks[idx[o]]
				if ot.delim != c.delim || !ot.canOpen || ot.count == 0 {
					continue
				}
				if c.delim != '~' && (ot.canClose || c.canOpen) && (ot.orig+c.orig)%3 == 0 &&
					!(ot.orig%3 == 0 && c.orig%3 == 0) {
					continue
				}
				found = o
				break
			}
			if found < 0 {
				bottom[k] = prev[d]
				if !c.canOpen {
					remove(d)
				}
				break
			}
			o := &toks[idx[found]]
			use, st := 1, stItalic
			switch {
			case c.delim == '~':
				use, st = 2, stStrike
			case o.count >= 2 && c.count >= 2:
				use, st = 2, stBold
			}
			o.opens[st]++
			c.closes[st]++
			o.count -= use
			c.count -= use
			for m := next[found]; m != d; {
				nm := next[m]
				remove(m)
				m = nm
			}
			if o.count == 0 {
				remove(found)
			}
		}
		if c.count == 0 {
			remove(d)
		}
	}
}

// renderTokens walks the tokens with bold/italic/strike depth counters.
func renderTokens(toks []token) []docxwrite.Run {
	var depth [3]int
	var runs []docxwrite.Run
	add := func(r docxwrite.Run) {
		r.Bold = r.Bold || depth[stBold] > 0
		r.Italic = r.Italic || depth[stItalic] > 0
		r.Strike = r.Strike || depth[stStrike] > 0
		runs = append(runs, r)
	}
	for _, t := range toks {
		switch {
		case t.delim != 0:
			for st := range depth {
				depth[st] -= t.closes[st]
			}
			if t.count > 0 {
				add(docxwrite.Run{Text: strings.Repeat(string(t.delim), t.count)})
			}
			for st := range depth {
				depth[st] += t.opens[st]
			}
		case t.isSub:
			for _, r := range t.sub {
				add(r)
			}
		default:
			add(docxwrite.Run{Text: t.text, Code: t.code})
		}
	}
	return runs
}

func mergeRuns(runs []docxwrite.Run) []docxwrite.Run {
	var out []docxwrite.Run
	var text strings.Builder
	same := func(a, b docxwrite.Run) bool {
		return a.Bold == b.Bold && a.Italic == b.Italic && a.Strike == b.Strike && a.Code == b.Code
	}
	for i, r := range runs {
		if r.Text == "" {
			continue
		}
		text.WriteString(r.Text)
		// flush when the next non-empty run is formatted differently
		j := i + 1
		for j < len(runs) && runs[j].Text == "" {
			j++
		}
		if j == len(runs) || !same(r, runs[j]) {
			r.Text = text.String()
			text.Reset()
			out = append(out, r)
		}
	}
	return out
}
