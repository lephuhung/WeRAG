// Package docxwrite builds a minimal .docx from a flat list of paragraphs:
// Times New Roman 14 pt on A4 with the NĐ30 margins, the paragraph styles
// Normal, Heading1–Heading4 and PageMarker, and the character style
// "WeRAG gạch chân" the ONLYOFFICE assistant plugin underlines live
// spelling findings with. It is enough for a document a person edits in
// the embedded editor (the OCR drafts of legal documents). Paragraphs can
// also carry formatted runs, list numbering and indents, and the body can
// hold simple tables (Document.Blocks), enough for a chat answer exported
// from Markdown; images and footnotes are out of scope.
package docxwrite

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"time"
)

// Paragraph styles Build knows.
const (
	StyleNormal     = "Normal"
	StyleHeading1   = "Heading1"
	StyleHeading2   = "Heading2"
	StyleHeading3   = "Heading3"
	StyleHeading4   = "Heading4"
	StylePageMarker = "PageMarker"
	// StylePageWarning flags a page whose OCR is broken (red, bold italic).
	StylePageWarning = "PageWarning"
	// StyleQuote is a block quote: grey text with a rule on the left.
	StyleQuote = "Quote"
	// StyleMeta is a small grey note (who wrote the document, when).
	StyleMeta = "Meta"
	// StyleRule is an empty paragraph drawn as a horizontal line.
	StyleRule = "HorizontalRule"
	// StyleCodeBlock is preformatted text in Courier New 12 pt.
	StyleCodeBlock = "CodeBlock"
	// StyleTableText is the 13 pt, unspaced text of a table cell.
	StyleTableText = "TableText"
)

// MarkStyleName is the name of the character style the ONLYOFFICE assistant
// plugin (docker/onlyoffice/plugins/werag-assistant/plugin.js, MARK_STYLE)
// looks up by name and applies to the runs it underlines. Defining it here
// keeps the plugin from creating it on first use, with the same properties.
const MarkStyleName = "WeRAG gạch chân"

// MarkStyleID is the style id of MarkStyleName in styles.xml.
const MarkStyleID = "WeRAGgachchan"

// Paragraph is one paragraph of the document. Text may hold "\t" (a tab)
// and "\n" (a line break inside the paragraph).
type Paragraph struct {
	Text string
	// Runs, when set, replace Text: each run carries its own character
	// formatting (Bold below still applies to every run).
	Runs []Run
	// Style is one of the Style* constants; "" is Normal, an unknown
	// style is an error.
	Style  string
	Bold   bool
	Center bool
	// Align overrides the style's alignment: "left", "center", "right" or
	// "both" ("" keeps the style's).
	Align string
	// List makes the paragraph a list item.
	List *ListItem
	// IndentTwips is extra left indent (block quotes, the follow-up
	// paragraphs of a list item).
	IndentTwips int
}

// Run is a stretch of text with one character format. Text may hold "\t"
// and "\n" like Paragraph.Text.
type Run struct {
	Text   string
	Bold   bool
	Italic bool
	Strike bool
	// Code sets the text in Courier New on a light grey background.
	Code bool
	// SizeHalfPt overrides the font size (half points; 0 keeps the style's).
	SizeHalfPt int
	// Color is an RRGGBB colour ("" keeps the style's).
	Color string
}

// ListItem places a paragraph in a bulleted or numbered list. Items with
// the same List key share one numbering sequence, which starts at Start
// (0 counts as 1); bulleted items ignore List and Start.
type ListItem struct {
	Ordered bool
	// Level is the nesting depth, 0 for the outermost list (at most 8).
	Level int
	List  int
	Start int
}

// Table is a simple grid of paragraphs with single borders. The first
// HeaderRows rows repeat on each page and are set bold on grey.
type Table struct {
	Rows       [][]TableCell
	HeaderRows int
}

// TableCell is one cell; an empty cell still gets an empty paragraph.
type TableCell struct {
	Paragraphs []Paragraph
}

// Block is a top-level body element: a Paragraph or a Table.
type Block interface{ isBlock() }

func (Paragraph) isBlock() {}
func (Table) isBlock()     {}

// Document is what Build writes.
type Document struct {
	Paragraphs []Paragraph
	// Blocks follow Paragraphs in the body.
	Blocks []Block
	// Title goes into the package's core properties (optional).
	Title string
}

var knownStyles = map[string]bool{
	StyleNormal: true, StyleHeading1: true, StyleHeading2: true, StyleHeading3: true,
	StyleHeading4: true, StylePageMarker: true, StylePageWarning: true,
	StyleQuote: true, StyleMeta: true, StyleRule: true, StyleCodeBlock: true, StyleTableText: true,
}

// textWidthTwips is the A4 width between the NĐ30 margins (see sectPrXML).
const textWidthTwips = 11906 - 1701 - 851

// Build writes doc as a .docx package.
func Build(doc Document) ([]byte, error) {
	w := &bodyWriter{orderedNums: map[int]int{}}
	for i, p := range doc.Paragraphs {
		if err := w.paragraph(p); err != nil {
			return nil, fmt.Errorf("paragraph %d: %w", i, err)
		}
	}
	for i, blk := range doc.Blocks {
		var err error
		switch v := blk.(type) {
		case Paragraph:
			err = w.paragraph(v)
		case *Paragraph:
			err = w.paragraph(*v)
		case Table:
			err = w.table(v)
		case *Table:
			err = w.table(*v)
		default:
			err = fmt.Errorf("unknown block %T", blk)
		}
		if err != nil {
			return nil, fmt.Errorf("block %d: %w", i, err)
		}
	}
	if len(doc.Paragraphs) == 0 && len(doc.Blocks) == 0 {
		w.b.WriteString("<w:p/>")
	}
	contentTypes, docRels := contentTypesXML, documentRelsXML
	parts := []struct{ name, content string }{
		{"[Content_Types].xml", ""},
		{"_rels/.rels", rootRelsXML},
		{"docProps/core.xml", coreXML(doc.Title)},
		{"docProps/app.xml", appXML},
		{"word/document.xml", documentXMLHead + w.b.String() + sectPrXML + documentXMLTail},
		{"word/_rels/document.xml.rels", ""},
		{"word/styles.xml", stylesXML},
		{"word/settings.xml", settingsXML},
	}
	if w.usesNumbering {
		contentTypes = strings.Replace(contentTypes, "</Types>", numberingContentType+"</Types>", 1)
		docRels = strings.Replace(docRels, "</Relationships>", numberingRel+"</Relationships>", 1)
		parts = append(parts, struct{ name, content string }{"word/numbering.xml", w.numberingXML()})
	}
	parts[0].content, parts[5].content = contentTypes, docRels
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, part := range parts {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: part.name, Method: zip.Deflate, Modified: fixedTime})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(part.content)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fixedTime keeps the zip bytes of the same document identical.
var fixedTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// bodyWriter writes the body and remembers the numbering it needs.
type bodyWriter struct {
	b             strings.Builder
	usesNumbering bool
	// orderedNums maps a ListItem.List key to its w:num id; orderedOrder
	// keeps the keys in first-use order with their start values.
	orderedNums  map[int]int
	orderedOrder []orderedNum
}

type orderedNum struct {
	numID, level, start int
}

// bulletNumID is the w:num of every bulleted item; ordered lists get ids
// from 2 on.
const bulletNumID = 1

func (w *bodyWriter) numID(l *ListItem) int {
	w.usesNumbering = true
	if !l.Ordered {
		return bulletNumID
	}
	if id, ok := w.orderedNums[l.List]; ok {
		return id
	}
	id := len(w.orderedOrder) + 2
	start := l.Start
	if start < 1 {
		start = 1
	}
	w.orderedNums[l.List] = id
	w.orderedOrder = append(w.orderedOrder, orderedNum{numID: id, level: clampLevel(l.Level), start: start})
	return id
}

func clampLevel(l int) int {
	if l < 0 {
		return 0
	}
	if l > 8 {
		return 8
	}
	return l
}

// listIndent is the left indent of list level lvl's text; the number hangs
// listHanging twips before it.
func listIndent(lvl int) int { return 567 * (clampLevel(lvl) + 1) }

const listHanging = 360

// ListTextIndent is the left indent of the text of a level-lvl list item,
// for paragraphs that continue the item.
func ListTextIndent(lvl int) int { return listIndent(lvl) }

func (w *bodyWriter) paragraph(p Paragraph) error {
	style := p.Style
	if style == "" {
		style = StyleNormal
	}
	if !knownStyles[style] {
		return fmt.Errorf("unknown style %q", p.Style)
	}
	writeParagraph(&w.b, style, p, w)
	return nil
}

func (w *bodyWriter) table(t Table) error {
	cols := 0
	for _, row := range t.Rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	if cols == 0 {
		return nil
	}
	colW := textWidthTwips / cols
	b := &w.b
	b.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="5000" w:type="pct"/><w:tblBorders>`)
	for _, side := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
		b.WriteString(`<w:` + side + ` w:val="single" w:sz="4" w:space="0" w:color="808080"/>`)
	}
	b.WriteString(`</w:tblBorders><w:tblLayout w:type="autofit"/><w:tblCellMar><w:left w:w="85" w:type="dxa"/><w:right w:w="85" w:type="dxa"/></w:tblCellMar>`)
	b.WriteString(`<w:tblLook w:val="04A0" w:firstRow="1" w:lastRow="0" w:firstColumn="0" w:lastColumn="0" w:noHBand="0" w:noVBand="1"/></w:tblPr><w:tblGrid>`)
	for i := 0; i < cols; i++ {
		b.WriteString(fmt.Sprintf(`<w:gridCol w:w="%d"/>`, colW))
	}
	b.WriteString(`</w:tblGrid>`)
	for r, row := range t.Rows {
		header := r < t.HeaderRows
		b.WriteString("<w:tr>")
		if header {
			b.WriteString(`<w:trPr><w:tblHeader/></w:trPr>`)
		}
		for c := 0; c < cols; c++ {
			b.WriteString(fmt.Sprintf(`<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>`, colW))
			if header {
				b.WriteString(`<w:shd w:val="clear" w:color="auto" w:fill="E7E6E6"/>`)
			}
			b.WriteString(`</w:tcPr>`)
			var paras []Paragraph
			if c < len(row) {
				paras = row[c].Paragraphs
			}
			if len(paras) == 0 {
				paras = []Paragraph{{Style: StyleTableText}}
			}
			for _, p := range paras {
				if p.Style == "" {
					p.Style = StyleTableText
				}
				if header {
					p.Bold = true
				}
				if err := w.paragraph(p); err != nil {
					return fmt.Errorf("row %d cell %d: %w", r, c, err)
				}
			}
			b.WriteString("</w:tc>")
		}
		b.WriteString("</w:tr>")
	}
	b.WriteString("</w:tbl>")
	// Word wants a paragraph between two tables and after a table that
	// ends the body; a small empty one does no harm elsewhere.
	b.WriteString(`<w:p><w:pPr><w:spacing w:after="0"/></w:pPr></w:p>`)
	return nil
}

func writeParagraph(b *strings.Builder, style string, p Paragraph, w *bodyWriter) {
	b.WriteString("<w:p><w:pPr>")
	if style != StyleNormal {
		b.WriteString(`<w:pStyle w:val="` + style + `"/>`)
	}
	if p.List != nil {
		lvl := clampLevel(p.List.Level)
		b.WriteString(fmt.Sprintf(`<w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr>`, lvl, w.numID(p.List)))
		b.WriteString(fmt.Sprintf(`<w:ind w:left="%d" w:hanging="%d"/>`, listIndent(lvl)+p.IndentTwips, listHanging))
	} else if p.IndentTwips > 0 {
		b.WriteString(fmt.Sprintf(`<w:ind w:left="%d"/>`, p.IndentTwips))
	}
	switch {
	case p.Align == "left" || p.Align == "center" || p.Align == "right" || p.Align == "both":
		b.WriteString(`<w:jc w:val="` + p.Align + `"/>`)
	case p.Center:
		b.WriteString(`<w:jc w:val="center"/>`)
	}
	b.WriteString("</w:pPr>")
	runs := p.Runs
	if len(runs) == 0 {
		if p.Text == "" {
			b.WriteString("</w:p>")
			return
		}
		runs = []Run{{Text: p.Text}}
	}
	for _, r := range runs {
		if p.Bold {
			r.Bold = true
		}
		writeRun(b, r)
	}
	b.WriteString("</w:p>")
}

func writeRun(b *strings.Builder, r Run) {
	text := Clean(r.Text)
	if text == "" {
		return
	}
	var rPr strings.Builder
	if r.Code {
		rPr.WriteString(`<w:rFonts w:ascii="Courier New" w:hAnsi="Courier New" w:cs="Courier New"/>`)
	}
	if r.Bold {
		rPr.WriteString("<w:b/><w:bCs/>")
	}
	if r.Italic {
		rPr.WriteString("<w:i/><w:iCs/>")
	}
	if r.Strike {
		rPr.WriteString("<w:strike/>")
	}
	if r.Color != "" {
		rPr.WriteString(`<w:color w:val="` + escape(r.Color) + `"/>`)
	}
	size := r.SizeHalfPt
	if size == 0 && r.Code {
		size = 24
	}
	if size > 0 {
		rPr.WriteString(fmt.Sprintf(`<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, size, size))
	}
	if r.Code {
		rPr.WriteString(`<w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/>`)
	}
	b.WriteString("<w:r>")
	if rPr.Len() > 0 {
		b.WriteString("<w:rPr>" + rPr.String() + "</w:rPr>")
	}
	var seg strings.Builder
	flush := func() {
		if seg.Len() > 0 {
			b.WriteString(`<w:t xml:space="preserve">` + escape(seg.String()) + "</w:t>")
			seg.Reset()
		}
	}
	for _, c := range text {
		switch c {
		case '\t':
			flush()
			b.WriteString("<w:tab/>")
		case '\n':
			flush()
			b.WriteString("<w:br/>")
		default:
			seg.WriteRune(c)
		}
	}
	flush()
	b.WriteString("</w:r>")
}

// bulletGlyphs cycle through the bullet levels.
var bulletGlyphs = []string{"•", "◦", "▪"}

// numberingXML defines one bullet and one decimal abstract numbering
// (nine levels each), the shared bullet num and one num per ordered list.
func (w *bodyWriter) numberingXML() string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	b.WriteString(`<w:abstractNum w:abstractNumId="0"><w:multiLevelType w:val="hybridMultilevel"/>`)
	for l := 0; l < 9; l++ {
		b.WriteString(fmt.Sprintf(`<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="%s"/>`+
			`<w:lvlJc w:val="left"/><w:pPr><w:ind w:left="%d" w:hanging="%d"/></w:pPr>`+
			`<w:rPr><w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman" w:cs="Times New Roman"/></w:rPr></w:lvl>`,
			l, bulletGlyphs[l%len(bulletGlyphs)], listIndent(l), listHanging))
	}
	b.WriteString(`</w:abstractNum><w:abstractNum w:abstractNumId="1"><w:multiLevelType w:val="hybridMultilevel"/>`)
	for l := 0; l < 9; l++ {
		b.WriteString(fmt.Sprintf(`<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%%%d."/>`+
			`<w:lvlJc w:val="left"/><w:pPr><w:ind w:left="%d" w:hanging="%d"/></w:pPr></w:lvl>`,
			l, l+1, listIndent(l), listHanging))
	}
	b.WriteString(`</w:abstractNum>`)
	b.WriteString(fmt.Sprintf(`<w:num w:numId="%d"><w:abstractNumId w:val="0"/></w:num>`, bulletNumID))
	for _, n := range w.orderedOrder {
		b.WriteString(fmt.Sprintf(`<w:num w:numId="%d"><w:abstractNumId w:val="1"/>`+
			`<w:lvlOverride w:ilvl="%d"><w:startOverride w:val="%d"/></w:lvlOverride></w:num>`, n.numID, n.level, n.start))
	}
	b.WriteString(`</w:numbering>`)
	return b.String()
}

const numberingContentType = `<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>`

const numberingRel = `<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>`

// Clean drops the runes XML 1.0 cannot carry (control characters other
// than tab and line feed, lone surrogates, U+FFFE/U+FFFF) and turns "\r"
// into nothing; OCR output carries them now and then.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n':
			return r
		case r < 0x20:
			return -1
		case r >= 0xD800 && r <= 0xDFFF, r == 0xFFFE, r == 0xFFFF:
			return -1
		}
		return r
	}, s)
}

var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")

func escape(s string) string { return xmlEscaper.Replace(s) }

func coreXML(title string) string {
	return xmlHeader + `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" ` +
		`xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" ` +
		`xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
		`<dc:title>` + escape(Clean(title)) + `</dc:title><dc:creator>WeRAG</dc:creator></cp:coreProperties>`
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

const contentTypesXML = xmlHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
	`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
	`<Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>` +
	`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
	`<Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>` +
	`</Types>`

const rootRelsXML = xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
	`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/>` +
	`</Relationships>`

const appXML = xmlHeader + `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties">` +
	`<Application>WeRAG</Application></Properties>`

const documentRelsXML = xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>` +
	`</Relationships>`

const documentXMLHead = xmlHeader + `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>`

// sectPrXML: A4 (210 × 297 mm) with the NĐ30 margins — top and bottom
// 20 mm, left 30 mm, right 15 mm (twips: 1 mm ≈ 56.7).
const sectPrXML = `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>` +
	`<w:pgMar w:top="1134" w:right="851" w:bottom="1134" w:left="1701" w:header="567" w:footer="567" w:gutter="0"/>` +
	`</w:sectPr>`

const documentXMLTail = `</w:body></w:document>`

const settingsXML = xmlHeader + `<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	`<w:defaultTabStop w:val="720"/><w:characterSpacingControl w:val="doNotCompress"/>` +
	`<w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat>` +
	`</w:settings>`

// headingStyle is a bold, keep-with-next heading style of outline level lvl.
func headingStyle(id, name string, lvl int, center bool) string {
	jc := ""
	if center {
		jc = `<w:jc w:val="center"/>`
	}
	return fmt.Sprintf(`<w:style w:type="paragraph" w:styleId="%s"><w:name w:val="%s"/><w:basedOn w:val="Normal"/>`+
		`<w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="120" w:after="120"/>%s`+
		`<w:outlineLvl w:val="%d"/></w:pPr><w:rPr><w:b/><w:bCs/></w:rPr></w:style>`, id, name, jc, lvl)
}

var stylesXML = xmlHeader + `<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	`<w:docDefaults><w:rPrDefault><w:rPr>` +
	`<w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman" w:eastAsia="Times New Roman" w:cs="Times New Roman"/>` +
	`<w:sz w:val="28"/><w:szCs w:val="28"/><w:lang w:val="vi-VN" w:eastAsia="vi-VN" w:bidi="ar-SA"/>` +
	`</w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:before="0" w:after="120" w:line="276" w:lineRule="auto"/>` +
	`</w:pPr></w:pPrDefault></w:docDefaults>` +
	`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/>` +
	`<w:pPr><w:jc w:val="both"/></w:pPr></w:style>` +
	headingStyle(StyleHeading1, "heading 1", 0, true) +
	headingStyle(StyleHeading2, "heading 2", 1, true) +
	headingStyle(StyleHeading3, "heading 3", 2, true) +
	headingStyle(StyleHeading4, "heading 4", 3, false) +
	`<w:style w:type="paragraph" w:customStyle="1" w:styleId="PageMarker"><w:name w:val="PageMarker"/>` +
	`<w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:jc w:val="right"/><w:spacing w:before="120" w:after="120"/></w:pPr>` +
	`<w:rPr><w:i/><w:iCs/><w:color w:val="808080"/><w:sz w:val="20"/><w:szCs w:val="20"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:customStyle="1" w:styleId="PageWarning"><w:name w:val="PageWarning"/>` +
	`<w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:spacing w:before="60" w:after="120"/></w:pPr>` +
	`<w:rPr><w:b/><w:bCs/><w:i/><w:iCs/><w:color w:val="FF0000"/><w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:customStyle="1" w:styleId="Quote"><w:name w:val="Quote"/>` +
	`<w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:pBdr><w:left w:val="single" w:sz="18" w:space="8" w:color="BFBFBF"/></w:pBdr>` +
	`<w:ind w:left="284"/></w:pPr><w:rPr><w:color w:val="404040"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:customStyle="1" w:styleId="Meta"><w:name w:val="Meta"/>` +
	`<w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:spacing w:after="60"/><w:jc w:val="left"/></w:pPr>` +
	`<w:rPr><w:color w:val="595959"/><w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:customStyle="1" w:styleId="HorizontalRule"><w:name w:val="Horizontal Rule"/>` +
	`<w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="A6A6A6"/></w:pBdr>` +
	`<w:spacing w:before="60" w:after="180"/></w:pPr><w:rPr><w:sz w:val="8"/><w:szCs w:val="8"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:customStyle="1" w:styleId="CodeBlock"><w:name w:val="Code Block"/>` +
	`<w:basedOn w:val="Normal"/><w:pPr><w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/><w:spacing w:after="120" w:line="240" w:lineRule="auto"/><w:jc w:val="left"/></w:pPr>` +
	`<w:rPr><w:rFonts w:ascii="Courier New" w:hAnsi="Courier New" w:cs="Courier New"/><w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:customStyle="1" w:styleId="TableText"><w:name w:val="Table Text"/>` +
	`<w:basedOn w:val="Normal"/><w:pPr><w:spacing w:before="40" w:after="40" w:line="240" w:lineRule="auto"/><w:jc w:val="left"/></w:pPr>` +
	`<w:rPr><w:sz w:val="26"/><w:szCs w:val="26"/></w:rPr></w:style>` +
	`<w:style w:type="character" w:default="1" w:styleId="DefaultParagraphFont"><w:name w:val="Default Paragraph Font"/>` +
	`<w:uiPriority w:val="1"/><w:semiHidden/><w:unhideWhenUsed/></w:style>` +
	// the plugin's mark style: underline only, out of the style gallery
	`<w:style w:type="character" w:customStyle="1" w:styleId="` + MarkStyleID + `"><w:name w:val="` + MarkStyleName + `"/>` +
	`<w:basedOn w:val="DefaultParagraphFont"/><w:rPr><w:u w:val="single"/></w:rPr></w:style>` +
	`</w:styles>`
