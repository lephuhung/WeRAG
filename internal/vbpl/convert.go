// Package vbpl converts the full-text HTML published by vbpl.vn (gateway
// field documentContent.content) into canonical Markdown plus a block map.
//
// vbpl.vn tags the legal structure on the paragraphs themselves:
// class="prov-part|prov-chapter|prov-section|prov-subsection" (headings,
// often split over several paragraphs), "prov-article" (Điều),
// "prov-clause" (Khoản), "prov-item" (Điểm), "prov-content" (any other
// paragraph — NOT only quoted text) and "prov-table", with a stable
// provision id on article/clause/item paragraphs. The portal deep-links
// to a provision with https://vbpl.vn/van-ban/chi-tiet/{docID}?go={id}.
//
// The converter trusts those classes for the document's own structure and
// recognises quoted amending text (“...”) by its quote span, never by
// class: amending decrees quote whole Điều of another document, and the
// class on those paragraphs is unreliable. Every block records its rune
// offsets in the produced Markdown, so chunks can be exact slices of it.
package vbpl

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/text/unicode/norm"
)

// ConverterVersion is recorded on ingested knowledge; bump it whenever the
// Markdown or block layout changes so stale documents can be re-ingested.
const ConverterVersion = 2

// BlockKind is the structural role of a block.
type BlockKind string

const (
	KindPreamble   BlockKind = "preamble"   // header, title, căn cứ — before the first provision
	KindPart       BlockKind = "part"       // Phần / old-style "I." sections
	KindChapter    BlockKind = "chapter"    // Chương
	KindSection    BlockKind = "section"    // Mục
	KindSubsection BlockKind = "subsection" // Tiểu mục
	KindArticle    BlockKind = "article"    // Điều
	KindClause     BlockKind = "clause"     // Khoản
	KindItem       BlockKind = "item"       // Điểm
	KindContent    BlockKind = "content"    // other body paragraph
	KindQuote      BlockKind = "quote"      // quoted (amending) text
	KindTable      BlockKind = "table"
	KindClosing    BlockKind = "closing"  // signature, Nơi nhận, closing formula
	KindAppendix   BlockKind = "appendix" // Phụ lục heading
)

// IsHeading reports whether the kind is a Phần/Chương/Mục/Tiểu mục heading.
func (k BlockKind) IsHeading() bool {
	return k == KindPart || k == KindChapter || k == KindSection || k == KindSubsection
}

func headingRank(k BlockKind) int {
	switch k {
	case KindPart:
		return 1
	case KindChapter:
		return 2
	case KindSection:
		return 3
	case KindSubsection:
		return 4
	}
	return 0
}

// Block is one paragraph-level unit of the Markdown.
type Block struct {
	Kind BlockKind `json:"kind"`
	// ID is the vbpl provision id (article/clause/item/heading paragraphs);
	// empty for content without one.
	ID string `json:"id,omitempty"`
	// Number is the parsed label: "17" for Điều 17, "2" for khoản 2, "a"
	// for điểm a, "II" for Chương II. Empty when it could not be parsed.
	Number string `json:"number,omitempty"`
	// Text is the plain text (heading text for headings and articles).
	Text string `json:"text"`
	// Parent is the index of the enclosing structural block, -1 at top.
	Parent int `json:"parent"`
	// Start/End are rune offsets of the block's Markdown in Document.Markdown.
	Start int `json:"start"`
	End   int `json:"end"`
	// RowStarts holds the rune offset of every data row of a table (header
	// row excluded) so an oversized table can be cut between rows.
	RowStarts []int `json:"row_starts,omitempty"`
	// HeaderEnd is the rune offset where a table's header + separator end.
	HeaderEnd int `json:"header_end,omitempty"`
	// Inferred is true when the structure was recognised from the text
	// (attached documents, appendices, pages without vbpl markup).
	Inferred bool `json:"inferred,omitempty"`
	// Amend is, on quote blocks, what the quoted span amends; nil when the
	// lead-in does not read as an amendment. Blocks of one span share it.
	Amend *Amend `json:"amend,omitempty"`
	// Phrases are the phrase amendments a clause, item or content
	// paragraph states ("Sửa đổi cụm từ “X” thành “Y” tại Điều 3").
	Phrases []Amend `json:"phrases,omitempty"`
}

// Document is the conversion result.
type Document struct {
	Markdown string  `json:"-"`
	Blocks   []Block `json:"blocks"`
	// Structured is true when the HTML carried provision markup the chunker
	// can rely on (at least one article, or a part with clauses). When
	// false the caller should fall back to the text-based legal chunker.
	Structured bool     `json:"structured"`
	Warnings   []string `json:"warnings,omitempty"`
	Stats      Stats    `json:"stats"`
	// Links are the page's links to cited vbpl.vn documents, in page
	// order; CitationLinks places them in chunks.
	Links []Link `json:"links,omitempty"`
}

// Stats is diagnostic counters for pilots and logs.
type Stats struct {
	Articles      int `json:"articles"`
	Clauses       int `json:"clauses"`
	Items         int `json:"items"`
	Headings      int `json:"headings"`
	QuoteSpans    int `json:"quote_spans"`
	ForcedCloses  int `json:"forced_quote_closes"`
	Tables        int `json:"tables"`
	Images        int `json:"images_dropped"`
	Appendices    int `json:"appendices"`
	ClosingRunes  int `json:"closing_runes"`
	UnnumberedArt int `json:"unnumbered_articles"`
}

// ---------------------------------------------------------------------------
// HTML → flat unit list

type unitKind int

const (
	uPara unitKind = iota
	uTable
	uAppendixRegion // marker: a div.doc-region-appendix starts here
)

type unit struct {
	kind  unitKind
	class string // the prov-* class, "" when none
	id    string
	text  string
	lines []string // paragraph lines split at <br>, for header tables
	table [][]string
}

var provClasses = map[string]bool{
	"prov-part": true, "prov-chapter": true, "prov-section": true, "prov-subsection": true,
	"prov-article": true, "prov-clause": true, "prov-item": true, "prov-content": true,
	"prov-table": true,
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func provClass(n *html.Node) string {
	for _, c := range strings.Fields(attr(n, "class")) {
		if provClasses[c] {
			return c
		}
	}
	return ""
}

func hasClass(n *html.Node, want string) bool {
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == want {
			return true
		}
	}
	return false
}

var blockAtoms = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Table: true, atom.Ul: true, atom.Ol: true, atom.Li: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Section: true, atom.Article: true, atom.Blockquote: true, atom.Pre: true,
}

func hasBlockChild(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (blockAtoms[c.DataAtom] || hasBlockChild(c)) {
			return true
		}
	}
	return false
}

type collector struct {
	units  []unit
	images int
}

func (c *collector) walk(n *html.Node) {
	if n.Type != html.ElementNode && n.Type != html.DocumentNode {
		return
	}
	if n.Type == html.ElementNode {
		switch n.DataAtom {
		case atom.Head, atom.Style, atom.Script, atom.Noscript, atom.Template:
			return
		case atom.Img:
			c.images++
			return
		case atom.Table:
			c.addTable(n)
			return
		case atom.P, atom.Li, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Pre:
			c.addPara(n)
			return
		case atom.Div, atom.Section, atom.Article, atom.Blockquote, atom.Span, atom.Font, atom.Body:
			if hasClass(n, "doc-region-appendix") {
				c.units = append(c.units, unit{kind: uAppendixRegion})
			}
			if n.DataAtom != atom.Body && !hasBlockChild(n) {
				c.addPara(n)
				return
			}
		}
	}
	// Direct inline text mixed with blocks (rare): emit it as its own unit.
	var inline []*html.Node
	flush := func() {
		if len(inline) == 0 {
			return
		}
		var b strings.Builder
		for _, x := range inline {
			c.countImages(x)
			writeText(&b, x)
		}
		if t := cleanText(b.String()); t != "" {
			c.units = append(c.units, unit{kind: uPara, text: t, lines: []string{t}})
		}
		inline = nil
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type == html.TextNode || (ch.Type == html.ElementNode && !blockAtoms[ch.DataAtom] && !hasBlockChild(ch) &&
			ch.DataAtom != atom.Img && ch.DataAtom != atom.Style && ch.DataAtom != atom.Script) {
			inline = append(inline, ch)
			continue
		}
		flush()
		c.walk(ch)
	}
	flush()
}

func (c *collector) countImages(n *html.Node) {
	if n.Type == html.ElementNode && n.DataAtom == atom.Img {
		c.images++
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.countImages(ch)
	}
}

func (c *collector) addPara(n *html.Node) {
	c.countImages(n)
	var b strings.Builder
	writeText(&b, n)
	raw := b.String()
	var lines []string
	for _, l := range strings.Split(raw, "\n") {
		if t := cleanText(l); t != "" {
			lines = append(lines, t)
		}
	}
	t := cleanText(raw)
	if t == "" {
		return
	}
	c.units = append(c.units, unit{kind: uPara, class: provClass(n), id: attr(n, "id"), text: t, lines: lines})
}

// addTable flattens a table into a rectangular grid; merged cells repeat
// their text in every covered position so each row reads on its own.
func (c *collector) addTable(n *html.Node) {
	c.countImages(n)
	cls := provClass(n)
	var rows [][]string
	type pending struct {
		text string
		left int
	}
	carry := map[int]pending{} // column → rowspan carry-over
	var visitRows func(*html.Node)
	visitRows = func(x *html.Node) {
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type != html.ElementNode {
				continue
			}
			switch ch.DataAtom {
			case atom.Tr:
				if cls == "" {
					cls = provClass(ch)
				}
				var row []string
				col := 0
				takeCarry := func() {
					for {
						p, ok := carry[col]
						if !ok {
							return
						}
						row = append(row, p.text)
						if p.left <= 1 {
							delete(carry, col)
						} else {
							carry[col] = pending{p.text, p.left - 1}
						}
						col++
					}
				}
				for td := ch.FirstChild; td != nil; td = td.NextSibling {
					if td.Type != html.ElementNode || (td.DataAtom != atom.Td && td.DataAtom != atom.Th) {
						continue
					}
					if cls == "" {
						cls = provClass(td)
					}
					takeCarry()
					var b strings.Builder
					writeText(&b, td)
					var parts []string
					for _, l := range strings.Split(b.String(), "\n") {
						if t := cleanText(l); t != "" {
							parts = append(parts, t)
						}
					}
					text := strings.Join(parts, " ")
					cs, rs := atoiDefault(attr(td, "colspan"), 1), atoiDefault(attr(td, "rowspan"), 1)
					for k := 0; k < cs; k++ {
						row = append(row, text)
						if rs > 1 {
							carry[col] = pending{text, rs - 1}
						}
						col++
					}
				}
				takeCarry()
				rows = append(rows, row)
			case atom.Table:
				// nested table: its text is already part of the outer cell
			default:
				visitRows(ch)
			}
		}
	}
	visitRows(n)
	// Drop empty rows and pad to a rectangle.
	width := 0
	var kept [][]string
	for _, r := range rows {
		empty := true
		for _, cell := range r {
			if cell != "" {
				empty = false
				break
			}
		}
		if empty {
			continue
		}
		kept = append(kept, r)
		if len(r) > width {
			width = len(r)
		}
	}
	if len(kept) == 0 {
		return
	}
	for i := range kept {
		for len(kept[i]) < width {
			kept[i] = append(kept[i], "")
		}
	}
	c.units = append(c.units, unit{kind: uTable, class: cls, table: kept})
}

func atoiDefault(s string, d int) int {
	n := 0
	for _, r := range strings.TrimSpace(s) {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	if n <= 0 || n > 50 {
		return d
	}
	return n
}

func writeText(b *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(n.Data)
		return
	case html.ElementNode:
		switch n.DataAtom {
		case atom.Br:
			b.WriteByte('\n')
			return
		case atom.Img, atom.Style, atom.Script:
			return
		case atom.P, atom.Div, atom.Li, atom.Tr, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
			b.WriteByte('\n')
			defer b.WriteByte('\n')
		case atom.Td, atom.Th:
			b.WriteByte(' ')
			defer b.WriteByte(' ')
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		writeText(b, c)
	}
}

// cleanText NFC-normalises (some pages spell "Điều" with combining marks),
// collapses whitespace, drops zero-width characters and maps NBSP.
func cleanText(s string) string {
	s = norm.NFC.String(s)
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\ufeff' || r == '\u00ad':
			continue
		case unicode.IsSpace(r) || r == '\u00a0':
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Recognisers

var (
	// "Điều 17." with the Eth look-alike (Ð U+00D0) and a typo seen in the corpus.
	articleNumRe = regexp.MustCompile(`^(?:Điều|ĐIỀU|Ðiều|Điêu|Điền|Ðiêu)\s*(\d+[a-zđ]?)`)
	clauseNumRe  = regexp.MustCompile(`^(\d+(?:\.\d+)*)\s*[.)]`)
	itemNumRe    = regexp.MustCompile(`^([a-zđ]{1,2}(?:\.\d+)?)\s*\)`)
	headingKwRe  = regexp.MustCompile(`(?i)^(?:phần|chương|mục|tiểu mục)\s+([\p{L}\p{N}]+)`)
	romanStartRe = regexp.MustCompile(`^([IVXLC]+)\s*[.:/-]`)
	// Closing formula / signature lines that end the body.
	closingRe = regexp.MustCompile(`^(?:Nơi nhận|NƠI NHẬN|TM\.|KT\.|TL\.|Q\.|(?:Bộ luật|Luật|Pháp lệnh|Nghị quyết|Hiến pháp) này (?:đã )?được .*thông qua)`)
	// Appendix headings after the body.
	appendixRe = regexp.MustCompile(`^(?:PHỤ LỤC|Phụ lục|PHỤ BIỂU|Phụ biểu|DANH MỤC|BIỂU MẪU|MẪU SỐ|Mẫu số|MẪU\s)`)
)

func headingKind(class string) BlockKind {
	switch class {
	case "prov-part":
		return KindPart
	case "prov-chapter":
		return KindChapter
	case "prov-section":
		return KindSection
	case "prov-subsection":
		return KindSubsection
	}
	return ""
}

// startsHeading reports whether a heading-class paragraph opens a new
// heading rather than continuing the title of the previous one.
func startsHeading(text string) (string, bool) {
	if m := headingKwRe.FindStringSubmatch(text); m != nil {
		return m[1], true
	}
	if m := romanStartRe.FindStringSubmatch(text); m != nil {
		return m[1], true
	}
	if m := clauseNumRe.FindStringSubmatch(text); m != nil {
		return m[1], true
	}
	return "", false
}

func startsKeyword(t string) bool { return headingKwRe.MatchString(t) }

// labelOnlyRe matches a heading paragraph that is just its label
// ("Chương II", "MỤC 3"), whose title follows in the next paragraph.
var labelOnlyRe = regexp.MustCompile(`(?i)^(?:phần|chương|mục|tiểu mục)\s+[\p{L}\p{N}]+\s*[.:]?$`)

// newAppendixRe starts a new appendix even right after another title.
var newAppendixRe = regexp.MustCompile(`^(?:PHỤ LỤC|Phụ lục|PHỤ BIỂU|Phụ biểu|MẪU SỐ|Mẫu số)`)

// isAppendixHeading: "PHỤ LỤC I", "Phụ lục 1", "DANH MỤC ..." title lines.
func isAppendixHeading(t string) bool {
	return appendixRe.MatchString(t) && (isUpperHeading(t) || utf8.RuneCountInString(t) < 150)
}

const (
	openQuote  = '“'
	closeQuote = '”'
)

// quoteOpener reports whether a paragraph starts a quoted span: it begins
// with a quote that is not closed before the paragraph ends (so
// “Giá bán điện” là ... — a quoted term — does not count).
func quoteOpener(t string) bool {
	r, size := utf8.DecodeRuneInString(t)
	if r != openQuote && r != '"' {
		return false
	}
	want := closeQuote
	if r == '"' {
		want = '"'
	}
	rest := t[size:]
	j := strings.IndexRune(rest, want)
	if j < 0 {
		return true
	}
	tail := strings.Trim(rest[j+utf8.RuneLen(want):], " .;,:\"”")
	return tail == ""
}

var quoteCloserRe = regexp.MustCompile(`[”"]\s*[.;,:]?\s*[.;]?$`)

func quoteCloser(t string) bool { return quoteCloserRe.MatchString(t) }

// ---------------------------------------------------------------------------
// Conversion

// Convert parses vbpl HTML and builds the Markdown and block map.
func Convert(r io.Reader) (*Document, error) {
	root, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("vbpl: parse html: %w", err)
	}
	col := &collector{}
	col.walk(root)
	b := newBuilder()
	b.doc.Stats.Images = col.images
	b.run(col.units)
	d := b.finish()
	d.Links = collectLinks(root)
	return d, nil
}

// ConvertBytes is Convert for an in-memory document.
func ConvertBytes(p []byte) (*Document, error) { return Convert(bytes.NewReader(p)) }

type builder struct {
	doc   *Document
	md    strings.Builder
	runes int
	// open structural blocks (indexes)
	heading  [5]int // by rank 1..4
	article  int
	clause   int
	item     int
	appendix int // current appendix / attached-document heading
	inQuote  bool
	amend    *Amend // target of the open quote span
	// lastHeading is a heading (or appendix title) that may still receive
	// title continuation lines; titleLines counts the merged lines.
	lastHeading int
	titleLines  int
	// infer: structure comes from text patterns, not vbpl classes
	infer bool
}

func newBuilder() *builder {
	b := &builder{doc: &Document{}, article: -1, clause: -1, item: -1, appendix: -1, lastHeading: -1}
	for i := range b.heading {
		b.heading[i] = -1
	}
	return b
}

func (b *builder) innermost() int {
	for _, i := range []int{b.item, b.clause, b.article} {
		if i >= 0 {
			return i
		}
	}
	return b.parentHeading()
}

func (b *builder) parentHeading() int {
	for r := 4; r >= 1; r-- {
		if b.heading[r] >= 0 {
			return b.heading[r]
		}
	}
	return b.appendix
}

// emit appends md as a new paragraph and records the block.
func (b *builder) emit(blk Block, md string) int {
	if b.runes > 0 {
		b.md.WriteString("\n\n")
		b.runes += 2
	}
	blk.Start = b.runes
	switch blk.Kind {
	case KindClause, KindItem, KindContent:
		blk.Phrases = b.phraseAmends(blk.Text)
	}
	b.md.WriteString(md)
	b.runes += utf8.RuneCountInString(md)
	blk.End = b.runes
	b.doc.Blocks = append(b.doc.Blocks, blk)
	return len(b.doc.Blocks) - 1
}

// appendToLast extends the last block (heading titles split over paragraphs).
func (b *builder) appendToLast(sep, text string) {
	last := &b.doc.Blocks[len(b.doc.Blocks)-1]
	b.md.WriteString(sep + text)
	b.runes += utf8.RuneCountInString(sep + text)
	last.End = b.runes
	last.Text += sep + text
}

type phase int

const (
	phPreamble phase = iota
	phBody
	phClosing
	phAppendix
)

var (
	quocHieuRe = regexp.MustCompile(`(?i)CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM|CỘNG HOÀ XÃ HỘI CHỦ NGHĨA VIỆT NAM`)
	// Title of a document issued together with the main one ("ban hành kèm theo").
	attachedTitleRe = regexp.MustCompile(`^(?:QUY CHẾ|QUY ĐỊNH|ĐỊNH MỨC|QUY CHUẨN|QCVN|TCVN|HƯỚNG DẪN|CHƯƠNG TRÌNH|ĐIỀU LỆ|KẾ HOẠCH|DANH MỤC|BẢNG|BIỂU|TIÊU CHUẨN|CHIẾN LƯỢC|ĐỀ ÁN|QUY TRÌNH|NỘI QUY|CHẾ ĐỘ|CHUẨN|KHUNG|PHƯƠNG ÁN|DANH SÁCH|NỘI DUNG|CHỈ TIÊU|HỆ THỐNG|BỘ TIÊU CHÍ|TIÊU CHÍ|QUY TẮC)`)
	attachedNoteRe  = regexp.MustCompile(`(?i)^\(?\s*(?:ban hành )?kèm theo`)
	// Agency header / date lines of an attached document's header.
	headerLineRe = regexp.MustCompile(`^(?:Số\s*:|Hà Nội,|[-–_.\s]+$|Độc lập)`)
)

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func unitText(u unit) string {
	if u.kind != uTable {
		return u.text
	}
	var sb strings.Builder
	for _, r := range u.table {
		sb.WriteString(strings.Join(r, " "))
		sb.WriteByte(' ')
	}
	return sb.String()
}

func (b *builder) run(units []unit) {
	// The body ends after the last provision outside an appendix region.
	firstProv, lastProv := -1, -1
	inRegion := false
	for i, u := range units {
		if u.kind == uAppendixRegion {
			inRegion = true
			continue
		}
		if inRegion || u.class == "" {
			continue
		}
		if firstProv < 0 {
			firstProv = i
		}
		lastProv = i
	}
	if firstProv < 0 {
		// No vbpl markup (old-format pages): infer the structure from text.
		b.infer = true
		b.doc.Warnings = append(b.doc.Warnings, "no provision markup; structure inferred from text")
		for i, u := range units {
			if u.kind == uPara && isTopStructure(u.text) {
				if firstProv < 0 {
					firstProv = i
				}
				lastProv = i
			}
		}
		if firstProv < 0 {
			firstProv, lastProv = len(units), len(units)
		}
	}

	ph := phPreamble
	awaitTitle := false
	for i, u := range units {
		if u.kind == uAppendixRegion {
			ph = phAppendix
			b.inQuote = false
			awaitTitle = true
			continue
		}
		if u.kind == uPara && !hasLetter(u.text) {
			continue // separator lines ("-------")
		}
		if ph == phPreamble && i >= firstProv {
			ph = phBody
		}
		text := unitText(u)
		if ph == phBody && i > lastProv {
			switch {
			case u.kind == uPara && isAppendixHeading(u.text):
				ph = phAppendix
			case u.kind == uTable || closingRe.MatchString(u.text):
				ph = phClosing
			}
			if ph != phBody {
				b.inQuote = false
			}
		}
		if ph == phClosing && b.startsAttachment(u, text) {
			ph = phAppendix
			awaitTitle = !(u.kind == uPara && (isAppendixHeading(u.text) || attachedTitleRe.MatchString(u.text)))
		}

		switch ph {
		case phPreamble:
			b.emitPlain(u, KindPreamble, -1)
		case phClosing:
			if idx := b.emitPlain(u, KindClosing, -1); idx >= 0 {
				blk := b.doc.Blocks[idx]
				b.doc.Stats.ClosingRunes += blk.End - blk.Start
			}
		case phAppendix:
			if awaitTitle {
				// header of an attached document: agency, quốc hiệu, số, date
				if u.kind == uTable || quocHieuRe.MatchString(text) || headerLineRe.MatchString(u.text) {
					b.emitPlain(u, KindClosing, -1)
					continue
				}
				awaitTitle = false
				b.openAppendix(u.text)
				continue
			}
			if b.lastHeading >= 0 && b.lastHeading == b.appendix && b.titleLines < 4 && u.kind == uPara &&
				utf8.RuneCountInString(u.text) < 250 && b.inferClass(u.text) == "" && !quoteOpener(u.text) &&
				!newAppendixRe.MatchString(u.text) {
				b.appendToLast(" ", u.text)
				b.titleLines++
				continue
			}
			if u.kind == uPara && (isAppendixHeading(u.text) || (isUpperHeading(u.text) && b.article < 0 && attachedTitleRe.MatchString(u.text) && b.lastHeading < 0)) {
				b.openAppendix(u.text)
				continue
			}
			if b.appendix < 0 {
				if u.kind == uPara && isUpperHeading(u.text) && utf8.RuneCountInString(u.text) < 250 && b.inferClass(u.text) == "" {
					b.openAppendix(u.text)
					continue
				}
				b.openAppendix(defaultAttachmentTitle)
				b.lastHeading = -1
			}
			u.class = b.inferClass(u.text)
			u.id = ""
			b.body(u, true)
		case phBody:
			inferred := b.infer
			if inferred && u.kind == uPara {
				u.class = b.inferClass(u.text)
			}
			b.body(u, inferred)
		}
	}
}

// startsAttachment reports whether a unit after the signature opens a
// document issued together with this one, or an appendix.
func (b *builder) startsAttachment(u unit, text string) bool {
	if quocHieuRe.MatchString(text) && b.doc.Stats.ClosingRunes > 0 {
		return true
	}
	if u.kind == uTable {
		// signature / Nơi nhận tables stay closing; any other table is content
		return !signatureRe.MatchString(text)
	}
	return isAppendixHeading(u.text) ||
		(attachedTitleRe.MatchString(u.text) && isUpperHeading(u.text)) ||
		attachedNoteRe.MatchString(u.text) ||
		isTopStructure(u.text) ||
		numberedLineRe.MatchString(u.text) ||
		(isUpperHeading(u.text) && utf8.RuneCountInString(u.text) >= 30 && !signatureRe.MatchString(u.text)) ||
		utf8.RuneCountInString(u.text) > 200
}

var (
	signatureRe    = regexp.MustCompile(`(?i)Nơi nhận|\bTM\.|\bKT\.|\bTL\.|\bTUQ\.|\(đã ký\)|BỘ TRƯỞNG|THỨ TRƯỞNG|CHỦ TỊCH|THỦ TƯỚNG|CHỦ NHIỆM|TỔNG KIỂM TOÁN|CHÁNH ÁN|VIỆN TRƯỞNG|THỐNG ĐỐC|TỔNG GIÁM ĐỐC|TỔNG THƯ KÝ|- Lưu:|Lưu: VT`)
	numberedLineRe = regexp.MustCompile(`^(?:\d+(?:\.\d+)*\.?|[IVXLC]+\.)\s+\S`)
)

// defaultAttachmentTitle names attached text that has no title line.
const defaultAttachmentTitle = "Văn bản kèm theo"

func (b *builder) openAppendix(title string) {
	b.resetStructure(0)
	b.inQuote = false
	b.doc.Stats.Appendices++
	b.appendix = b.emit(Block{Kind: KindAppendix, Text: title, Parent: -1}, "## "+escapeLine(title))
	b.lastHeading = b.appendix
	b.titleLines = 0
}

var (
	inferPartRe    = regexp.MustCompile(`^(?:Phần|PHẦN)\s+(thứ\s+\S+|THỨ\s+\S+|[IVXLC]+\b|\d+)`)
	inferChapterRe = regexp.MustCompile(`^(?:Chương|CHƯƠNG)\s+([IVXLC]+|\d+)`)
	inferSectionRe = regexp.MustCompile(`^(?:Mục|MỤC)\s+(\d+|[IVXLC]+)\b`)
	inferArticleRe = regexp.MustCompile(`^(?:Điều|ĐIỀU|Ðiều)\s*\d+[a-zđ]?\s*(?:[.:]|$)`)
	inferClauseRe  = regexp.MustCompile(`^\d{1,3}\s*[.)]\s`)
	inferItemRe    = regexp.MustCompile(`^[a-zđ]\s*\)\s`)
	numHeadingRe   = regexp.MustCompile(`^(?:[IVXLC]+|\d{1,2})\.\s+\S`)
)

// isTopStructure: a Chương/Phần/Mục/Điều line, usable without context.
func isTopStructure(t string) bool {
	return inferArticleRe.MatchString(t) || inferChapterRe.MatchString(t) || inferPartRe.MatchString(t)
}

// inferClass maps a paragraph of an attached document / appendix / old
// page to a prov-* class from its leading pattern.
func (b *builder) inferClass(t string) string {
	switch {
	case inferArticleRe.MatchString(t):
		return "prov-article"
	case inferChapterRe.MatchString(t):
		return "prov-chapter"
	case inferPartRe.MatchString(t):
		return "prov-part"
	case inferSectionRe.MatchString(t):
		return "prov-section"
	}
	if _, starts := startsHeading(t); !starts && b.lastHeading >= 0 && b.lastHeading != b.appendix && isUpperHeading(t) && utf8.RuneCountInString(t) < 300 {
		// second line of a split heading ("Chương I" / "QUY ĐỊNH CHUNG")
		return string(classOfKind(b.doc.Blocks[b.lastHeading].Kind))
	}
	// "I. QUY ĐỊNH CHUNG", "1. PHẠM VI" headings: one level below the
	// deepest open heading (a part at the top)
	if numHeadingRe.MatchString(t) && (b.article < 0 || romanStartRe.MatchString(t)) && isUpperHeading(t) && utf8.RuneCountInString(t) < 200 {
		style := numStyle(t)
		deepest := 0
		for r := 4; r >= 1; r-- {
			if h := b.heading[r]; h >= 0 {
				if deepest == 0 {
					deepest = r
				}
				if numStyle(b.doc.Blocks[h].Text) == style {
					return classOfKind(b.doc.Blocks[h].Kind) // sibling
				}
			}
		}
		switch {
		case deepest == 0:
			return "prov-part"
		case deepest >= 3:
			return "prov-subsection"
		}
		return "prov-section"
	}
	if b.article >= 0 {
		if inferClauseRe.MatchString(t) {
			return "prov-clause"
		}
		if inferItemRe.MatchString(t) {
			return "prov-item"
		}
		return ""
	}
	return ""
}

// numStyle is "roman", "digit" or "" for a numbered heading line.
func numStyle(t string) string {
	switch {
	case !numHeadingRe.MatchString(t):
		return ""
	case romanStartRe.MatchString(t):
		return "roman"
	}
	return "digit"
}

func classOfKind(k BlockKind) string {
	switch k {
	case KindPart:
		return "prov-part"
	case KindChapter:
		return "prov-chapter"
	case KindSection:
		return "prov-section"
	case KindSubsection:
		return "prov-subsection"
	}
	return ""
}

// body handles one unit of the document body (or of an attached document,
// with inferred classes).
func (b *builder) body(u unit, inferred bool) {
	// Quote spans override classes.
	if !b.inQuote && u.kind == uPara && quoteOpener(u.text) {
		b.inQuote = true
		b.amend = b.amendTarget(u.text)
		b.doc.Stats.QuoteSpans++
	} else if b.inQuote && u.class == "prov-article" && u.kind == uPara && !inferred &&
		!strings.HasPrefix(u.text, "“") && !strings.HasPrefix(u.text, "\"") {
		b.inQuote = false
		b.doc.Stats.ForcedCloses++
	}
	if b.inQuote {
		parent := b.innermost()
		if u.kind == uTable {
			idx := b.emitTable(u, KindQuote, parent, "> ")
			b.doc.Blocks[idx].Amend = b.amend
		} else {
			b.emit(Block{Kind: KindQuote, Text: u.text, Parent: parent, Inferred: inferred, Amend: b.amend}, "> "+escapeLine(u.text))
			if quoteCloser(u.text) && utf8.RuneCountInString(u.text) >= 3 {
				b.inQuote = false
			}
		}
		b.lastHeading = -1
		return
	}

	if u.kind == uTable {
		b.doc.Stats.Tables++
		b.emitTable(u, KindTable, b.innermost(), "")
		b.lastHeading = -1
		return
	}

	if hk := headingKind(u.class); hk != "" && !startsKeyword(u.text) && utf8.RuneCountInString(u.text) > 250 {
		// a long numbered paragraph tagged as a heading (old-style "1. ..."
		// under "I.") is body text: keep it as a clause
		u.class = "prov-clause"
	}
	if hk := headingKind(u.class); hk != "" {
		num, starts := startsHeading(u.text)
		if !starts && b.lastHeading >= 0 && b.doc.Blocks[b.lastHeading].Kind == hk {
			sep := " "
			if labelOnlyRe.MatchString(b.doc.Blocks[b.lastHeading].Text) {
				sep = ". "
			}
			b.appendToLast(sep, u.text)
			return
		}
		rank := headingRank(hk)
		b.resetStructure(rank)
		parent := b.appendix
		for r := rank - 1; r >= 1; r-- {
			if b.heading[r] >= 0 {
				parent = b.heading[r]
				break
			}
		}
		b.doc.Stats.Headings++
		idx := b.emit(Block{Kind: hk, ID: u.id, Number: num, Text: u.text, Parent: parent, Inferred: inferred},
			strings.Repeat("#", rank)+" "+escapeLine(u.text))
		b.heading[rank] = idx
		b.lastHeading = idx
		return
	}
	b.lastHeading = -1

	switch u.class {
	case "prov-article":
		b.resetStructure(5)
		num := ""
		if m := articleNumRe.FindStringSubmatch(u.text); m != nil {
			num = m[1]
		} else {
			b.doc.Stats.UnnumberedArt++
		}
		b.doc.Stats.Articles++
		b.article = b.emit(Block{Kind: KindArticle, ID: u.id, Number: num, Text: u.text, Parent: b.parentHeading(), Inferred: inferred}, "#### "+escapeLine(u.text))
		return
	case "prov-clause":
		b.clause, b.item = -1, -1
		parent := b.innermost()
		num := ""
		if m := clauseNumRe.FindStringSubmatch(u.text); m != nil {
			num = m[1]
		}
		b.doc.Stats.Clauses++
		b.clause = b.emit(Block{Kind: KindClause, ID: u.id, Number: num, Text: u.text, Parent: parent, Inferred: inferred}, escapeLine(u.text))
		return
	case "prov-item":
		b.item = -1
		parent := b.innermost()
		num := ""
		if m := itemNumRe.FindStringSubmatch(u.text); m != nil {
			num = m[1]
		}
		b.doc.Stats.Items++
		b.item = b.emit(Block{Kind: KindItem, ID: u.id, Number: num, Text: u.text, Parent: parent, Inferred: inferred}, escapeLine(u.text))
		return
	}
	// prov-content or unclassed paragraph inside the body.
	b.emit(Block{Kind: KindContent, ID: u.id, Text: u.text, Parent: b.innermost(), Inferred: inferred}, escapeLine(u.text))
}

// resetStructure closes every open block at heading rank >= rank (articles
// count as rank 5, so resetStructure(5) closes article/clause/item only).
func (b *builder) resetStructure(rank int) {
	for r := 4; r >= 1 && r >= rank; r-- {
		b.heading[r] = -1
	}
	b.article, b.clause, b.item = -1, -1, -1
}

// emitPlain writes a preamble/closing/appendix unit. Header tables become
// one line per cell line; other tables stay tables.
func (b *builder) emitPlain(u unit, kind BlockKind, parent int) int {
	if u.kind == uTable {
		if kind == KindPreamble || kind == KindClosing {
			last := -1
			for _, row := range u.table {
				for ci, cell := range row {
					if cell == "" || (ci > 0 && cell == row[ci-1]) {
						continue
					}
					last = b.emit(Block{Kind: kind, Text: cell, Parent: parent}, escapeLine(cell))
				}
			}
			return last
		}
		b.doc.Stats.Tables++
		return b.emitTable(u, KindTable, parent, "")
	}
	return b.emit(Block{Kind: kind, ID: u.id, Text: u.text, Parent: parent}, escapeLine(u.text))
}

// emitTable renders a GFM table (first row as header) and records row offsets.
func (b *builder) emitTable(u unit, kind BlockKind, parent int, prefix string) int {
	rows := u.table
	width := len(rows[0])
	var sb strings.Builder
	row := func(cells []string) string {
		var r strings.Builder
		r.WriteString(prefix + "|")
		for _, c := range cells {
			r.WriteString(" " + strings.ReplaceAll(c, "|", "\\|") + " |")
		}
		return r.String()
	}
	sb.WriteString(row(rows[0]))
	sb.WriteString("\n" + prefix + "|" + strings.Repeat(" --- |", width))
	header := utf8.RuneCountInString(sb.String())
	var starts []int
	var text strings.Builder
	text.WriteString(strings.Join(rows[0], " | "))
	for _, r := range rows[1:] {
		sb.WriteString("\n")
		starts = append(starts, utf8.RuneCountInString(sb.String()))
		sb.WriteString(row(r))
		text.WriteString("\n" + strings.Join(r, " | "))
	}
	idx := b.emit(Block{Kind: kind, Text: text.String(), Parent: parent}, sb.String())
	blk := &b.doc.Blocks[idx]
	blk.HeaderEnd = blk.Start + header
	for _, s := range starts {
		blk.RowStarts = append(blk.RowStarts, blk.Start+s)
	}
	return idx
}

// escapeLine keeps a paragraph from being read as Markdown structure it is
// not: a leading '#' or '>' would turn body text into a heading or quote.
func escapeLine(t string) string {
	if t != "" && (t[0] == '#' || t[0] == '>') {
		return "\\" + t
	}
	return t
}

func isUpperHeading(t string) bool {
	letters, upper := 0, 0
	for _, r := range t {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsUpper(r) {
				upper++
			}
		}
		if letters >= 40 {
			break
		}
	}
	return letters > 0 && upper*10 >= letters*7
}

func (b *builder) finish() *Document {
	d := b.doc
	d.Markdown = b.md.String()
	d.Structured = d.Stats.Articles > 0 || (d.Stats.Headings > 0 && d.Stats.Clauses > 0)
	if d.Stats.UnnumberedArt > 0 {
		d.Warnings = append(d.Warnings, fmt.Sprintf("%d article(s) without a parsable number", d.Stats.UnnumberedArt))
	}
	if d.Stats.ForcedCloses > 0 {
		d.Warnings = append(d.Warnings, fmt.Sprintf("%d quote span(s) closed at a real article", d.Stats.ForcedCloses))
	}
	return d
}

// LooksLikeVBPL reports whether raw HTML carries vbpl.vn provision markup.
// Older pages tag only prov-section / prov-item, so any structural class
// counts; Convert's Structured flag decides whether the structure is usable.
func LooksLikeVBPL(raw []byte) bool {
	for _, c := range provStructureClasses {
		if bytes.Contains(raw, []byte(c)) {
			return true
		}
	}
	return false
}

var provStructureClasses = []string{
	"prov-article", "prov-clause", "prov-item", "prov-part", "prov-chapter", "prov-section", "prov-subsection",
}
