package answerdocx

import (
	"context"
	"regexp"
	"strconv"
	"strings"

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
// Common HTML tags (b, a, code, li, span, …) are dropped, their text kept,
// and <br> is a line break; anything else in angle brackets stays as text.
// HTML character references with their ";" are decoded (as marked does).
// Inline citation tags (<kb …/>, <web …/>, <ref …/>), closed
// <think>…</think> reasoning and <people_lookup>…</people_lookup>
// envelopes outside code are removed.
//
// The parser is linear in the input: emphasis follows the CommonMark
// delimiter algorithm, brackets, parentheses and code spans are matched in
// one pass up front. Nesting of lists and quotes is capped at
// maxNesting levels (deeper lines are read as text), and an answer longer
// than MaxMarkdownBytes is written as plain paragraphs.

// MaxMarkdownBytes is the longest answer converted as Markdown; a longer
// one is written as plain paragraphs.
const MaxMarkdownBytes = 300 << 10

// maxNesting caps nested lists and quotes.
const maxNesting = 8

// maxTableColumns caps the columns of a table; further cells join the last.
const maxTableColumns = 64

// maxTableCells caps the cells of all tables (short rows are padded, so a
// small input could otherwise ask for millions); rows past it are text.
const maxTableCells = 20000

// converter turns Markdown lines into docx blocks. listSeq hands out the
// numbering keys of ordered lists so each list restarts at its own start.
type converter struct {
	listSeq int
	bud     *budget
	cells   int
}

// budget stops a conversion whose context ends: every loop calls out(),
// which checks the context every 1024 calls.
type budget struct {
	ctx context.Context
	n   int
	err error
}

func (b *budget) out() bool {
	if b == nil {
		return false
	}
	if b.err != nil {
		return true
	}
	b.n++
	if b.n&1023 == 0 {
		b.err = b.ctx.Err()
	}
	return b.err != nil
}

// MarkdownBlocks converts a chat answer's Markdown to docx blocks.
func MarkdownBlocks(md string) []docxwrite.Block {
	blocks, _ := MarkdownBlocksContext(context.Background(), md)
	return blocks
}

// MarkdownBlocksContext is MarkdownBlocks that gives up with ctx's error
// once ctx ends. An answer over MaxMarkdownBytes comes back as plain
// paragraphs.
func MarkdownBlocksContext(ctx context.Context, md string) ([]docxwrite.Block, error) {
	if len(md) > MaxMarkdownBytes {
		return PlainBlocks(md), nil
	}
	md = preprocess(md)
	c := &converter{bud: &budget{ctx: ctx}}
	blocks := c.blocks(splitLines(md), 0, 0)
	if c.bud.err != nil {
		return nil, c.bud.err
	}
	return blocks, nil
}

// PlainBlocks writes md as it is, one paragraph per blank-line separated
// block: the fallback for an answer too long or too slow to convert.
func PlainBlocks(md string) []docxwrite.Block {
	md = strings.ReplaceAll(strings.ReplaceAll(md, "\r\n", "\n"), "\r", "\n")
	var out []docxwrite.Block
	for _, para := range strings.Split(md, "\n\n") {
		if para = strings.Trim(para, "\n"); strings.TrimSpace(para) != "" {
			out = append(out, docxwrite.Paragraph{Runs: []docxwrite.Run{{Text: para}}})
		}
	}
	return out
}

var (
	wikiLinkRe = regexp.MustCompile(`\[\[([^\]\n]+)\]\]`)
	// citationTagRe is an inline citation of the agent pipeline: <kb …/>,
	// <web …/>, <ref …/> (the chat draws them as chips; the sources list
	// replaces them here). Like imCitationTagRe in internal/im, with no
	// length bound: titles run to hundreds of characters.
	citationTagRe = regexp.MustCompile(`</?(?:kb|web|ref)\b(?:[^<>"'\n]|"[^"\n]*"|'[^'\n]*')*>`)
	headingRe     = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?[ \t]*$`)
	closingHash   = regexp.MustCompile(`[ \t]+#+$`)
	ruleRe        = regexp.MustCompile(`^ {0,3}((?:-[ \t]*){3,}|(?:\*[ \t]*){3,}|(?:_[ \t]*){3,})$`)
	fenceRe       = regexp.MustCompile("^( {0,3})(`{3,}|~{3,})(.*)$")
	listItemRe    = regexp.MustCompile(`^( *)([-*+]|\d{1,9}[.)])([ \t]+|$)(.*)$`)
	tableSepRe    = regexp.MustCompile(`^ {0,3}\|?[ \t]*:?-+:?[ \t]*(\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$`)
	setextH1Re    = regexp.MustCompile(`^ {0,3}=+[ \t]*$`)
	setextH2Re    = regexp.MustCompile(`^ {0,3}-+[ \t]*$`)
	quoteRe       = regexp.MustCompile(`^ {0,3}>`)
)

func preprocess(md string) string {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	md = strings.ReplaceAll(md, "\r", "\n")
	md = stripPairs(md, "<think>", "</think>")
	md = stripPairs(md, "<people_lookup>", "</people_lookup>")
	md = replaceOutsideCode(md, citationTagRe, "")
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
// get); nest counts the lists and quotes around them.
func (c *converter) blocks(lines []string, depth, nest int) []docxwrite.Block {
	var out []docxwrite.Block
	structured := nest < maxNesting
	for i := 0; i < len(lines); {
		if c.bud.out() {
			return out
		}
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
			out = append(out, c.heading(len(m[1]), text))
			i++
			continue
		}
		if ruleRe.MatchString(l) {
			out = append(out, docxwrite.Paragraph{Style: docxwrite.StyleRule})
			i++
			continue
		}
		if structured && quoteRe.MatchString(l) {
			i = c.quote(lines, i, depth, nest, &out)
			continue
		}
		if i+1 < len(lines) && strings.Contains(l, "|") && tableSepRe.MatchString(lines[i+1]) &&
			strings.Contains(lines[i+1], "-") && len(splitRow(l)) == len(splitRow(lines[i+1])) {
			i = c.table(lines, i, &out)
			continue
		}
		if _, ok := parseListItem(l); ok && structured {
			i = c.list(lines, i, depth, nest, &out)
			continue
		}
		i = c.paragraph(lines, i, &out)
	}
	return out
}

func (c *converter) heading(level int, text string) docxwrite.Paragraph {
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
	return docxwrite.Paragraph{Style: style, Align: align, Runs: c.inline(text)}
}

func (c *converter) fencedCode(lines []string, i int, m []string, out *[]docxwrite.Block) int {
	indent, fence := len(m[1]), m[2]
	var body []string
	j := i + 1
	for ; j < len(lines) && !c.bud.out(); j++ {
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

func (c *converter) quote(lines []string, i, depth, nest int, out *[]docxwrite.Block) int {
	var inner []string
	j := i
	for ; j < len(lines) && !c.bud.out(); j++ {
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
	for _, blk := range c.blocks(inner, depth, nest+1) {
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
func (c *converter) list(lines []string, i, depth, nest int, out *[]docxwrite.Block) int {
	first, _ := parseListItem(lines[i])
	c.listSeq++
	key := c.listSeq
	j := i
	for j < len(lines) && !c.bud.out() {
		lm, ok := parseListItem(lines[j])
		if !ok || lm.ordered != first.ordered || lm.indent > first.indent+3 || lm.indent < first.indent {
			break
		}
		// the item's own lines: the first, then indented or lazy lines
		item := []string{lm.text}
		k := j + 1
		childIndent := min(lm.content, lm.indent+2)
		for k < len(lines) && !c.bud.out() {
			l := lines[k]
			if isBlank(l) {
				// blank lines stay inside the item when more of it follows
				n := k + 1
				for n < len(lines) && isBlank(lines[n]) {
					n++
				}
				if n < len(lines) && indentOf(lines[n]) >= childIndent && indentOf(lines[n]) > lm.indent {
					for ; k < n; k++ {
						item = append(item, "")
					}
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
		c.listItem(item, lm, key, depth, nest, out)
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

func (c *converter) listItem(item []string, lm listMarker, key, depth, nest int, out *[]docxwrite.Block) {
	marker := &docxwrite.ListItem{Ordered: lm.ordered, Level: depth, List: key, Start: lm.start}
	var own []docxwrite.Block
	marked := false
	for _, blk := range c.blocks(item, depth+1, nest+1) {
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
	for ; j < len(lines) && !c.bud.out(); j++ {
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
				*out = append(*out, c.heading(level, strings.Join(text, " ")))
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
	*out = append(*out, docxwrite.Paragraph{Runs: c.inline(strings.Join(text, "\n"))})
	return j
}

// splitRow splits a table row into trimmed cells on unescaped pipes; as in
// the chat renderer (marked), a pipe inside a code span splits too unless
// written \|.
func splitRow(l string) []string {
	l = strings.TrimSpace(l)
	l = strings.TrimPrefix(l, "|")
	if strings.HasSuffix(l, "|") && !strings.HasSuffix(l, "\\|") {
		l = l[:len(l)-1]
	}
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(l); i++ {
		ch := l[i]
		switch {
		case ch == '\\' && i+1 < len(l) && l[i+1] == '|':
			cur.WriteByte('|')
			i++
		case ch == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

func (c *converter) table(lines []string, i int, out *[]docxwrite.Block) int {
	header := capColumns(splitRow(lines[i]))
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
		cells = capColumns(cells)
		row := make([]docxwrite.TableCell, len(header))
		for k := range row {
			text := ""
			if k < len(cells) {
				text = cells[k]
			}
			// <br> inside a cell is a line break (inline handles it)
			row[k] = docxwrite.TableCell{Paragraphs: []docxwrite.Paragraph{{
				Style: docxwrite.StyleTableText, Align: aligns[min(k, len(aligns)-1)], Runs: c.inline(text),
			}}}
		}
		return row
	}
	if c.cells+len(header) > maxTableCells {
		*out = append(*out, docxwrite.Paragraph{Runs: c.inline(lines[i])})
		return i + 1
	}
	c.cells += len(header)
	t := docxwrite.Table{HeaderRows: 1, Rows: [][]docxwrite.TableCell{cellsOf(header)}}
	j := i + 2
	for ; j < len(lines) && !c.bud.out() && c.cells+len(header) <= maxTableCells; j++ {
		l := lines[j]
		if isBlank(l) || !strings.Contains(l, "|") && startsBlock(l) {
			break
		}
		if !strings.Contains(l, "|") {
			break
		}
		t.Rows = append(t.Rows, cellsOf(splitRow(l)))
		c.cells += len(header)
	}
	*out = append(*out, t)
	return j
}

// capColumns joins cells past maxTableColumns into the last one.
func capColumns(cells []string) []string {
	if len(cells) <= maxTableColumns {
		return cells
	}
	out := append([]string{}, cells[:maxTableColumns-1]...)
	return append(out, strings.Join(cells[maxTableColumns-1:], " | "))
}

// replaceOutsideCode replaces the matches of re outside fenced code and
// code spans, line by line.
func replaceOutsideCode(md string, re *regexp.Regexp, repl string) string {
	if !re.MatchString(md) {
		return md
	}
	var b strings.Builder
	inFence, fenceCh, fenceLen := false, byte(0), 0
	for _, line := range strings.SplitAfter(md, "\n") {
		body := strings.TrimRight(line, "\n")
		if m := fenceRe.FindStringSubmatch(body); m != nil {
			if !inFence {
				inFence, fenceCh, fenceLen = true, m[2][0], len(m[2])
			} else if m[2][0] == fenceCh && len(m[2]) >= fenceLen && strings.TrimSpace(m[3]) == "" {
				inFence = false
			}
			b.WriteString(line)
			continue
		}
		if inFence || !re.MatchString(body) {
			b.WriteString(line)
			continue
		}
		spans := codeSpans(body, true)
		last := 0
		for i := 0; i < len(body); {
			switch {
			case body[i] == '\\':
				i += 2
				continue
			case body[i] == '`':
				if e, ok := spans[i]; ok {
					b.WriteString(re.ReplaceAllString(body[last:i], repl))
					b.WriteString(body[i:e])
					last, i = e, e
					continue
				}
				i += runLen(body, i, '`')
				continue
			}
			i++
		}
		b.WriteString(re.ReplaceAllString(body[last:], repl))
		b.WriteString(line[len(body):])
	}
	return b.String()
}

// stripPairs removes each closed open…close pair outside fenced code and
// code spans; an open tag without its close is left alone.
func stripPairs(md, open, close string) string {
	if !strings.Contains(md, open) {
		return md
	}
	type span struct{ start, end int }
	var opens, closes []int
	inFence, fenceCh, fenceLen := false, byte(0), 0
	pos := 0
	for _, line := range strings.SplitAfter(md, "\n") {
		body := strings.TrimRight(line, "\n")
		if m := fenceRe.FindStringSubmatch(body); m != nil {
			if !inFence {
				inFence, fenceCh, fenceLen = true, m[2][0], len(m[2])
			} else if m[2][0] == fenceCh && len(m[2]) >= fenceLen && strings.TrimSpace(m[3]) == "" {
				inFence = false
			}
			pos += len(line)
			continue
		}
		if !inFence {
			spans := codeSpans(body, false)
			for i := 0; i < len(body); {
				switch {
				case body[i] == '`':
					if e, ok := spans[i]; ok {
						i = e
					} else {
						i += runLen(body, i, '`')
					}
					continue
				case strings.HasPrefix(body[i:], open):
					opens = append(opens, pos+i)
					i += len(open)
					continue
				case strings.HasPrefix(body[i:], close):
					closes = append(closes, pos+i)
					i += len(close)
					continue
				}
				i++
			}
		}
		pos += len(line)
	}
	var spans []span
	ci := 0
	for oi := 0; oi < len(opens); {
		for ci < len(closes) && closes[ci] < opens[oi] {
			ci++
		}
		if ci == len(closes) {
			break
		}
		spans = append(spans, span{opens[oi], closes[ci] + len(close)})
		for oi < len(opens) && opens[oi] < closes[ci] {
			oi++
		}
		ci++
	}
	if len(spans) == 0 {
		return md
	}
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		b.WriteString(md[last:sp.start])
		last = sp.end
	}
	b.WriteString(md[last:])
	return strings.TrimSpace(b.String())
}
