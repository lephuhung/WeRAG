package docxwrite

import (
	"archive/zip"
	"bytes"
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
