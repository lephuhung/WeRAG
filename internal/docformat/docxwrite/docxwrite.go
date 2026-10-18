// Package docxwrite builds a minimal .docx from a flat list of paragraphs:
// Times New Roman 14 pt on A4 with the NĐ30 margins, the paragraph styles
// Normal, Heading1–Heading4 and PageMarker, and the character style
// "WeRAG gạch chân" the ONLYOFFICE assistant plugin underlines live
// spelling findings with. It is enough for a document a person edits in
// the embedded editor (the OCR drafts of legal documents); anything richer
// (tables, numbering, images) is out of scope.
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
	// Style is one of the Style* constants; "" is Normal, an unknown
	// style is an error.
	Style  string
	Bold   bool
	Center bool
}

// Document is what Build writes.
type Document struct {
	Paragraphs []Paragraph
	// Title goes into the package's core properties (optional).
	Title string
}

var knownStyles = map[string]bool{
	StyleNormal: true, StyleHeading1: true, StyleHeading2: true, StyleHeading3: true,
	StyleHeading4: true, StylePageMarker: true,
}

// Build writes doc as a .docx package.
func Build(doc Document) ([]byte, error) {
	var body strings.Builder
	for i, p := range doc.Paragraphs {
		style := p.Style
		if style == "" {
			style = StyleNormal
		}
		if !knownStyles[style] {
			return nil, fmt.Errorf("paragraph %d: unknown style %q", i, p.Style)
		}
		writeParagraph(&body, style, p)
	}
	if len(doc.Paragraphs) == 0 {
		body.WriteString("<w:p/>")
	}
	parts := []struct{ name, content string }{
		{"[Content_Types].xml", contentTypesXML},
		{"_rels/.rels", rootRelsXML},
		{"docProps/core.xml", coreXML(doc.Title)},
		{"docProps/app.xml", appXML},
		{"word/document.xml", documentXMLHead + body.String() + sectPrXML + documentXMLTail},
		{"word/_rels/document.xml.rels", documentRelsXML},
		{"word/styles.xml", stylesXML},
		{"word/settings.xml", settingsXML},
	}
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

func writeParagraph(b *strings.Builder, style string, p Paragraph) {
	b.WriteString("<w:p><w:pPr>")
	if style != StyleNormal {
		b.WriteString(`<w:pStyle w:val="` + style + `"/>`)
	}
	if p.Center {
		b.WriteString(`<w:jc w:val="center"/>`)
	}
	b.WriteString("</w:pPr>")
	if p.Text == "" {
		b.WriteString("</w:p>")
		return
	}
	rPr := ""
	if p.Bold {
		rPr = "<w:rPr><w:b/><w:bCs/></w:rPr>"
	}
	b.WriteString("<w:r>" + rPr)
	text := Clean(p.Text)
	var seg strings.Builder
	flush := func() {
		if seg.Len() > 0 {
			b.WriteString(`<w:t xml:space="preserve">` + escape(seg.String()) + "</w:t>")
			seg.Reset()
		}
	}
	for _, r := range text {
		switch r {
		case '\t':
			flush()
			b.WriteString("<w:tab/>")
		case '\n':
			flush()
			b.WriteString("<w:br/>")
		default:
			seg.WriteRune(r)
		}
	}
	flush()
	b.WriteString("</w:r></w:p>")
}

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
	`<w:style w:type="character" w:default="1" w:styleId="DefaultParagraphFont"><w:name w:val="Default Paragraph Font"/>` +
	`<w:uiPriority w:val="1"/><w:semiHidden/><w:unhideWhenUsed/></w:style>` +
	// the plugin's mark style: underline only, out of the style gallery
	`<w:style w:type="character" w:customStyle="1" w:styleId="` + MarkStyleID + `"><w:name w:val="` + MarkStyleName + `"/>` +
	`<w:basedOn w:val="DefaultParagraphFont"/><w:rPr><w:u w:val="single"/></w:rPr></w:style>` +
	`</w:styles>`
