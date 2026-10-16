package docmeasure

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// buildPDF writes a minimal PDF with n pages; each page gets the given
// resources dictionary body.
func buildPDF(n int, resources string) []byte {
	var b bytes.Buffer
	var offsets []int
	obj := func(body string) {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	b.WriteString("%PDF-1.4\n")
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	kids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", 3+i))
	}
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	for i := 0; i < n; i++ {
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << %s >> >>", resources))
	}
	// shared resource objects referenced by the dictionaries below
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return b.Bytes()
}

func TestMeasurePDFCountsPagesOfATextPDF(t *testing.T) {
	data := buildPDF(23, "/Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >>")
	m := MeasurePDF(data)
	if m.Pages != 23 || m.Scanned {
		t.Fatalf("got %+v, want 23 text pages", m)
	}
}

func TestMeasurePDFDetectsAScan(t *testing.T) {
	data := buildPDF(7, "/XObject << /Im0 << /Type /XObject /Subtype /Image /Width 1 /Height 1 >> >>")
	m := MeasurePDF(data)
	if m.Pages != 7 || !m.Scanned {
		t.Fatalf("got %+v, want 7 scanned pages", m)
	}
}

func TestMeasurePDFIgnoresAFileThatIsNotAPDF(t *testing.T) {
	if m := MeasurePDF([]byte("hello, not a pdf")); m.Pages != 0 {
		t.Fatalf("got %+v, want zero", m)
	}
}

func buildDOCX(t *testing.T, app, document string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	if app != "" {
		w, _ := zw.Create("docProps/app.xml")
		_, _ = w.Write([]byte(app))
	}
	w, _ := zw.Create("word/document.xml")
	_, _ = w.Write([]byte(document))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestMeasureDOCXReadsTheStoredPageCount(t *testing.T) {
	data := buildDOCX(t, `<Properties><Pages>42</Pages></Properties>`, `<w:document><w:body><w:p><w:r><w:t>x</w:t></w:r></w:p></w:body></w:document>`)
	m := MeasureDOCX(data)
	if m.Pages != 42 || m.Estimated {
		t.Fatalf("got %+v, want 42 stored pages", m)
	}
}

func TestMeasureDOCXEstimatesFromTextAndPageBreaks(t *testing.T) {
	text := strings.Repeat("Điều 1. Phạm vi điều chỉnh của văn bản này. ", 200) // ~9000 runes → 5 pages
	doc := `<w:document><w:body><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:body></w:document>`
	m := MeasureDOCX(buildDOCX(t, "", doc))
	if m.Pages != 5 || !m.Estimated {
		t.Fatalf("got %+v, want 5 estimated pages", m)
	}
	breaks := strings.Repeat(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`, 8)
	m = MeasureDOCX(buildDOCX(t, "", `<w:document><w:body>`+breaks+`<w:p><w:r><w:t>x</w:t></w:r></w:p></w:body></w:document>`))
	if m.Pages != 9 {
		t.Fatalf("got %+v, want 9 pages from 8 page breaks", m)
	}
}

func TestMeasureSkipsOtherFormats(t *testing.T) {
	if m := MeasureFile(".png", []byte{1, 2, 3}); m.Pages != 0 {
		t.Fatalf("got %+v, want zero", m)
	}
}
