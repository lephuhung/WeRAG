package answerdocx

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/docformat/docxwrite"
)

// The Markdown subset the chat answers use (the web renders them with
// marked, GFM, breaks: true):
//
//   - ATX headings (# … ######) and setext headings (text over === / ---)
//   - paragraphs; a single newline is a line break, as in the chat
//   - **bold**, __bold__, *italic*, _italic_, ***both***, ~~strike~~,
//     `code`, backslash escapes, <br>
//   - links [text](url) as "text (url)", autolinks <https://…>, images as
//     "[Hình: alt]", wiki links [[slug|name]] as name
//   - bulleted (-, *, +) and numbered (1. / 1)) lists, nested by indent,
//     with follow-up paragraphs inside an item
//   - GFM tables with column alignment
//   - block quotes (nested), fenced code blocks (``` and ~~~), indented
//     code is read as text (answers indent lists, not code)
//   - thematic breaks (---, ***, ___)
//
// Raw HTML tags other than <br> are dropped, their text kept; <think>
// reasoning and <people_lookup> envelopes are removed.

// converter turns Markdown lines into docx blocks. listSeq hands out the
// numbering keys of ordered lists so each list restarts at its own start.
type converter struct {
	listSeq int
}

// MarkdownBlocks converts a chat answer's Markdown to docx blocks.
func MarkdownBlocks(md string) []docxwrite.Block {
	md = preprocess(md)
	c := &converter{}
	return c.blocks(splitLines(md), 0)
}

var (
	thinkRe     = regexp.MustCompile(`(?s)<think>.*?(</think>|$)`)
	peopleRe    = regexp.MustCompile(`(?s)<people_lookup>.*?</people_lookup>`)
	wikiLinkRe  = regexp.MustCompile(`\[\[([^\]\n]+)\]\]`)
	headingRe   = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?[ \t]*$`)
	closingHash = regexp.MustCompile(`[ \t]+#+$`)
	ruleRe      = regexp.MustCompile(`^ {0,3}((?:-[ \t]*){3,}|(?:\*[ \t]*){3,}|(?:_[ \t]*){3,})$`)
	fenceRe     = regexp.MustCompile("^( {0,3})(`{3,}|~{3,})(.*)$")
	listItemRe  = regexp.MustCompile(`^( *)([-*+]|\d{1,9}[.)])([ \t]+|$)(.*)$`)
	tableSepRe  = regexp.MustCompile(`^ {0,3}\|?[ \t]*:?-+:?[ \t]*(\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$`)
	setextH1Re  = regexp.MustCompile(`^ {0,3}=+[ \t]*$`)
	setextH2Re  = regexp.MustCompile(`^ {0,3}-+[ \t]*$`)
	quoteRe     = regexp.MustCompile(`^ {0,3}>`)
)

func preprocess(md string) string {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	md = strings.ReplaceAll(md, "\r", "\n")
	if strings.Contains(md, "<think>") {
		md = strings.TrimSpace(thinkRe.ReplaceAllString(md, ""))
	}
	md = peopleRe.ReplaceAllString(md, "")
	return wikiLinkRe.ReplaceAllStringFunc(md, func(m string) string {
		inner := m[2 : len(m)-2]
		if i := strings.Index(inner, "|"); i > 0 {
			return strings.TrimSpace(inner[i+1:])
		}
		slug := strings.TrimSpace(inner)
		if parts := strings.Split(slug, "/"); len(parts) > 1 {
			return strings.Join(parts[1:], "/")
		}
		return slug
	})
}

// splitLines splits md into lines and expands leading tabs to four spaces.
func splitLines(md string) []string {
	lines := strings.Split(md, "\n")
	for i, l := range lines {
		lead := 0
		for lead < len(l) && (l[lead] == ' ' || l[lead] == '\t') {
			lead++
		}
		if strings.Contains(l[:lead], "\t") {
			lines[i] = strings.ReplaceAll(l[:lead], "\t", "    ") + l[lead:]
		}
	}
	return lines
}

func isBlank(l string) bool { return strings.TrimSpace(l) == "" }

func indentOf(l string) int {
	n := 0
	for n < len(l) && l[n] == ' ' {
		n++
	}
	return n
}

// listMarker is a parsed list item line.
type listMarker struct {
	indent  int // spaces before the marker
	ordered bool
	start   int
	content int // column where the item text starts
	text    string
}

func parseListItem(l string) (listMarker, bool) {
	m := listItemRe.FindStringSubmatch(l)
	if m == nil {
		return listMarker{}, false
	}
	// "**bold**" and "***" are not bullets: a bullet needs a space after it.
	if m[3] == "" && m[4] == "" && len(m[2]) == 1 {
		// a lone "-" is an empty item only when nothing follows at all
		if strings.TrimSpace(l) != m[2] {
			return listMarker{}, false
		}
	}
	lm := listMarker{indent: len(m[1]), text: m[4]}
	if c := m[2][0]; c >= '0' && c <= '9' {
		lm.ordered = true
		lm.start, _ = strconv.Atoi(m[2][:len(m[2])-1])
	}
	spaces := len(strings.ReplaceAll(m[3], "\t", "    "))
	if spaces > 4 || spaces == 0 {
		spaces = 1
	}
	lm.content = lm.indent + len(m[2]) + spaces
	return lm, true
}

// startsBlock reports whether l opens a block that interrupts a paragraph.
func startsBlock(l string) bool {
	if headingRe.MatchString(l) || fenceRe.MatchString(l) || quoteRe.MatchString(l) || ruleRe.MatchString(l) {
		return true
	}
	if lm, ok := parseListItem(l); ok && strings.TrimSpace(lm.text) != "" {
		// as in CommonMark, only a list starting at 1 interrupts a paragraph
		return !lm.ordered || lm.start == 1
	}
	return false
}

// blocks converts lines at list depth depth (the level nested list items
// get).
func (c *converter) blocks(lines []string, depth int) []docxwrite.Block {
	var out []docxwrite.Block
	for i := 0; i < len(lines); {
		l := lines[i]
		if isBlank(l) {
			i++
			continue
		}
		if m := fenceRe.FindStringSubmatch(l); m != nil {
			i = c.fencedCode(lines, i, m, &out)
			continue
		}
		if m := headingRe.FindStringSubmatch(l); m != nil {
			text := closingHash.ReplaceAllString(m[2], "")
			if strings.Trim(text, "#") == "" {
				text = ""
			}
			out = append(out, headingParagraph(len(m[1]), text))
			i++
			continue
		}
		if ruleRe.MatchString(l) {
			out = append(out, docxwrite.Paragraph{Style: docxwrite.StyleRule})
			i++
			continue
		}
		if quoteRe.MatchString(l) {
			i = c.quote(lines, i, depth, &out)
			continue
		}
		if i+1 < len(lines) && strings.Contains(l, "|") && tableSepRe.MatchString(lines[i+1]) &&
			strings.Contains(lines[i+1], "-") && len(splitRow(l)) == len(splitRow(lines[i+1])) {
			i = c.table(lines, i, &out)
			continue
		}
		if _, ok := parseListItem(l); ok {
			i = c.list(lines, i, depth, &out)
			continue
		}
		i = c.paragraph(lines, i, &out)
	}
	return out
}

func headingParagraph(level int, text string) docxwrite.Paragraph {
	style := docxwrite.StyleHeading4
	align := "left"
	switch level {
	case 1:
		style, align = docxwrite.StyleHeading1, "" // the title stays centred
	case 2:
		style = docxwrite.StyleHeading2
	case 3:
		style = docxwrite.StyleHeading3
	}
	return docxwrite.Paragraph{Style: style, Align: align, Runs: inlineRuns(text)}
}

func (c *converter) fencedCode(lines []string, i int, m []string, out *[]docxwrite.Block) int {
	indent, fence := len(m[1]), m[2]
	var body []string
	j := i + 1
	for ; j < len(lines); j++ {
		t := strings.TrimSpace(lines[j])
		if strings.HasPrefix(t, fence[:1]) && strings.Trim(t, fence[:1]) == "" && len(t) >= len(fence) {
			j++
			break
		}
		l := lines[j]
		if n := indentOf(l); n > 0 {
			l = l[min(n, indent):]
		}
		body = append(body, l)
	}
	*out = append(*out, docxwrite.Paragraph{Style: docxwrite.StyleCodeBlock,
		Runs: []docxwrite.Run{{Text: strings.Join(body, "\n")}}})
	return j
}

func (c *converter) quote(lines []string, i, depth int, out *[]docxwrite.Block) int {
	var inner []string
	j := i
	for ; j < len(lines); j++ {
		l := lines[j]
		if quoteRe.MatchString(l) {
			l = strings.TrimLeft(l, " ")[1:]
			if strings.HasPrefix(l, " ") {
				l = l[1:]
			}
			inner = append(inner, l)
			continue
		}
		// lazy continuation of a quoted paragraph
		if !isBlank(l) && len(inner) > 0 && !isBlank(inner[len(inner)-1]) && !startsBlock(l) {
			inner = append(inner, l)
			continue
		}
		break
	}
	for _, blk := range c.blocks(inner, depth) {
		if p, ok := blk.(docxwrite.Paragraph); ok {
			if p.Style == "" || p.Style == docxwrite.StyleNormal {
				p.Style = docxwrite.StyleQuote
			} else {
				p.IndentTwips += 284
			}
			if p.Style == docxwrite.StyleQuote && p.IndentTwips > 0 {
				p.IndentTwips += 284
			}
			blk = p
		}
		*out = append(*out, blk)
	}
	return j
}

// list reads one list (items of the same kind at the indent of lines[i])
// and the lines nested in its items.
func (c *converter) list(lines []string, i, depth int, out *[]docxwrite.Block) int {
	first, _ := parseListItem(lines[i])
	c.listSeq++
	key := c.listSeq
	j := i
	for j < len(lines) {
		lm, ok := parseListItem(lines[j])
		if !ok || lm.ordered != first.ordered || lm.indent > first.indent+3 || lm.indent < first.indent {
			break
		}
		// the item's own lines: the first, then indented or lazy lines
		item := []string{lm.text}
		k := j + 1
		childIndent := min(lm.content, lm.indent+2)
		for k < len(lines) {
			l := lines[k]
			if isBlank(l) {
				// a blank line stays inside the item when more of it follows
				n := k + 1
				for n < len(lines) && isBlank(lines[n]) {
					n++
				}
				if n < len(lines) && indentOf(lines[n]) >= childIndent && indentOf(lines[n]) > lm.indent {
					item = append(item, "")
					k++
					continue
				}
				break
			}
			ind := indentOf(l)
			if ind >= childIndent && ind > lm.indent {
				item = append(item, l[min(ind, lm.content):])
				k++
				continue
			}
			// another item ends this one; otherwise a lazy continuation
			// of the item's paragraph
			if _, isItem := parseListItem(l); isItem {
				break
			}
			if !isBlank(item[len(item)-1]) && !startsBlock(l) && !strings.Contains(l, "|") {
				item = append(item, strings.TrimLeft(l, " "))
				k++
				continue
			}
			break
		}
		c.listItem(item, lm, key, depth, out)
		j = k
		// blank lines between items keep the list going
		n := j
		for n < len(lines) && isBlank(lines[n]) {
			n++
		}
		if n > j {
			if next, ok := parseListItem(safeLine(lines, n)); ok && next.ordered == first.ordered &&
				next.indent >= first.indent && next.indent <= first.indent+3 {
				j = n
			}
		}
	}
	return j
}

func safeLine(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

func (c *converter) listItem(item []string, lm listMarker, key, depth int, out *[]docxwrite.Block) {
	marker := &docxwrite.ListItem{Ordered: lm.ordered, Level: depth, List: key, Start: lm.start}
	var own []docxwrite.Block
	marked := false
	for _, blk := range c.blocks(item, depth+1) {
		p, ok := blk.(docxwrite.Paragraph)
		if !ok {
			own = append(own, blk)
			continue
		}
		if !marked && len(own) == 0 && p.List == nil && (p.Style == "" || p.Style == docxwrite.StyleNormal) {
			p.List = marker
			marked = true
		} else if p.List == nil {
			p.IndentTwips += docxwrite.ListTextIndent(depth)
		}
		own = append(own, p)
	}
	if !marked {
		// an empty item, or one opening with another block: keep its number
		own = append([]docxwrite.Block{docxwrite.Paragraph{List: marker}}, own...)
	}
	*out = append(*out, own...)
}

func (c *converter) paragraph(lines []string, i int, out *[]docxwrite.Block) int {
	var text []string
	j := i
	for ; j < len(lines); j++ {
		l := lines[j]
		if isBlank(l) {
			break
		}
		if j > i {
			if setextH1Re.MatchString(l) || setextH2Re.MatchString(l) {
				level := 1
				if setextH2Re.MatchString(l) {
					level = 2
				}
				*out = append(*out, headingParagraph(level, strings.Join(text, " ")))
				return j + 1
			}
			if startsBlock(l) {
				break
			}
			if j+1 < len(lines) && strings.Contains(l, "|") && tableSepRe.MatchString(lines[j+1]) {
				break
			}
		}
		text = append(text, strings.TrimLeft(l, " "))
	}
	// a hard break written as two trailing spaces or a backslash is the same
	// line break a plain newline already gives
	for k, t := range text {
		t = strings.TrimRight(t, " \t")
		if k < len(text)-1 {
			t = strings.TrimSuffix(t, "\\")
		}
		text[k] = t
	}
	*out = append(*out, docxwrite.Paragraph{Runs: inlineRuns(strings.Join(text, "\n"))})
	return j
}

// splitRow splits a table row into trimmed cells, honouring \| escapes and
// pipes inside code spans.
func splitRow(l string) []string {
	l = strings.TrimSpace(l)
	l = strings.TrimPrefix(l, "|")
	if strings.HasSuffix(l, "|") && !strings.HasSuffix(l, "\\|") {
		l = l[:len(l)-1]
	}
	var cells []string
	var cur strings.Builder
	inCode := false
	for i := 0; i < len(l); i++ {
		ch := l[i]
		switch {
		case ch == '\\' && i+1 < len(l) && l[i+1] == '|':
			cur.WriteByte('|')
			i++
		case ch == '`':
			inCode = !inCode
			cur.WriteByte(ch)
		case ch == '|' && !inCode:
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

func (c *converter) table(lines []string, i int, out *[]docxwrite.Block) int {
	header := splitRow(lines[i])
	var aligns []string
	for _, s := range splitRow(lines[i+1]) {
		left, right := strings.HasPrefix(s, ":"), strings.HasSuffix(s, ":")
		switch {
		case left && right:
			aligns = append(aligns, "center")
		case right:
			aligns = append(aligns, "right")
		default:
			aligns = append(aligns, "left")
		}
	}
	cellsOf := func(cells []string) []docxwrite.TableCell {
		row := make([]docxwrite.TableCell, len(header))
		for k := range row {
			text := ""
			if k < len(cells) {
				text = cells[k]
			}
			// <br> inside a cell is a line break (inlineRuns handles it)
			row[k] = docxwrite.TableCell{Paragraphs: []docxwrite.Paragraph{{
				Style: docxwrite.StyleTableText, Align: aligns[min(k, len(aligns)-1)], Runs: inlineRuns(text),
			}}}
		}
		return row
	}
	t := docxwrite.Table{HeaderRows: 1, Rows: [][]docxwrite.TableCell{cellsOf(header)}}
	j := i + 2
	for ; j < len(lines); j++ {
		l := lines[j]
		if isBlank(l) || !strings.Contains(l, "|") && startsBlock(l) {
			break
		}
		if !strings.Contains(l, "|") {
			break
		}
		t.Rows = append(t.Rows, cellsOf(splitRow(l)))
	}
	*out = append(*out, t)
	return j
}

// ---- inline ----------------------------------------------------------------

type inlineStyle struct {
	bold, italic, strike bool
}

var (
	htmlBreakRe = regexp.MustCompile(`(?i)<br\s*/?>`)
	htmlTagRe   = regexp.MustCompile(`</?[a-zA-Z][a-zA-Z0-9-]*(?:\s[^<>]*)?/?>`)
)

// inlineRuns converts the inline Markdown of one paragraph to runs.
func inlineRuns(s string) []docxwrite.Run {
	s = htmlBreakRe.ReplaceAllString(s, "\n")
	var runs []docxwrite.Run
	parseInline(s, inlineStyle{}, &runs)
	return mergeRuns(runs)
}

func mergeRuns(runs []docxwrite.Run) []docxwrite.Run {
	var out []docxwrite.Run
	for _, r := range runs {
		if r.Text == "" {
			continue
		}
		if n := len(out); n > 0 {
			last := &out[n-1]
			if last.Bold == r.Bold && last.Italic == r.Italic && last.Strike == r.Strike && last.Code == r.Code {
				last.Text += r.Text
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

func emit(runs *[]docxwrite.Run, text string, st inlineStyle) {
	if text == "" {
		return
	}
	text = htmlTagRe.ReplaceAllString(text, "")
	*runs = append(*runs, docxwrite.Run{Text: text, Bold: st.bold, Italic: st.italic, Strike: st.strike})
}

func isPunctOrSpace(r rune) bool {
	return r == utf8.RuneError || unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func runeBefore(s string, i int) rune {
	if i <= 0 {
		return ' '
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return r
}

func runeAt(s string, i int) rune {
	if i >= len(s) {
		return ' '
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return r
}

// findCloser finds the delimiter run closing an emphasis opened with
// delim just before start: a run of the same character preceded by a
// non-space (and for "_" not followed by a letter). Code spans and escapes
// are skipped, and a nested emphasis of another width is skipped whole, so
// "*a **b** c*" closes at the last "*".
func findCloser(s string, start int, delim string) int {
	ch := delim[0]
	for k := start; k < len(s); {
		c := s[k]
		switch {
		case c == '\\':
			k += 2
			continue
		case c == '`':
			n := runLen(s, k, '`')
			if e := strings.Index(s[k+n:], s[k:k+n]); e >= 0 {
				k += n + e + n
			} else {
				k += n
			}
			continue
		case c != ch:
			k++
			continue
		}
		n := runLen(s, k, ch)
		canClose := k > start && !unicode.IsSpace(runeBefore(s, k)) && (ch != '_' || isPunctOrSpace(runeAt(s, k+n)))
		canOpen := k+n < len(s) && !unicode.IsSpace(runeAt(s, k+n)) && (ch != '_' || isPunctOrSpace(runeBefore(s, k)))
		if n == len(delim) && canClose {
			return k
		}
		if n != len(delim) && canOpen && (!canClose || n < len(delim)) {
			if e := findCloser(s, k+n, s[k:k+n]); e >= 0 {
				k = e + n
				continue
			}
		}
		if n > len(delim) && canClose {
			return k + n - len(delim)
		}
		k += n
	}
	return -1
}

func runLen(s string, i int, ch byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == ch {
		n++
	}
	return n
}

func parseInline(s string, st inlineStyle, runs *[]docxwrite.Run) {
	var plain strings.Builder
	flush := func() {
		emit(runs, plain.String(), st)
		plain.Reset()
	}
	for i := 0; i < len(s); {
		ch := s[i]
		switch {
		case ch == '\\' && i+1 < len(s) && strings.IndexByte("\\`*_{}[]()#+-.!|~<>", s[i+1]) >= 0:
			plain.WriteByte(s[i+1])
			i += 2
			continue
		case ch == '`':
			n := 1
			for i+n < len(s) && s[i+n] == '`' {
				n++
			}
			ticks := s[i : i+n]
			if end := strings.Index(s[i+n:], ticks); end >= 0 {
				code := s[i+n : i+n+end]
				if len(code) > 1 && code[0] == ' ' && code[len(code)-1] == ' ' && strings.TrimSpace(code) != "" {
					code = code[1 : len(code)-1]
				}
				flush()
				*runs = append(*runs, docxwrite.Run{Text: strings.ReplaceAll(code, "\n", " "), Code: true,
					Bold: st.bold, Italic: st.italic, Strike: st.strike})
				i += n + end + n
				continue
			}
			plain.WriteString(ticks)
			i += n
			continue
		case ch == '!' && i+1 < len(s) && s[i+1] == '[':
			if text, _, end, ok := parseLink(s, i+1); ok {
				flush()
				alt := strings.TrimSpace(text)
				if alt == "" {
					emit(runs, "[Hình]", st)
				} else {
					emit(runs, "[Hình: "+alt+"]", st)
				}
				i = end
				continue
			}
		case ch == '[':
			if text, url, end, ok := parseLink(s, i); ok {
				flush()
				parseInline(text, st, runs)
				if url != "" && url != "#" && strings.TrimSpace(stripMarkup(text)) != url {
					emit(runs, " ("+url+")", st)
				}
				i = end
				continue
			}
		case ch == '<':
			if end := strings.IndexByte(s[i:], '>'); end > 0 {
				inner := s[i+1 : i+end]
				if (strings.HasPrefix(inner, "http://") || strings.HasPrefix(inner, "https://") ||
					strings.HasPrefix(inner, "mailto:")) && !strings.ContainsAny(inner, " <") {
					plain.WriteString(strings.TrimPrefix(inner, "mailto:"))
					i += end + 1
					continue
				}
			}
		case ch == '*' || ch == '_' || ch == '~':
			n := 1
			for i+n < len(s) && s[i+n] == ch {
				n++
			}
			if ch == '~' && n != 2 {
				plain.WriteString(s[i : i+n])
				i += n
				continue
			}
			if n > 3 {
				plain.WriteString(s[i : i+n])
				i += n
				continue
			}
			delim := s[i : i+n]
			after := runeAt(s, i+n)
			before := runeBefore(s, i)
			opens := !unicode.IsSpace(after) && i+n < len(s)
			if ch == '_' && !isPunctOrSpace(before) {
				opens = false // snake_case stays as written
			}
			if opens {
				if end := findCloser(s, i+n, delim); end >= 0 {
					inner := s[i+n : end]
					next := st
					switch {
					case ch == '~':
						next.strike = true
					case n == 1:
						next.italic = true
					case n == 2:
						next.bold = true
					default:
						next.bold, next.italic = true, true
					}
					flush()
					parseInline(inner, next, runs)
					i = end + n
					continue
				}
			}
			plain.WriteString(delim)
			i += n
			continue
		}
		plain.WriteByte(ch)
		i++
	}
	flush()
}

// parseLink parses "[text](url)" at s[i] (s[i] == '['), returning the end
// index past ")". Nested brackets in the text and parentheses in the URL
// are balanced; an optional "title" after the URL is dropped.
func parseLink(s string, i int) (text, url string, end int, ok bool) {
	depth := 0
	j := i
	for ; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '[':
			depth++
		case ']':
			depth--
		}
		if depth == 0 {
			break
		}
	}
	if j >= len(s) || j+1 >= len(s) || s[j+1] != '(' {
		return "", "", 0, false
	}
	text = s[i+1 : j]
	k := j + 2
	paren := 1
	for ; k < len(s); k++ {
		if s[k] == '(' {
			paren++
		} else if s[k] == ')' {
			paren--
			if paren == 0 {
				break
			}
		} else if s[k] == '\n' {
			return "", "", 0, false
		}
	}
	if k >= len(s) {
		return "", "", 0, false
	}
	dest := strings.TrimSpace(s[j+2 : k])
	if sp := strings.IndexAny(dest, " \t"); sp >= 0 {
		dest = dest[:sp]
	}
	dest = strings.TrimSuffix(strings.TrimPrefix(dest, "<"), ">")
	return text, dest, k + 1, true
}

// stripMarkup is the visible text of inline Markdown (to compare a link's
// text with its URL).
func stripMarkup(s string) string {
	var b strings.Builder
	for _, r := range inlineRuns(s) {
		b.WriteString(r.Text)
	}
	return b.String()
}
