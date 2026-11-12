package answerdocx

import (
	"html"
	"regexp"
	"sort"
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
//     parsed recursively over its own range, at most maxLinkNesting deep;
//     brackets holding a link are text, as in marked), images (their alt
//     text read once, without images or links inside it) and
//     delimiter runs of *, _ and ~~;
//  3. emphasis pairs the delimiter runs with the CommonMark "process
//     emphasis" algorithm (openers_bottom, rule of three) on a linked list,
//     recording on each run how many bold/italic/strike spans it opens or
//     closes; a last pass walks the tokens keeping depth counters.

// maxLinkNesting caps links inside link text; deeper brackets are text.
const maxLinkNesting = 4

// knownTagRe matches the HTML tags dropped from text (their content kept):
// a common name and attributes of the name=value shape only, so prose such
// as "a <b and c> d" is left alone. No part of it crosses a "<", so a
// failed match stops at the next one.
var knownTagRe = regexp.MustCompile(`^<(/?)(?i:(a|abbr|address|article|b|big|blockquote|br|caption|center|cite|code|col|colgroup|dd|del|details|dfn|div|dl|dt|em|figcaption|figure|font|footer|h[1-6]|header|hr|i|img|ins|kbd|label|li|mark|nav|ol|p|people_lookup|pre|q|s|samp|section|small|span|strike|strong|sub|summary|sup|table|tbody|td|tfoot|th|thead|think|time|tr|tt|u|ul|var|wbr))` +
	`((?:\s+[a-zA-Z_:][-a-zA-Z0-9_:.]*\s*=\s*(?:"[^"<>]*"|'[^'<>]*'|[^\s"'<>=` + "`" + `]+))*)\s*/?>`)

// maxTagLen bounds the text a tag match looks at (long href URLs fit).
const maxTagLen = 4096

// tagAt matches knownTagRe at s[i] ("<") only when a letter or "/"
// follows and a ">" closes the candidate before the next "<" or line end
// (within maxTagLen): the regex then runs on that candidate alone.
func (p *inlineParser) tagAt(i, b int) []string {
	s := p.s
	if i+1 >= b {
		return nil
	}
	if c := s[i+1]; c != '/' && !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
		return nil
	}
	k := i + 1
	for k < b && k-i < maxTagLen && s[k] != '>' && s[k] != '<' && s[k] != '\n' {
		k++
	}
	if k >= b || s[k] != '>' {
		return nil
	}
	return knownTagRe.FindStringSubmatch(s[i : k+1])
}

// entityRe is an HTML character reference with its semicolon, the only
// form marked decodes: "1826/BNV-CVT&LTNN" stays as written.
var entityRe = regexp.MustCompile(`&(?:#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[A-Za-z][A-Za-z0-9]{1,31});`)

// decodeEntities decodes the character references of plain text.
func decodeEntities(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	return entityRe.ReplaceAllStringFunc(s, func(ref string) string {
		// html also reads legacy names without ";" ("&not" in
		// "&notanentity;"): keep a reference it does not decode whole
		if out := html.UnescapeString(ref); strings.HasPrefix(ref, "&#") || !strings.Contains(out, ";") {
			return out
		}
		return ref
	})
}

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
	// textOnly marks "[" whose text holds a link: as in CommonMark and
	// marked, such brackets are text and the inner link stays a link
	textOnly map[int]bool
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
	return mergeRuns(p.parse(0, len(s), 0, false))
}

// inlineRuns is inline without a context (tests, captions).
func inlineRuns(s string) []docxwrite.Run {
	return (&converter{}).inline(s)
}

// codeSpans returns, for each backtick run that opens a code span when s is
// read left to right, the index just past its closing run: the next run of
// exactly the same length. Inside a span a backslash is literal; outside
// it, when escapes, "\`" is an escaped backtick that opens nothing.
func codeSpans(s string, escapes bool) map[int]int {
	byLen := map[int][]int{} // starts of the raw backtick runs, by length
	for i := 0; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		n := runLen(s, i, '`')
		byLen[n] = append(byLen[n], i)
		i += n
	}
	if len(byLen) == 0 {
		return nil
	}
	out := map[int]int{}
	for i := 0; i < len(s); {
		switch {
		case escapes && s[i] == '\\':
			i += 2
		case s[i] == '`':
			n := runLen(s, i, '`')
			starts := byLen[n]
			if k := sort.SearchInts(starts, i+n); k < len(starts) {
				out[i] = starts[k] + n
				i = starts[k] + n
				continue
			}
			i += n
		default:
			i++
		}
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
	p.bracket, p.paren, p.textOnly = map[int]int{}, map[int]int{}, map[int]bool{}
	var bs, ps []int
	// closed lists the "[" in the order their "]" came (inner before
	// outer), parent the "[" around each one (-1 at the top)
	var closed []int
	parent := map[int]int{}
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
				open := bs[n-1]
				p.bracket[open] = i
				bs = bs[:n-1]
				closed = append(closed, open)
				parent[open] = -1
				if n > 1 {
					parent[open] = bs[n-2]
				}
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
	// a link may not hold another link (an image may): walk the pairs
	// inner first, passing "holds a link" up to the enclosing pair
	holds := map[int]bool{}
	for _, open := range closed {
		image := open > 0 && s[open-1] == '!'
		isLink := false
		if j := p.bracket[open]; j+1 < len(s) && s[j+1] == '(' {
			_, hasDest := p.paren[j+1]
			isLink = hasDest && !image
		}
		if isLink && holds[open] {
			p.textOnly[open] = true
			isLink = false
		}
		if up := parent[open]; up >= 0 && (isLink || holds[open]) {
			holds[up] = true
		}
	}
}

// link reports a link "[text](dest)" whose "[" is at i, inside [i, end).
func (p *inlineParser) link(i, end int) (textEnd int, dest string, next int, ok bool) {
	j, ok := p.bracket[i]
	if !ok || p.textOnly[i] || j+1 >= end || p.s[j+1] != '(' {
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

// parse tokenises s[a:b] and resolves its emphasis. In alt (an image's
// alt text, read once) images and links are text.
func (p *inlineParser) parse(a, b, linkDepth int, alt bool) []docxwrite.Run {
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
		case ch == '!' && !alt && i+1 < b && s[i+1] == '[':
			if te, _, next, ok := p.link(i+1, b); ok {
				flush()
				var altText strings.Builder
				altRuns := p.parse(i+2, te, maxLinkNesting, true)
				for _, r := range altRuns {
					altText.WriteString(r.Text)
				}
				if a := strings.TrimSpace(altText.String()); a != "" {
					plain.WriteString("[Hình: " + a + "]")
				} else {
					plain.WriteString("[Hình]")
				}
				i = next
				continue
			}
		case ch == '[' && !alt && linkDepth < maxLinkNesting:
			if te, dest, next, ok := p.link(i, b); ok {
				flush()
				sub := p.parse(i+1, te, linkDepth+1, false)
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
			if m := p.tagAt(i, b); m != nil {
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
			plain.WriteString(decodeEntities(s[i:k]))
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
