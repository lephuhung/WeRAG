package docxwrite

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
)

func TestBuildRoundTrip(t *testing.T) {
	doc := Document{Title: "Thông tư 74/2022/TT-BTC", Paragraphs: []Paragraph{
		{Text: "BỘ TÀI CHÍNH", Bold: true, Center: true},
		{Text: "[Trang 1]", Style: StylePageMarker},
		{Text: "[OCR hỏng ở trang 1 — nhập tay từ bản PDF]", Style: StylePageWarning},
		{Text: "Chương I", Style: StyleHeading2},
		{Text: "QUY ĐỊNH CHUNG", Style: StyleHeading2},
		{Text: "Điều 1. Phạm vi <điều chỉnh> & \"đối tượng\"", Style: StyleHeading4},
		{Text: "1. Khoản\tcó tab\nvà xuống dòng"},
		{Text: "a) điểm có ký tự lạ \x01\x0b"},
		{Text: ""},
	}}
	data, err := Build(doc)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/_rels/document.xml.rels",
		"word/styles.xml", "word/settings.xml"} {
		if !names[want] {
			t.Fatalf("missing part %s in %v", want, names)
		}
	}

	d, err := docxedit.Open(data)
	if err != nil {
		t.Fatalf("docxedit.Open: %v", err)
	}
	paras := d.Paragraphs()
	if len(paras) != len(doc.Paragraphs) {
		t.Fatalf("%d paragraphs, want %d", len(paras), len(doc.Paragraphs))
	}
	want := []string{"BỘ TÀI CHÍNH", "[Trang 1]", "[OCR hỏng ở trang 1 — nhập tay từ bản PDF]", "Chương I", "QUY ĐỊNH CHUNG", "Điều 1. Phạm vi <điều chỉnh> & \"đối tượng\"",
		"1. Khoản\tcó tab\nvà xuống dòng", "a) điểm có ký tự lạ ", ""}
	for i, p := range paras {
		if p.Text != want[i] {
			t.Errorf("paragraph %d = %q, want %q", i, p.Text, want[i])
		}
	}

	layout := docformat.InspectDocx(data)
	if len(layout.Errors) > 0 {
		t.Fatalf("layout errors: %v", layout.Errors)
	}
	styleOf := func(i int) string {
		if p := layout.Paragraphs[i]; p.StyleName != nil {
			return *p.StyleName
		}
		return ""
	}
	if got := styleOf(1); got != "PageMarker" {
		t.Errorf("page marker style = %q", got)
	}
	if got := styleOf(2); got != "PageWarning" {
		t.Errorf("page warning style = %q", got)
	}
	if p := layout.Paragraphs[2]; p.Color == nil || *p.Color != "FF0000" {
		t.Errorf("page warning colour = %v", p.Color)
	}
	if got := styleOf(3); got != "heading 2" {
		t.Errorf("chapter style = %q", got)
	}
	if got := styleOf(5); got != "heading 4" {
		t.Errorf("article style = %q", got)
	}
	if p := layout.Paragraphs[6]; p.FontName == nil || *p.FontName != "Times New Roman" || p.SizePt == nil || *p.SizePt != 14 {
		t.Errorf("body font = %v %v", p.FontName, p.SizePt)
	}
	if p := layout.Paragraphs[5]; p.Bold == nil || !*p.Bold {
		t.Errorf("heading 4 is not bold")
	}
	f, _ := zr.Open("word/styles.xml")
	var styles bytes.Buffer
	_, _ = styles.ReadFrom(f)
	if !strings.Contains(styles.String(), `<w:name w:val="WeRAG gạch chân"/>`) || !strings.Contains(styles.String(), `<w:u w:val="single"/>`) {
		t.Fatalf("mark style missing: %s", styles.String())
	}
	if again, _ := Build(doc); !bytes.Equal(again, data) {
		t.Fatal("Build is not deterministic")
	}
	if _, err := Build(Document{Paragraphs: []Paragraph{{Text: "x", Style: "Title"}}}); err == nil {
		t.Fatal("unknown style accepted")
	}
}

// The legal OCR drafts (paragraphs with Text, Style, Bold, Center) must
// come out as before the writer learnt runs, lists and tables: the golden
// document.xml was written by the earlier writer (main before F8).
func TestBuildLegalDraftMatchesGolden(t *testing.T) {
	doc := Document{Title: "Thông tư 74/2022/TT-BTC", Paragraphs: []Paragraph{
		{Text: "BỘ TÀI CHÍNH", Bold: true, Center: true},
		{Text: "[Trang 1]", Style: StylePageMarker},
		{Text: "[OCR hỏng ở trang 1 — nhập tay từ bản PDF]", Style: StylePageWarning},
		{Text: "Chương I", Style: StyleHeading2},
		{Text: "QUY ĐỊNH CHUNG", Style: StyleHeading2},
		{Text: "Điều 1. Phạm vi <điều chỉnh> & \"đối tượng\"", Style: StyleHeading4},
		{Text: "1. Khoản\tcó tab\nvà xuống dòng"},
		{Text: "a) điểm có ký tự lạ \x01\x0b"},
		{Text: "Điều 2", Style: StyleHeading3},
		{Text: "Chương II", Style: StyleHeading1},
		{Text: ""},
	}}
	data, err := Build(doc)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/legal_draft_document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if got := documentXML(t, data); got != string(want) {
		t.Fatalf("document.xml changed:\n got %s\nwant %s", got, want)
	}
}

// The one change for text-only paragraphs: a paragraph whose text is only
// control characters no longer carries an empty run.
func TestBuildControlCharacterParagraphHasNoEmptyRun(t *testing.T) {
	data, err := Build(Document{Paragraphs: []Paragraph{{Text: "\x01\x02"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := documentXML(t, data); !strings.Contains(got, "<w:p><w:pPr></w:pPr></w:p>") || strings.Contains(got, "<w:r></w:r>") {
		t.Fatalf("document.xml = %s", got)
	}
}

func TestBuildClampsIndentAndListStart(t *testing.T) {
	data, err := Build(Document{Blocks: []Block{
		Paragraph{Text: "sâu", Style: StyleQuote, IndentTwips: 100000},
		Paragraph{Text: "a", List: &ListItem{Ordered: true, List: 1, Start: 99999, Level: 20}},
		Paragraph{Text: "b", List: &ListItem{Ordered: true, List: 2, Start: 0}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	doc := documentXML(t, data)
	if !strings.Contains(doc, `<w:ind w:left="4677"/>`) || !strings.Contains(doc, `<w:ind w:left="4677" w:hanging="360"/>`) ||
		!strings.Contains(doc, `<w:ilvl w:val="8"/>`) {
		t.Errorf("indent not clamped: %s", doc)
	}
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	f, _ := zr.Open("word/numbering.xml")
	var num bytes.Buffer
	_, _ = num.ReadFrom(f)
	if !strings.Contains(num.String(), `<w:startOverride w:val="32767"/>`) || !strings.Contains(num.String(), `<w:startOverride w:val="0"/>`) {
		t.Errorf("list starts: %s", num.String())
	}
}

func documentXML(t *testing.T, data []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	f, err := zr.Open("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	_, _ = b.ReadFrom(f)
	return b.String()
}
