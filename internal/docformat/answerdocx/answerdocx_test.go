package answerdocx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
)

func readPart(t *testing.T, data []byte, name string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			b, _ := io.ReadAll(rc)
			return string(b)
		}
	}
	t.Fatalf("part %s missing", name)
	return ""
}

// wellFormed fails when an XML part does not parse.
func wellFormed(t *testing.T, name, part string) {
	t.Helper()
	d := xml.NewDecoder(strings.NewReader(part))
	for {
		if _, err := d.Token(); err == io.EOF {
			return
		} else if err != nil {
			t.Fatalf("%s is not well-formed: %v", name, err)
		}
	}
}

const sampleAnswer = `## Hồ sơ đề nghị cấp phép

Theo **Điều 5 Nghị định 30/2020/NĐ-CP**, hồ sơ gồm *các giấy tờ* sau:

1. Đơn đề nghị (mẫu ` + "`01/ĐN`" + `);
2. Bản sao giấy tờ:
   - Căn cước công dân;
   - Giấy phép cũ.

| Loại | Lệ phí |
|:---|---:|
| Cấp mới | 50.000 đồng |
| Gia hạn | Miễn |

> Trích dẫn: "Văn bản hành chính phải ghi rõ <số, ký hiệu> & ngày tháng."

Xem thêm [Cổng VBPL](https://vbpl.vn/nd30).

---
**Lịch sử sửa đổi**
- Sửa đổi bởi Nghị định 01/2025/NĐ-CP`

func buildSample(t *testing.T) []byte {
	t.Helper()
	data, err := Build(context.Background(), Input{
		Question:   "Hồ sơ cấp phép gồm những gì?",
		AnsweredAt: "14h05 - 16/10/2026",
		Content:    sampleAnswer,
		References: []Reference{
			{Title: "Nghị định về công tác văn thư", DocumentNumber: "30/2020/NĐ-CP", Source: "Văn bản pháp luật", FileName: "nd30.pdf"},
			{Title: "Hướng dẫn nội bộ", Source: "Tài liệu của Tổ chức"},
			{Title: "Cổng thông tin", URL: "https://chinhphu.vn/x"},
			{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBuildWritesAnswerDocument(t *testing.T) {
	data := buildSample(t)
	doc := readPart(t, data, "word/document.xml")
	for _, name := range []string{"word/document.xml", "word/styles.xml", "word/numbering.xml", "[Content_Types].xml",
		"word/_rels/document.xml.rels", "docProps/core.xml"} {
		wellFormed(t, name, readPart(t, data, name))
	}
	for _, want := range []string{
		// header
		`<w:pStyle w:val="Meta"/>`, `>Câu hỏi: </w:t>`, `>Hồ sơ cấp phép gồm những gì?</w:t>`,
		`>14h05 - 16/10/2026 · Trả lời bởi WeRAG</w:t>`, `<w:pStyle w:val="HorizontalRule"/>`,
		// heading + emphasis
		`<w:pStyle w:val="Heading2"/><w:jc w:val="left"/>`, `>Hồ sơ đề nghị cấp phép</w:t>`,
		`<w:r><w:rPr><w:b/><w:bCs/></w:rPr><w:t xml:space="preserve">Điều 5 Nghị định 30/2020/NĐ-CP</w:t></w:r>`,
		`<w:r><w:rPr><w:i/><w:iCs/></w:rPr><w:t xml:space="preserve">các giấy tờ</w:t></w:r>`,
		// inline code
		`<w:rFonts w:ascii="Courier New" w:hAnsi="Courier New" w:cs="Courier New"/>`, `>01/ĐN</w:t>`,
		// lists: ordered level 0, bullets level 1
		`<w:numPr><w:ilvl w:val="0"/><w:numId w:val="2"/></w:numPr>`,
		`<w:numPr><w:ilvl w:val="1"/><w:numId w:val="1"/></w:numPr>`, `>Căn cước công dân;</w:t>`,
		// table with header row, right-aligned column, 13 pt text
		`<w:tbl>`, `<w:tblHeader/>`, `<w:gridCol w:w="4677"/>`, `<w:pStyle w:val="TableText"/><w:jc w:val="right"/>`,
		`>50.000 đồng</w:t>`,
		// quote, escaped text
		`<w:pStyle w:val="WeRAGQuote"/>`, `&lt;số, ký hiệu&gt; &amp; ngày tháng.`,
		// link as text + URL
		`>Xem thêm Cổng VBPL (https://vbpl.vn/nd30).</w:t>`,
		// code-generated block kept
		`>Lịch sử sửa đổi</w:t>`, `>Sửa đổi bởi Nghị định 01/2025/NĐ-CP</w:t>`,
		// references
		`>Tài liệu tham khảo</w:t>`, `>Nghị định về công tác văn thư</w:t>`, `> — Số hiệu: 30/2020/NĐ-CP</w:t>`,
		`> — Tệp: nd30.pdf</w:t>`, `>Hướng dẫn nội bộ</w:t>`, `> — Tài liệu của Tổ chức</w:t>`, `> — https://chinhphu.vn/x</w:t>`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("document.xml lacks %s", want)
		}
	}
	// the empty reference is skipped: three numbered sources
	if n := strings.Count(doc, `<w:numId w:val="3"/>`); n != 3 {
		t.Errorf("%d reference items, want 3", n)
	}
	numbering := readPart(t, data, "word/numbering.xml")
	if !strings.Contains(numbering, `<w:num w:numId="3"><w:abstractNumId w:val="1"/>`) ||
		!strings.Contains(numbering, `<w:numFmt w:val="bullet"/><w:lvlText w:val="•"/>`) {
		t.Errorf("numbering.xml: %s", numbering)
	}
	if core := readPart(t, data, "docProps/core.xml"); !strings.Contains(core, "<dc:title>Hồ sơ cấp phép gồm những gì?</dc:title>") {
		t.Errorf("core title: %s", core)
	}

	// The package opens with the repo's own docx readers, in Times New Roman.
	d, err := docxedit.Open(data)
	if err != nil {
		t.Fatalf("docxedit.Open: %v", err)
	}
	var texts []string
	for _, p := range d.Paragraphs() {
		texts = append(texts, p.Text)
	}
	joined := strings.Join(texts, "\n")
	for _, want := range []string{"Câu hỏi: Hồ sơ cấp phép gồm những gì?", "Đơn đề nghị (mẫu 01/ĐN);", "Lịch sử sửa đổi"} {
		if !strings.Contains(joined, want) {
			t.Errorf("paragraph text lacks %q", want)
		}
	}
	layout := docformat.InspectDocx(data)
	if len(layout.Errors) > 0 {
		t.Fatalf("layout errors: %v", layout.Errors)
	}
	found := false
	for _, p := range layout.Paragraphs {
		if p.FontName != nil && *p.FontName == "Times New Roman" && p.SizePt != nil && *p.SizePt == 14 {
			found = true
			break
		}
	}
	if !found {
		t.Error("no body paragraph in Times New Roman 14")
	}
	if again := buildSample(t); !bytes.Equal(again, data) {
		t.Error("Build is not deterministic")
	}
}

func TestBuildWithoutQuestionOrReferences(t *testing.T) {
	data, err := Build(context.Background(), Input{Content: "Chỉ một câu."})
	if err != nil {
		t.Fatal(err)
	}
	doc := readPart(t, data, "word/document.xml")
	if strings.Contains(doc, "Câu hỏi") || strings.Contains(doc, ReferencesHeading) {
		t.Errorf("unexpected header or sources: %s", doc)
	}
	if !strings.Contains(doc, ">Trả lời bởi WeRAG</w:t>") || !strings.Contains(doc, ">Chỉ một câu.</w:t>") {
		t.Errorf("document.xml: %s", doc)
	}
	// no list → no numbering part
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	for _, f := range zr.File {
		if f.Name == "word/numbering.xml" {
			t.Error("numbering.xml written without lists")
		}
	}
}

func TestBuildVeryLongAnswer(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 400; i++ {
		b.WriteString("## Mục lớn\n\nĐoạn văn **có đậm** và *nghiêng* với `mã` và [liên kết](https://a.vn) — Điều khoản tiếng Việt có dấu.\n\n")
		b.WriteString("- ý một\n  - ý con\n1. bước\n2. bước\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n> trích\n\n")
	}
	b.WriteString(strings.Repeat("*", 5000) + strings.Repeat("_x", 5000))
	start := time.Now()
	data, err := Build(context.Background(), Input{Question: strings.Repeat("rất dài ", 100), Content: b.String()})
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took %v", d)
	}
	doc := readPart(t, data, "word/document.xml")
	wellFormed(t, "document.xml", doc)
	if n := strings.Count(doc, "<w:tbl>"); n != 400 {
		t.Errorf("%d tables, want 400", n)
	}
	// each ordered list restarts: 400 ordered nums
	if n := strings.Count(readPart(t, data, "word/numbering.xml"), "<w:startOverride"); n != 400 {
		t.Errorf("%d ordered lists, want 400", n)
	}
	if layout := docformat.InspectDocx(data); len(layout.Errors) > 0 {
		t.Fatalf("layout errors: %v", layout.Errors)
	}
}

func TestFileNames(t *testing.T) {
	cases := []struct{ question, name, ascii string }{
		{"Hồ sơ cấp phép gồm những gì?", "Trả lời - Hồ sơ cấp phép gồm những gì.docx", "Tra loi - Ho so cap phep gom nhung gi.docx"},
		{"Đường/đi: \"x\" <y>|z*", "Trả lời - Đường đi x y z.docx", "Tra loi - Duong di x y z.docx"},
		{"   ", "Trả lời WeRAG.docx", "Tra loi WeRAG.docx"},
		{strings.Repeat("Nghị định quy định chi tiết ", 10), "Trả lời - Nghị định quy định chi tiết Nghị định quy định chi tiết.docx",
			"Tra loi - Nghi dinh quy dinh chi tiet Nghi dinh quy dinh chi tiet.docx"},
		{"问题 ✓", "Trả lời - 问题 ✓.docx", "Tra loi - .docx"},
	}
	for _, tc := range cases {
		if got := FileName(tc.question); got != tc.name {
			t.Errorf("FileName(%q) = %q, want %q", tc.question, got, tc.name)
		}
		if got := ASCIIFileName(FileName(tc.question)); got != tc.ascii {
			t.Errorf("ASCIIFileName(%q) = %q, want %q", tc.question, got, tc.ascii)
		}
	}
}
