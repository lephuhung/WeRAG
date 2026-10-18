package service

import (
	"archive/zip"
	"bytes"
	"io"
	"regexp"
	"strings"
)

// emptyDocxMaxPart caps how much of a part is read; a larger part is not
// judged (a blank document's parts are a few KB).
const emptyDocxMaxPart = 4 << 20

var (
	// a text run of any prefix (w:t, m:t in math, a:t in DrawingML)
	docxTextRunRe = regexp.MustCompile(`<\w+:t(?:\s[^>]*)?>([^<]*)</\w+:t>`)
	// content that is not a text run: pictures, objects, embedded HTML,
	// text boxes, notes, fields, symbols, tracked changes, alternatives
	docxObjectRe = regexp.MustCompile(`<(?:w:(?:drawing|pict|object|altChunk|txbxContent|footnoteReference|endnoteReference|instrText|fldSimple|sym|ins|del)|mc:AlternateContent)[\s>/]`)
)

// emptyDocx tells whether data is a .docx with no text and no picture or
// object in its body, headers or footers: the empty document of "Tạo văn
// bản mới" or of a switch to the document assistant. The parser fails on it
// (or hangs, holding the owner's parse slot), and there is nothing to read.
// Anything it does not recognise counts as content.
func emptyDocx(data []byte) bool {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	body := false
	for _, f := range z.File {
		main := f.Name == "word/document.xml"
		side := strings.HasPrefix(f.Name, "word/header") || strings.HasPrefix(f.Name, "word/footer")
		if !main && !side {
			continue
		}
		xml, ok := readDocxPart(f)
		if !ok || (main && !bytes.Contains(xml, []byte("<w:document"))) || docxPartHasContent(xml) {
			return false
		}
		body = body || main
	}
	return body
}

func readDocxPart(f *zip.File) ([]byte, bool) {
	rc, err := f.Open()
	if err != nil {
		return nil, false
	}
	defer rc.Close()
	xml, err := io.ReadAll(io.LimitReader(rc, emptyDocxMaxPart+1))
	return xml, err == nil && len(xml) <= emptyDocxMaxPart
}

func docxPartHasContent(xml []byte) bool {
	if docxObjectRe.Match(xml) {
		return true
	}
	for _, m := range docxTextRunRe.FindAllSubmatch(xml, -1) {
		if len(bytes.TrimSpace(m[1])) > 0 {
			return true
		}
	}
	return false
}
