package docxedit

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/docformat"
)

const wNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`

var testAuthor = Author{Name: `Trợ lý "AI" & co`, Date: time.Date(2026, 10, 7, 8, 30, 0, 0, time.UTC)}

func ptrTo[T any](v T) *T { return &v }

// makeDocx zips a minimal package around body markup.
func makeDocx(t *testing.T, body string) []byte {
	t.Helper()
	return makeDocxRaw(t, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
		`<w:document `+wNS+` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`+
		`<w:body>`+body+`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="851" w:bottom="1134" w:left="1701" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr></w:body></w:document>`)
}

func makeDocxRaw(t *testing.T, documentXML string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct{ name, data string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", documentXML},
		{"word/styles.xml", `<?xml version="1.0" encoding="UTF-8"?><w:styles ` + wNS + `><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial"/><w:sz w:val="22"/></w:rPr></w:rPrDefault></w:docDefaults></w:styles>`},
	} {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func documentXML(t *testing.T, docx []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(docx), int64(len(docx)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			b, err := io.ReadAll(rc)
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
	}
	t.Fatal("document.xml missing")
	return ""
}

// wellFormed parses every token with namespace resolution.
func wellFormed(t *testing.T, s string) {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(s))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("document.xml not well-formed: %v\n%s", err, s)
		}
	}
}

func mustOpen(t *testing.T, b []byte) *Document {
	t.Helper()
	d, err := Open(b)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustBytes(t *testing.T, d *Document) []byte {
	t.Helper()
	b, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func squash(s string) string { return strings.Join(strings.Fields(s), "") }

func fixtures(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../testdata/parity/*.docx")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	return files
}

func countTag(s, tag string) int {
	return len(regexp.MustCompile(`<w:`+tag+`[ >/]`).FindAllStringIndex(s, -1))
}

// ---------------------------------------------------------------------------

func TestParityWithInspectDocx(t *testing.T) {
	synthetic := map[string]string{
		"sdt_textbox_nested_table": `<w:sdt><w:sdtContent><w:p><w:r><w:t>In content control</w:t></w:r></w:p></w:sdtContent></w:sdt>` +
			`<w:p><w:pPr><w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr></w:pPr><w:r><w:t>Anchor</w:t></w:r></w:p>` +
			`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>A1</w:t></w:r></w:p><w:tbl><w:tr><w:tc><w:p><w:r><w:t>nested</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:tc>` +
			`<w:tc><w:p><w:r><w:t xml:space="preserve">B1 </w:t></w:r><w:r><w:tab/><w:t>x</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
			`<w:customXml><w:p><w:r><w:t>custom</w:t></w:r></w:p></w:customXml>`,
	}
	type tc struct {
		name string
		data []byte
	}
	var cases []tc
	for _, f := range fixtures(t) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, tc{filepath.Base(f), b})
	}
	for name, body := range synthetic {
		cases = append(cases, tc{name, makeDocx(t, body)})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lay := docformat.InspectDocx(c.data)
			if len(lay.Errors) > 0 {
				t.Fatalf("InspectDocx: %v", lay.Errors)
			}
			d := mustOpen(t, c.data)
			ps := d.Paragraphs()
			if len(ps) != len(lay.Paragraphs) {
				t.Fatalf("paragraphs: got %d, docformat %d", len(ps), len(lay.Paragraphs))
			}
			for i, p := range ps {
				lp := lay.Paragraphs[i]
				if p.Index != i || squash(p.Text) != squash(lp.Text) || p.InTable != lp.InTable {
					t.Errorf("paragraph %d: got %q (table=%v), docformat %q (table=%v)",
						i, p.Text, p.InTable, lp.Text, lp.InTable)
				}
			}
			if len(d.sects) != len(lay.Sections) {
				t.Errorf("sections: got %d, docformat %d", len(d.sects), len(lay.Sections))
			}
		})
	}
}

func firstTextPara(t *testing.T, d *Document) int {
	t.Helper()
	for _, p := range d.Paragraphs() {
		if !p.Empty && !p.InTable {
			return p.Index
		}
	}
	t.Fatal("no body text paragraph")
	return -1
}

func TestReplaceTextFixtures(t *testing.T) {
	for _, f := range fixtures(t)[:8] {
		t.Run(filepath.Base(f), func(t *testing.T) {
			in, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			d := mustOpen(t, in)
			idx := firstTextPara(t, d)
			newText := `Nội dung mới <đã sửa> & "kiểm tra"`
			if err := d.ReplaceText(idx, newText, testAuthor); err != nil {
				t.Fatal(err)
			}
			out := mustBytes(t, d)
			x := documentXML(t, out)
			wellFormed(t, x)
			if n := countTag(x, "del"); n != 1 {
				t.Errorf("w:del count = %d, want 1", n)
			}
			if n := countTag(x, "ins"); n != 1 {
				t.Errorf("w:ins count = %d, want 1", n)
			}
			if !strings.Contains(x, "&lt;đã sửa&gt; &amp; \"kiểm tra\"") {
				t.Error("inserted text not escaped as expected")
			}
			if got := mustOpen(t, out).Paragraphs()[idx].Text; got != newText {
				t.Errorf("visible text = %q, want %q", got, newText)
			}
			lay := docformat.InspectDocx(out)
			if len(lay.Errors) > 0 || len(lay.Paragraphs) != len(d.Paragraphs()) {
				t.Fatalf("InspectDocx after edit: %d paragraphs, errors %v", len(lay.Paragraphs), lay.Errors)
			}
			if lay.Paragraphs[idx].Text != newText {
				t.Errorf("docformat text = %q", lay.Paragraphs[idx].Text)
			}
		})
	}
}

func TestReplaceTextHardCases(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		newText  string
		wantText string
		contains []string // markup that must survive
		dels     int
		re       string
	}{
		{
			name:     "zero runs",
			body:     `<w:p><w:pPr><w:jc w:val="center"/></w:pPr></w:p>`,
			newText:  "Mới",
			wantText: "Mới",
		},
		{
			name:     "self-closing paragraph",
			body:     `<w:p/>`,
			newText:  "Mới\tcó tab",
			wantText: "Mới\tcó tab",
			contains: []string{`<w:tab/>`},
		},
		{
			name: "runs split across formatting with tab and break, bookmarks between",
			body: `<w:p><w:r><w:rPr><w:b/></w:rPr><w:t>Kính</w:t></w:r><w:bookmarkStart w:id="7" w:name="x"/>` +
				`<w:proofErr w:type="spellStart"/><w:r><w:tab/><w:t xml:space="preserve"> gửi</w:t><w:br/></w:r><w:bookmarkEnd w:id="7"/></w:p>`,
			newText:  "Kính gửi:",
			wantText: "Kính gửi:",
			contains: []string{`<w:bookmarkStart w:id="7" w:name="x"/>`, `<w:bookmarkEnd w:id="7"/>`},
			dels:     1,
		},
		{
			name:     "hyperlink runs",
			body:     `<w:p><w:r><w:t xml:space="preserve">Xem </w:t></w:r><w:hyperlink r:id="rId9"><w:r><w:rPr><w:rStyle w:val="Hyperlink"/></w:rPr><w:t>tại đây</w:t></w:r></w:hyperlink></w:p>`,
			newText:  "Xem văn bản",
			wantText: "Xem văn bản",
			contains: []string{`<w:hyperlink r:id="rId9">`},
			dels:     2,
		},
		{
			name: "drawing, field and footnote ref preserved",
			body: `<w:p><w:r><w:t>Ảnh</w:t><w:drawing><wp:inline xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"/></w:drawing></w:r>` +
				`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> PAGE </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>3</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>` +
				`<w:r><w:footnoteReference w:id="1"/></w:r><w:r><w:br w:type="page"/></w:r></w:p>`,
			re:       `</w:del><w:ins [^>]*><w:r><w:t xml:space="preserve">Hình</w:t></w:r></w:ins><w:r><w:drawing>`,
			newText:  "Hình",
			wantText: "Hình\n", // the kept page break stays visible as "\n"
			contains: []string{`<wp:inline`, `<w:instrText xml:space="preserve"> PAGE </w:instrText>`,
				`w:fldCharType="begin"`, `w:fldCharType="end"`, `<w:footnoteReference w:id="1"/>`, `<w:br w:type="page"/>`},
		},
		{
			name: "existing tracked changes",
			body: `<w:p><w:r><w:t xml:space="preserve">Giữ </w:t></w:r><w:del w:id="5" w:author="X" w:date="2026-01-01T00:00:00Z"><w:r><w:delText>bỏ</w:delText></w:r></w:del>` +
				`<w:ins w:id="6" w:author="X" w:date="2026-01-01T00:00:00Z"><w:r><w:t>thêm</w:t></w:r></w:ins></w:p>`,
			newText:  "Khác",
			wantText: "Khác",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := mustOpen(t, makeDocx(t, c.body))
			if err := d.ReplaceText(0, c.newText, testAuthor); err != nil {
				t.Fatal(err)
			}
			out := mustBytes(t, d)
			x := documentXML(t, out)
			wellFormed(t, x)
			if got := mustOpen(t, out).Paragraphs()[0].Text; got != c.wantText {
				t.Errorf("text = %q, want %q\n%s", got, c.wantText, x)
			}
			for _, s := range c.contains {
				if !strings.Contains(x, s) {
					t.Errorf("lost %s\n%s", s, x)
				}
			}
			if c.re != "" && !regexp.MustCompile(c.re).MatchString(x) {
				t.Errorf("markup mismatch:\n%s", x)
			}
			if c.dels > 0 && countTag(x, "del") != c.dels {
				t.Errorf("w:del count = %d, want %d\n%s", countTag(x, "del"), c.dels, x)
			}
			// deleted text never escapes a w:del
			for _, m := range regexp.MustCompile(`<w:delText[^>]*>`).FindAllStringIndex(x, -1) {
				if strings.LastIndex(x[:m[0]], "<w:del ") < strings.LastIndex(x[:m[0]], "</w:del>") {
					t.Errorf("w:delText outside w:del at %d\n%s", m[0], x)
				}
			}
			if lay := docformat.InspectDocx(out); len(lay.Errors) > 0 || len(lay.Paragraphs) != 1 {
				t.Errorf("InspectDocx: %v", lay.Errors)
			}
		})
	}
}

func TestReplaceTextInsideInsertion(t *testing.T) {
	body := `<w:p><w:ins w:id="6" w:author="X" w:date="2026-01-01T00:00:00Z"><w:r><w:t>thêm</w:t></w:r></w:ins><w:r><w:drawing/></w:r></w:p>`
	d := mustOpen(t, makeDocx(t, body))
	if err := d.ReplaceText(0, "mới", testAuthor); err != nil {
		t.Fatal(err)
	}
	x := documentXML(t, mustBytes(t, d))
	wellFormed(t, x)
	// deletion of inserted text nests w:del in w:ins; our w:ins follows it
	if !regexp.MustCompile(`<w:ins w:id="6"[^>]*><w:del [^>]*><w:r><w:delText xml:space="preserve">thêm</w:delText></w:r></w:del></w:ins><w:ins `).MatchString(x) {
		t.Errorf("unexpected markup:\n%s", x)
	}
}

func TestReplaceSubstring(t *testing.T) {
	cases := []struct {
		name, body, old, new, want string
		re                         string // optional markup check
	}{
		{
			name: "inside one run",
			body: `<w:p><w:r><w:rPr><w:b/></w:rPr><w:t>Số: 12/CV-UBND ngày</w:t></w:r></w:p>`,
			old:  "12/CV", new: "15/CV", want: "Số: 15/CV-UBND ngày",
			re: `<w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">Số: </w:t></w:r><w:del [^>]*><w:r><w:rPr><w:b/></w:rPr><w:delText xml:space="preserve">12/CV</w:delText></w:r></w:del><w:ins [^>]*><w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">15/CV</w:t></w:r></w:ins><w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">-UBND ngày</w:t></w:r>`,
		},
		{
			name: "across runs and a tab",
			body: `<w:p><w:r><w:t xml:space="preserve">Hà Nội, ngày </w:t></w:r><w:r><w:rPr><w:i/></w:rPr><w:t>05</w:t><w:tab/></w:r><w:r><w:t xml:space="preserve"> tháng 10</w:t></w:r></w:p>`,
			old:  "ngày 05\t tháng", new: "ngày 06 tháng", want: "Hà Nội, ngày 06 tháng 10",
		},
		{
			name: "whole text, empty replacement",
			body: `<w:p><w:r><w:t>xoá</w:t></w:r></w:p>`,
			old:  "xoá", new: "", want: "",
		},
		{
			name: "inside an existing insertion",
			body: `<w:p><w:ins w:id="3" w:author="X" w:date="2026-01-01T00:00:00Z"><w:r><w:t>abc</w:t></w:r></w:ins></w:p>`,
			old:  "b", new: "X", want: "aXc",
		},
		{
			name: "decomposed needle",
			body: `<w:p><w:r><w:t>Quyết định</w:t></w:r></w:p>`,
			old:  "Quye\u0302\u0301t", new: "QUYẾT", want: "QUYẾT định",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := mustOpen(t, makeDocx(t, c.body))
			if err := d.ReplaceSubstring(0, c.old, c.new, testAuthor); err != nil {
				t.Fatal(err)
			}
			out := mustBytes(t, d)
			x := documentXML(t, out)
			wellFormed(t, x)
			if got := mustOpen(t, out).Paragraphs()[0].Text; got != c.want {
				t.Errorf("text = %q, want %q\n%s", got, c.want, x)
			}
			if c.re != "" && !regexp.MustCompile(c.re).MatchString(x) {
				t.Errorf("markup mismatch:\n%s", x)
			}
			if countTag(x, "del") < 1 {
				t.Error("no w:del")
			}
		})
	}
	d := mustOpen(t, makeDocx(t, `<w:p><w:r><w:t>abc</w:t></w:r></w:p>`))
	if err := d.ReplaceSubstring(0, "zzz", "y", testAuthor); err == nil {
		t.Error("missing substring: want error")
	}
}

func TestSetParaProps(t *testing.T) {
	body := `<w:p><w:pPr><w:pStyle w:val="Body"/><w:jc w:val="left"/><w:rPr><w:b/></w:rPr></w:pPr><w:r><w:t>Đoạn văn</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>Không có pPr</w:t></w:r></w:p>`
	d := mustOpen(t, makeDocx(t, body))
	err := d.SetParaProps(0, ParaProps{
		Alignment: ptrTo("both"), FirstLinePt: ptrTo(36.0), IndentLeftPt: ptrTo(0.0),
		SpaceBeforePt: ptrTo(6.0), SpaceAfterPt: ptrTo(6.0),
		LineSpacing: &LineSpacing{Multiple: ptrTo(1.15)},
	}, testAuthor)
	if err != nil {
		t.Fatal(err)
	}
	x := documentXML(t, mustBytes(t, d))
	wellFormed(t, x)
	want := `<w:pPr><w:pStyle w:val="Body"/><w:spacing w:before="120" w:after="120" w:line="276" w:lineRule="auto"/>` +
		`<w:ind w:left="0" w:firstLine="720"/><w:jc w:val="both"/><w:rPr><w:b/></w:rPr>` +
		`<w:pPrChange w:id="0" w:author="Trợ lý &quot;AI&quot; &amp; co" w:date="2026-10-07T08:30:00Z"><w:pPr><w:pStyle w:val="Body"/><w:jc w:val="left"/></w:pPr></w:pPrChange></w:pPr>`
	if !strings.Contains(x, want) {
		t.Fatalf("pPr mismatch:\n got %s\nwant %s", x, want)
	}
	// second change keeps the original previous properties, no nesting
	if err := d.SetParaProps(0, ParaProps{Alignment: ptrTo("center")}, testAuthor); err != nil {
		t.Fatal(err)
	}
	if err := d.SetParaProps(1, ParaProps{Alignment: ptrTo("center")}, testAuthor); err != nil {
		t.Fatal(err)
	}
	out := mustBytes(t, d)
	x = documentXML(t, out)
	wellFormed(t, x)
	if n := strings.Count(x, "<w:pPrChange "); n != 2 {
		t.Errorf("pPrChange count = %d, want 2\n%s", n, x)
	}
	if !strings.Contains(x, `<w:jc w:val="center"/><w:rPr><w:b/></w:rPr><w:pPrChange w:id="0" w:author="Trợ lý &quot;AI&quot; &amp; co" w:date="2026-10-07T08:30:00Z"><w:pPr><w:pStyle w:val="Body"/><w:jc w:val="left"/></w:pPr></w:pPrChange>`) {
		t.Errorf("second change lost the original props:\n%s", x)
	}
	if !strings.Contains(x, `<w:p><w:pPr><w:jc w:val="center"/><w:pPrChange w:id="1" `) || !strings.Contains(x, `<w:pPr></w:pPr></w:pPrChange>`) {
		t.Errorf("created pPr wrong:\n%s", x)
	}
	lay := docformat.InspectDocx(out)
	p := lay.Paragraphs[0]
	if p.Alignment != "center" || p.IndentFirstLineMM == nil || math.Abs(*p.IndentFirstLineMM-12.7) > 0.01 ||
		p.LineSpacingMult == nil || *p.LineSpacingMult != 1.15 || p.SpaceBeforePt == nil || *p.SpaceBeforePt != 6 {
		t.Errorf("docformat sees %+v", p)
	}
	// no-op change records nothing
	before := string(d.xml)
	if err := d.SetParaProps(1, ParaProps{Alignment: ptrTo("center")}, testAuthor); err != nil || string(d.xml) != before {
		t.Errorf("no-op edit changed the document (err %v)", err)
	}
	if err := d.SetParaProps(0, ParaProps{FirstLinePt: ptrTo(1.0), HangingPt: ptrTo(1.0)}, testAuthor); err == nil {
		t.Error("firstLine+hanging: want error")
	}
}

func TestSetRunProps(t *testing.T) {
	in, err := os.ReadFile("../testdata/parity/fx_good_cong_van_arial.docx")
	if err != nil {
		t.Fatal(err)
	}
	d := mustOpen(t, in)
	idx := firstTextPara(t, d)
	if err := d.SetRunProps(idx, RunProps{Font: ptrTo("Times New Roman"), SizePt: ptrTo(13.5),
		Bold: ptrTo(false), Italic: ptrTo(true), Underline: ptrTo(true)}, testAuthor); err != nil {
		t.Fatal(err)
	}
	out := mustBytes(t, d)
	x := documentXML(t, out)
	wellFormed(t, x)
	if !regexp.MustCompile(`<w:rPr><w:rFonts [^>]*w:ascii="Times New Roman"[^>]*/><w:b w:val="0"/><w:bCs w:val="0"/><w:i/><w:iCs/><w:sz w:val="27"/><w:szCs w:val="27"/><w:u w:val="single"/><w:rPrChange w:id="\d+" w:author="[^"]+" w:date="2026-10-07T08:30:00Z"><w:rPr>.*?</w:rPr></w:rPrChange></w:rPr>`).MatchString(x) {
		t.Errorf("rPr not as expected:\n%s", x)
	}
	lay := docformat.InspectDocx(out)
	p := lay.Paragraphs[idx]
	if p.FontName == nil || *p.FontName != "Times New Roman" || p.SizePt == nil || *p.SizePt != 13.5 ||
		p.Italic == nil || !*p.Italic || p.Bold == nil || *p.Bold || p.Underline == nil || !*p.Underline {
		t.Errorf("docformat sees font=%v size=%v bold=%v italic=%v", deref(p.FontName), deref(p.SizePt), deref(p.Bold), deref(p.Italic))
	}
	// a second change keeps one rPrChange per run, with the original props
	orig := regexp.MustCompile(`<w:rPrChange [^>]*>.*?</w:rPrChange>`).FindString(x)
	if err := d.SetRunProps(idx, RunProps{SizePt: ptrTo(14.0)}, testAuthor); err != nil {
		t.Fatal(err)
	}
	x2 := documentXML(t, mustBytes(t, d))
	if strings.Count(x2, "<w:rPrChange ") != strings.Count(x, "<w:rPrChange ") || !strings.Contains(x2, orig) {
		t.Errorf("rPrChange not preserved:\n%s", x2)
	}
	// a run without rPr gets one
	d = mustOpen(t, makeDocx(t, `<w:p><w:r><w:t>trần</w:t></w:r><w:r><w:drawing/></w:r></w:p>`))
	if err := d.SetRunProps(0, RunProps{Caps: ptrTo(true)}, testAuthor); err != nil {
		t.Fatal(err)
	}
	x = documentXML(t, mustBytes(t, d))
	if !strings.Contains(x, `<w:r><w:rPr><w:caps/><w:rPrChange w:id="0" w:author="Trợ lý &quot;AI&quot; &amp; co" w:date="2026-10-07T08:30:00Z"><w:rPr></w:rPr></w:rPrChange></w:rPr><w:t>trần</w:t></w:r><w:r><w:drawing/></w:r>`) {
		t.Errorf("unexpected:\n%s", x)
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestSetSectionProps(t *testing.T) {
	for _, f := range fixtures(t)[:5] {
		t.Run(filepath.Base(f), func(t *testing.T) {
			in, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			d := mustOpen(t, in)
			if len(d.sects) == 0 {
				t.Skip("no sectPr")
			}
			last := len(d.sects) - 1
			mm := func(v float64) *float64 { return ptrTo(v * 72 / 25.4) }
			if err := d.SetSectionProps(last, SectionProps{MarginTopPt: mm(20), MarginBottomPt: mm(20),
				MarginLeftPt: mm(30), MarginRightPt: mm(15), PageWidthPt: mm(210), PageHeightPt: mm(297)}, testAuthor); err != nil {
				t.Fatal(err)
			}
			out := mustBytes(t, d)
			x := documentXML(t, out)
			wellFormed(t, x)
			if strings.Count(x, "<w:sectPrChange ") != 1 {
				t.Errorf("want one sectPrChange\n%s", x)
			}
			sec := docformat.InspectDocx(out).Sections[last]
			for name, got := range map[string]*float64{"top": sec.MarginTopMM, "bottom": sec.MarginBottomMM,
				"left": sec.MarginLeftMM, "right": sec.MarginRightMM, "width": sec.PageWidthMM} {
				want := map[string]float64{"top": 20, "bottom": 20, "left": 30, "right": 15, "width": 210}[name]
				if got == nil || math.Abs(*got-want) > 0.05 {
					t.Errorf("%s = %v, want %v", name, deref(got), want)
				}
			}
		})
	}
	// orientation swaps the page, header refs stay out of the change record
	d := mustOpen(t, makeDocxRaw(t, `<?xml version="1.0"?><w:document `+wNS+` xmlns:r="r"><w:body><w:p/><w:sectPr><w:headerReference w:type="default" r:id="rId1"/><w:pgSz w:w="11906" w:h="16838"/></w:sectPr></w:body></w:document>`))
	if err := d.SetSectionProps(0, SectionProps{Orientation: ptrTo("landscape"), GutterPt: ptrTo(0.0)}, testAuthor); err != nil {
		t.Fatal(err)
	}
	x := documentXML(t, mustBytes(t, d))
	want := `<w:sectPr><w:headerReference w:type="default" r:id="rId1"/><w:pgSz w:w="16838" w:h="11906" w:orient="landscape"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/><w:sectPrChange w:id="0" w:author="Trợ lý &quot;AI&quot; &amp; co" w:date="2026-10-07T08:30:00Z"><w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr></w:sectPrChange></w:sectPr>`
	if !strings.Contains(x, want) {
		t.Errorf("got\n%s\nwant\n%s", x, want)
	}
}

func TestRevisionIDsUnique(t *testing.T) {
	body := `<w:p><w:bookmarkStart w:id="41" w:name="a"/><w:r><w:t>Một hai ba</w:t></w:r><w:bookmarkEnd w:id="41"/></w:p>` +
		`<w:p><w:r><w:t>Bốn năm</w:t></w:r></w:p>`
	d := mustOpen(t, makeDocx(t, body))
	a := Author{Name: "AI"} // zero date → now
	steps := []func() error{
		func() error { return d.ReplaceSubstring(0, "hai", "2", a) },
		func() error { return d.SetRunProps(0, RunProps{SizePt: ptrTo(14.0)}, a) },
		func() error { return d.SetParaProps(0, ParaProps{Alignment: ptrTo("center")}, a) },
		func() error { return d.ReplaceText(1, "Sáu", a) },
		func() error { return d.SetSectionProps(0, SectionProps{MarginLeftPt: ptrTo(85.0)}, a) },
	}
	for i, s := range steps {
		if err := s(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	// a run that already carries w:rPrChange, then split by a substring edit
	if err := d.SetRunProps(1, RunProps{SizePt: ptrTo(13.0)}, a); err != nil {
		t.Fatal(err)
	}
	if err := d.ReplaceSubstring(1, "u", "X", a); err != nil {
		t.Fatal(err)
	}
	x := documentXML(t, mustBytes(t, d))
	wellFormed(t, x)
	assertIDsUnique(t, x)
	seen := map[string]bool{}
	re := regexp.MustCompile(`<w:(ins|del|rPrChange|pPrChange|sectPrChange) ([^>]*)>`)
	ms := re.FindAllStringSubmatch(x, -1)
	if len(ms) < 6 {
		t.Fatalf("only %d revisions\n%s", len(ms), x)
	}
	dateRe := regexp.MustCompile(`w:date="\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ"`)
	for _, m := range ms {
		id := regexp.MustCompile(`w:id="(\d+)"`).FindStringSubmatch(m[2])
		if id == nil || !strings.Contains(m[2], `w:author="AI"`) || !dateRe.MatchString(m[2]) {
			t.Errorf("bad revision attrs %q", m[0])
			continue
		}
		if seen[id[1]] || id[1] == "41" {
			t.Errorf("duplicate id %s", id[1])
		}
		seen[id[1]] = true
	}
	if got := mustOpen(t, mustBytes(t, d)).Paragraphs(); got[0].Text != "Một 2 ba" || got[1].Text != "SáX" {
		t.Errorf("texts %q / %q", got[0].Text, got[1].Text)
	}
}

func TestOtherEntriesUntouched(t *testing.T) {
	for _, f := range fixtures(t)[:6] {
		in, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		d := mustOpen(t, in)
		if err := d.ReplaceText(firstTextPara(t, d), "x", testAuthor); err != nil {
			t.Fatal(err)
		}
		out := mustBytes(t, d)
		zin, _ := zip.NewReader(bytes.NewReader(in), int64(len(in)))
		zout, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
		if err != nil {
			t.Fatal(err)
		}
		if len(zin.File) != len(zout.File) {
			t.Fatalf("%s: entry count %d → %d", f, len(zin.File), len(zout.File))
		}
		for i, a := range zin.File {
			b := zout.File[i]
			if a.Name != b.Name {
				t.Fatalf("%s: entry %d %s → %s", f, i, a.Name, b.Name)
			}
			if a.Name == "word/document.xml" {
				continue
			}
			ra, _ := a.OpenRaw()
			rb, _ := b.OpenRaw()
			ba, _ := io.ReadAll(ra)
			bb, _ := io.ReadAll(rb)
			if a.CRC32 != b.CRC32 || a.CompressedSize64 != b.CompressedSize64 || !bytes.Equal(ba, bb) || a.Method != b.Method {
				t.Errorf("%s: %s changed", f, a.Name)
			}
		}
	}
}

func TestFindParagraph(t *testing.T) {
	body := `<w:p><w:r><w:t>CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t xml:space="preserve">Độc lập  -  Tự do - </w:t></w:r><w:r><w:t>Hạnh phúc</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>Kính gửi: Sở Y tế</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>Kính gửi:</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>Nơi nhận: như trên; Sở Y tế</w:t></w:r></w:p>`
	d := mustOpen(t, makeDocx(t, body))
	cases := []struct {
		needle string
		idx    int
		amb    bool
	}{
		{"cộng hòa xã hội chủ nghĩa việt nam", 0, false},
		{"độc lập - tự do - hạnh phúc", 1, false},
		{"Đo\u0323\u0302c la\u0323\u0302p", 1, false}, // decomposed, any mark order
		{"Độc\u00a0lập\n-\tTự do", 1, false},
		{"Doc lap", -1, false}, // diacritics are significant
		{"kính gửi:", 3, false},
		{"Sở Y tế", 2, true},
		{"", -1, false},
		{"không có", -1, false},
	}
	for _, c := range cases {
		idx, amb := d.FindParagraph(c.needle)
		if idx != c.idx || amb != c.amb {
			t.Errorf("FindParagraph(%q) = %d,%v want %d,%v", c.needle, idx, amb, c.idx, c.amb)
		}
	}
}

func TestCustomPrefix(t *testing.T) {
	doc := `<?xml version="1.0"?><x:document xmlns:x="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><!-- keep me --><x:body>` +
		`<x:p><x:r><x:t>Văn bản</x:t></x:r></x:p><x:sectPr/></x:body></x:document>`
	d := mustOpen(t, makeDocxRaw(t, doc))
	if err := d.ReplaceText(0, "Mới", testAuthor); err != nil {
		t.Fatal(err)
	}
	if err := d.SetParaProps(0, ParaProps{Alignment: ptrTo("center")}, testAuthor); err != nil {
		t.Fatal(err)
	}
	out := mustBytes(t, d)
	x := documentXML(t, out)
	wellFormed(t, x)
	if strings.Contains(x, "<w:") || !strings.Contains(x, "<x:ins x:id=") || !strings.Contains(x, "<!-- keep me -->") {
		t.Errorf("prefix not respected:\n%s", x)
	}
	if lay := docformat.InspectDocx(out); lay.Paragraphs[0].Text != "Mới" || lay.Paragraphs[0].Alignment != "center" {
		t.Errorf("docformat sees %q %s", lay.Paragraphs[0].Text, lay.Paragraphs[0].Alignment)
	}
}

func TestNfcLatin(t *testing.T) {
	for in, want := range map[string]string{
		"a\u0323\u0302":    "ậ",
		"a\u0302\u0323":    "ậ",
		"u\u031b\u0300":    "ừ",
		"O\u031b\u0303":    "Ỡ",
		"Vie\u0323\u0302t": "Việt",
		"đ":                "đ",
		"x\u0301":          "x\u0301",
	} {
		if got := nfcLatin(in); got != want {
			t.Errorf("nfcLatin(%+q) = %+q, want %+q", in, got, want)
		}
	}
}

// TestPythonDocx opens the output with python-docx when it is installed.
func TestPythonDocx(t *testing.T) {
	if exec.Command("python3", "-c", "import docx").Run() != nil {
		t.Skip("python-docx not installed")
	}
	in, err := os.ReadFile("../testdata/parity/fx_good_cong_van.docx")
	if err != nil {
		t.Fatal(err)
	}
	d := mustOpen(t, in)
	idx := firstTextPara(t, d)
	if err := d.ReplaceText(idx, "Mới", testAuthor); err != nil {
		t.Fatal(err)
	}
	if err := d.SetRunProps(idx, RunProps{SizePt: ptrTo(14.0)}, testAuthor); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "out.docx")
	if err := os.WriteFile(path, mustBytes(t, d), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("python3", "-c", "import sys, docx; docx.Document(sys.argv[1])", path).CombinedOutput(); err != nil {
		t.Fatalf("python-docx: %v\n%s", err, out)
	}
}

// assertIDsUnique checks every w:id in document.xml is unique, except the
// end markers that legitimately repeat their start's id.
func assertIDsUnique(t *testing.T, x string) {
	t.Helper()
	seen := map[string]string{}
	for _, m := range regexp.MustCompile(`<w:(\w+)\b[^>]*?\sw:id="(\d+)"`).FindAllStringSubmatch(x, -1) {
		switch m[1] {
		case "bookmarkEnd", "commentRangeEnd", "commentReference", "footnoteReference", "endnoteReference":
			continue
		}
		if prev, ok := seen[m[2]]; ok {
			t.Errorf("w:id %s used by w:%s and w:%s", m[2], prev, m[1])
		}
		seen[m[2]] = m[1]
	}
}

func TestSplitRunKeepsRPrChangeIDUnique(t *testing.T) {
	d := mustOpen(t, makeDocx(t, `<w:p><w:r><w:t>Số 12/UBND-VP ngày</w:t></w:r></w:p>`))
	if err := d.SetRunProps(0, RunProps{SizePt: ptrTo(13.0)}, testAuthor); err != nil {
		t.Fatal(err)
	}
	if err := d.ReplaceSubstring(0, "UBND", "X", testAuthor); err != nil {
		t.Fatal(err)
	}
	x := documentXML(t, mustBytes(t, d))
	wellFormed(t, x)
	assertIDsUnique(t, x)
	if n := strings.Count(x, "<w:rPrChange "); n != 1 {
		t.Errorf("rPrChange count = %d, want 1 (first piece only)\n%s", n, x)
	}
	if got := d.Paragraphs()[0].Text; got != "Số 12/X-VP ngày" {
		t.Errorf("text %q", got)
	}
}

func TestBatchMatchesSequential(t *testing.T) {
	var body strings.Builder
	for i := 0; i < 50; i++ {
		if i%2 == 0 {
			body.WriteString(`<w:p><w:pPr><w:jc w:val="left"/></w:pPr><w:r><w:rPr><w:sz w:val="24"/></w:rPr><w:t xml:space="preserve">Đoạn số ` + strings.Repeat("x", i%7) + ` hết</w:t></w:r><w:r><w:t>.</w:t></w:r></w:p>`)
		} else {
			body.WriteString(`<w:p><w:r><w:t xml:space="preserve">Dòng lẻ UBND </w:t></w:r></w:p>`)
		}
	}
	src := makeDocx(t, body.String())
	a := testAuthor
	type op struct {
		seq   func(d *Document) error
		batch func(b *Batch) error
	}
	var ops []op
	for i := 0; i < 50; i++ {
		i := i
		switch i % 4 {
		case 0: // pPr + run props on one paragraph touch disjoint markup
			ops = append(ops, op{
				func(d *Document) error { return d.SetParaProps(i, ParaProps{Alignment: ptrTo("both")}, a) },
				func(b *Batch) error { return b.SetParaProps(i, ParaProps{Alignment: ptrTo("both")}, a) },
			}, op{
				func(d *Document) error { return d.SetRunProps(i, RunProps{SizePt: ptrTo(14.0)}, a) },
				func(b *Batch) error { return b.SetRunProps(i, RunProps{SizePt: ptrTo(14.0)}, a) },
			})
		case 1: // paragraph without pPr: pPr insertion + text replacement
			ops = append(ops, op{
				func(d *Document) error { return d.ReplaceSubstring(i, "UBND", "Ủy ban", a) },
				func(b *Batch) error { return b.ReplaceSubstring(i, "UBND", "Ủy ban", a) },
			}, op{
				func(d *Document) error { return d.SetParaProps(i, ParaProps{FirstLinePt: ptrTo(36.0)}, a) },
				func(b *Batch) error { return b.SetParaProps(i, ParaProps{FirstLinePt: ptrTo(36.0)}, a) },
			})
		case 2:
			ops = append(ops, op{
				func(d *Document) error { return d.ReplaceText(i, "Mới", a) },
				func(b *Batch) error { return b.ReplaceText(i, "Mới", a) },
			})
		case 3:
			ops = append(ops, op{
				func(d *Document) error { return d.SetRunProps(i, RunProps{Font: ptrTo("Times New Roman")}, a) },
				func(b *Batch) error { return b.SetRunProps(i, RunProps{Font: ptrTo("Times New Roman")}, a) },
			})
		}
	}
	ops = append(ops, op{
		func(d *Document) error { return d.SetSectionProps(0, SectionProps{MarginLeftPt: ptrTo(85.0)}, a) },
		func(b *Batch) error { return b.SetSectionProps(0, SectionProps{MarginLeftPt: ptrTo(85.0)}, a) },
	})

	seq := mustOpen(t, src)
	for i, o := range ops {
		if err := o.seq(seq); err != nil {
			t.Fatalf("sequential op %d: %v", i, err)
		}
	}
	bat := mustOpen(t, src)
	if err := bat.Batch(func(b *Batch) error {
		for i, o := range ops {
			if err := o.batch(b); err != nil {
				return fmt.Errorf("batch op %d: %w", i, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	xs, xb := documentXML(t, mustBytes(t, seq)), documentXML(t, mustBytes(t, bat))
	if xs != xb {
		t.Fatalf("batch differs from sequential\nseq:   %s\nbatch: %s", xs, xb)
	}
	wellFormed(t, xb)
	assertIDsUnique(t, xb)
	out := mustBytes(t, bat)
	if lay := docformat.InspectDocx(out); len(lay.Paragraphs) != 50 || lay.Paragraphs[0].Alignment != "justify" {
		t.Errorf("InspectDocx after batch: %d paragraphs", len(lay.Paragraphs))
	}
}

func TestBatchConflict(t *testing.T) {
	src := makeDocx(t, `<w:p><w:r><w:t>Một</w:t></w:r></w:p><w:p><w:r><w:t>Hai</w:t></w:r></w:p>`)
	d := mustOpen(t, src)
	before := string(d.xml)
	err := d.Batch(func(b *Batch) error {
		if err := b.SetRunProps(1, RunProps{Bold: ptrTo(true)}, testAuthor); err != nil {
			return err
		}
		if err := b.ReplaceText(0, "Ba", testAuthor); err != nil {
			return err
		}
		return b.SetRunProps(0, RunProps{Bold: ptrTo(true)}, testAuthor)
	})
	if err == nil || !strings.Contains(err.Error(), "SetRunProps on paragraph 0 overlaps ReplaceText on paragraph 0") {
		t.Fatalf("want conflict error, got %v", err)
	}
	if string(d.xml) != before {
		t.Error("failed batch modified the document")
	}
	// a conflicting call can be skipped and the rest still applied
	if err := d.Batch(func(b *Batch) error {
		_ = b.ReplaceText(0, "Ba", testAuthor)
		if b.ReplaceText(0, "Bốn", testAuthor) == nil {
			t.Error("second ReplaceText on paragraph 0: want error")
		}
		return b.ReplaceText(1, "Năm", testAuthor)
	}); err != nil {
		t.Fatal(err)
	}
	if ps := d.Paragraphs(); ps[0].Text != "Ba" || ps[1].Text != "Năm" {
		t.Errorf("texts %q %q", ps[0].Text, ps[1].Text)
	}
}

func TestLenientEntities(t *testing.T) {
	src := makeDocx(t, `<w:p><w:r><w:t>A&nbsp;B</w:t></w:r></w:p><w:p><w:r><w:t>Khác</w:t></w:r></w:p>`)
	lay := docformat.InspectDocx(src)
	if len(lay.Errors) > 0 {
		t.Fatalf("docformat rejects it: %v", lay.Errors)
	}
	d := mustOpen(t, src)
	if got, want := d.Paragraphs()[0].Text, lay.Paragraphs[0].Text; got != want {
		t.Errorf("text %q, docformat %q", got, want)
	}
	if err := d.SetParaProps(1, ParaProps{Alignment: ptrTo("center")}, testAuthor); err != nil {
		t.Fatal(err)
	}
	if err := d.ReplaceText(1, "Mới", testAuthor); err != nil {
		t.Fatal(err)
	}
	x := documentXML(t, mustBytes(t, d))
	if !strings.Contains(x, `<w:t>A&nbsp;B</w:t>`) {
		t.Errorf("untouched paragraph rewritten:\n%s", x)
	}
	if ps := mustOpen(t, mustBytes(t, d)).Paragraphs(); ps[1].Text != "Mới" {
		t.Errorf("text %q", ps[1].Text)
	}
	// editing the paragraph itself turns the literal into escaped text
	if err := d.ReplaceSubstring(0, "B", "C", testAuthor); err != nil {
		t.Fatal(err)
	}
	if got := d.Paragraphs()[0].Text; got != "A&nbsp;C" {
		t.Errorf("text %q", got)
	}
}

func TestNoOpEditsLeaveTheDocumentClean(t *testing.T) {
	d := mustOpen(t, makeDocx(t, `<w:p><w:r><w:t>Văn bản</w:t></w:r></w:p>`))
	// right margin is 851 twips already: 851/20 pt asks for the same value
	same := SectionProps{MarginRightPt: ptrTo(851.0 / 20)}
	if err := d.Batch(func(b *Batch) error {
		if err := b.SetSectionProps(0, same, testAuthor); err != nil {
			return err
		}
		if b.Pending() != 0 {
			t.Fatalf("a no-op edit was recorded: %d", b.Pending())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if d.Dirty() {
		t.Fatal("a batch of no-op edits must leave the document clean")
	}
	if err := d.Batch(func(b *Batch) error {
		if err := b.SetSectionProps(0, SectionProps{MarginRightPt: ptrTo(852.0 / 20)}, testAuthor); err != nil {
			return err
		}
		if b.Pending() != 1 {
			t.Fatalf("pending = %d", b.Pending())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !d.Dirty() {
		t.Fatal("a real edit must mark the document dirty")
	}
}
