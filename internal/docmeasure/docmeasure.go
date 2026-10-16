// Package docmeasure sizes an uploaded document before it is parsed, so an
// upload gate can refuse one that would keep the parser or the OCR model
// busy for minutes. It reads only the file's own structure (the PDF page
// tree, the DOCX package properties) and never renders a page, so a
// measurement costs milliseconds even for a large file.
package docmeasure

import (
	"archive/zip"
	"bytes"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

// Measure is what the gate knows about an upload.
type Measure struct {
	// Pages is the page count (PDF) or an estimate of it (DOCX). Zero
	// when the file could not be measured; the parser then decides.
	Pages int
	// Scanned is set when the PDF carries no text: its pages have no font
	// resources and show images, so every page would go through OCR.
	Scanned bool
	// Estimated marks a DOCX page count derived from text length and page
	// breaks because the package stores none.
	Estimated bool
}

const (
	// pdfSamplePages bounds how many pages are inspected for fonts: the
	// first pages of a scan are the same as the rest, and a large PDF with
	// a text layer shows one within a few pages.
	pdfSamplePages = 10
	// DocxRunesPerPage is the text an A4 page of Times New Roman 14 holds,
	// the layout of Vietnamese administrative documents (NĐ 30/2020).
	DocxRunesPerPage = 2000
)

var (
	docxPagesPattern     = regexp.MustCompile(`<Pages>\s*(\d+)\s*</Pages>`)
	docxPageBreakPattern = regexp.MustCompile(`<w:br\s+[^>]*w:type="page"|<w:pageBreakBefore\b|<w:sectPr\b`)
	docxTagPattern       = regexp.MustCompile(`<[^>]+>`)
)

// MeasurePDF counts the pages of a PDF and tells whether it is a scan. A
// file the library cannot read (damaged, encrypted, not a PDF) yields a
// zero Measure and no error: the gate lets the parser report it.
func MeasurePDF(data []byte) (m Measure) {
	defer func() {
		// the reader panics on some malformed files
		if r := recover(); r != nil {
			m = Measure{}
		}
	}()
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Measure{}
	}
	m.Pages = reader.NumPage()
	if m.Pages <= 0 {
		return Measure{}
	}
	sampled, withFonts, withImages := 0, 0, 0
	step := 1
	if m.Pages > pdfSamplePages {
		step = m.Pages / pdfSamplePages
	}
	for i := 1; i <= m.Pages && sampled < pdfSamplePages; i += step {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		sampled++
		res := page.Resources()
		if fonts := res.Key("Font"); !fonts.IsNull() && len(fonts.Keys()) > 0 {
			withFonts++
		}
		if xobjects := res.Key("XObject"); !xobjects.IsNull() {
			for _, key := range xobjects.Keys() {
				if xobjects.Key(key).Key("Subtype").Name() == "Image" {
					withImages++
					break
				}
			}
		}
	}
	// A scan: no sampled page has a font and at least one shows an image.
	// A PDF with neither (empty pages, vector drawings) is treated as text:
	// it costs the parser nothing.
	m.Scanned = sampled > 0 && withFonts == 0 && withImages > 0
	return m
}

// MeasureDOCX returns the page count Word stored in docProps/app.xml, or,
// when the package has none (LibreOffice, generated files), an estimate
// from the text length and the explicit page breaks of word/document.xml.
// A file that is not a DOCX package yields a zero Measure.
func MeasureDOCX(data []byte) Measure {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Measure{}
	}
	if app := readZipEntry(zr, "docProps/app.xml", 1<<20); app != nil {
		if sub := docxPagesPattern.FindSubmatch(app); sub != nil {
			if n, err := strconv.Atoi(string(sub[1])); err == nil && n > 0 {
				return Measure{Pages: n}
			}
		}
	}
	body := readZipEntry(zr, "word/document.xml", 64<<20)
	if body == nil {
		return Measure{}
	}
	breaks := len(docxPageBreakPattern.FindAllIndex(body, -1))
	text := docxTagPattern.ReplaceAll(body, nil)
	runes := utf8.RuneCount(bytes.TrimSpace(text))
	byText := (runes + DocxRunesPerPage - 1) / DocxRunesPerPage
	pages := byText
	if breaks+1 > pages {
		pages = breaks + 1
	}
	if pages < 1 {
		pages = 1
	}
	return Measure{Pages: pages, Estimated: true}
}

// MeasureFile sizes an upload by extension (with or without the dot). Other
// formats are not measured: images are one page, text has no pages.
func MeasureFile(ext string, data []byte) Measure {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "pdf":
		return MeasurePDF(data)
	case "docx":
		return MeasureDOCX(data)
	}
	return Measure{}
}

func readZipEntry(zr *zip.Reader, name string, limit int64) []byte {
	for _, f := range zr.File {
		if !strings.EqualFold(f.Name, name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil
		}
		defer rc.Close()
		out, err := io.ReadAll(io.LimitReader(rc, limit))
		if err != nil {
			return nil
		}
		return out
	}
	return nil
}
