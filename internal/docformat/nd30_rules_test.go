package docformat

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// Rules added from the verified NĐ30 Phụ lục I text: modes first/rest,
// regex_prop, skip_when, run colour, page numbering, ô 11/12/13.

const testStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman"/><w:sz w:val="28"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr/></w:pPrDefault></w:docDefaults>
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
<w:style w:type="character" w:styleId="Red"><w:name w:val="Red"/><w:rPr><w:color w:val="FF0000"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Header"><w:name w:val="header"/><w:pPr><w:jc w:val="center"/></w:pPr></w:style>
</w:styles>`

// tp is one paragraph of a test document: runs separated by "|", a run
// starting with "*" is bold, "_" italic, "#" red.
func tp(text, align string, size float64) string {
	var b strings.Builder
	b.WriteString("<w:p>")
	if align != "" {
		fmt.Fprintf(&b, `<w:pPr><w:jc w:val="%s"/></w:pPr>`, align)
	}
	for _, r := range strings.Split(text, "|") {
		rpr := ""
		for len(r) > 0 && strings.ContainsRune("*_#", rune(r[0])) {
			switch r[0] {
			case '*':
				rpr += "<w:b/>"
			case '_':
				rpr += "<w:i/>"
			case '#':
				rpr += `<w:color w:val="FF0000"/>`
			}
			r = r[1:]
		}
		if size > 0 {
			rpr += fmt.Sprintf(`<w:sz w:val="%d"/>`, int(size*2))
		}
		fmt.Fprintf(&b, `<w:r><w:rPr>%s</w:rPr><w:t xml:space="preserve">%s</w:t></w:r>`, rpr, r)
	}
	b.WriteString("</w:p>")
	return b.String()
}

func ttbl(rows ...[2]string) string {
	var b strings.Builder
	b.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="9070" w:type="dxa"/></w:tblPr><w:tblGrid><w:gridCol w:w="4535"/><w:gridCol w:w="4535"/></w:tblGrid>`)
	for _, r := range rows {
		fmt.Fprintf(&b, "<w:tr><w:tc><w:tcPr/>%s</w:tc><w:tc><w:tcPr/>%s</w:tc></w:tr>", r[0], r[1])
	}
	b.WriteString("</w:tbl>")
	return b.String()
}

// testDocx packs a body (and optional header/footer XML bodies, and
// whether page 1 has its own header) into .docx bytes.
func testDocx(t *testing.T, body, header, footer string, titlePg bool) []byte {
	t.Helper()
	const w = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	refs, rels := "", ""
	if header != "" {
		refs += `<w:headerReference w:type="default" r:id="rIdH"/>`
		rels += `<Relationship Id="rIdH" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>`
	}
	if footer != "" {
		refs += `<w:footerReference w:type="default" r:id="rIdF"/>`
		rels += `<Relationship Id="rIdF" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="/word/footer1.xml"/>`
	}
	if titlePg {
		refs += "<w:titlePg/>"
	}
	sect := `<w:sectPr>` + refs + `<w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1247" w:right="1077" w:bottom="1247" w:left="1814" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr>`
	parts := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"word/document.xml":   `<?xml version="1.0" encoding="UTF-8"?><w:document ` + w + `><w:body>` + body + sect + `</w:body></w:document>`,
		"word/styles.xml":     testStyles,
		"word/_rels/document.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			rels + `</Relationships>`,
	}
	if header != "" {
		parts["word/header1.xml"] = `<?xml version="1.0"?><w:hdr ` + w + `>` + header + `</w:hdr>`
	}
	if footer != "" {
		parts["word/footer1.xml"] = `<?xml version="1.0"?><w:ftr ` + w + `>` + footer + `</w:ftr>`
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range parts {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(data))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func checkByID(t *testing.T, checks []CheckResult, id string) CheckResult {
	t.Helper()
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no check %s", id)
	return CheckResult{}
}

// noiNhanLayout: one component of lines, given as (size, bold, italic).
func compLayout(key string, lines ...Para) (*Layout, *Segmentation) {
	l := &Layout{}
	comp := &Component{Found: true}
	for i := range lines {
		p := lines[i]
		p.Index, p.Zone = i, ZoneLeft
		l.Paragraphs = append(l.Paragraphs, &p)
		comp.Paras = append(comp.Paras, i)
		comp.Zones = append(comp.Zones, ZoneLeft)
	}
	return l, &Segmentation{Components: map[string]*Component{key: comp}}
}

func line(text string, size float64, bold, italic bool) Para {
	return Para{Text: text, SizePt: ptr(size), Bold: ptr(bold), Italic: ptr(italic)}
}

func TestModesFirstRest(t *testing.T) {
	rs := mustRuleSet(t, "cong_van")
	run := func(lines ...Para) map[string]string {
		l, seg := compLayout("noi_nhan", lines...)
		out := map[string]string{}
		for _, c := range Evaluate(l, seg, rs) {
			if strings.HasPrefix(c.ID, "noi_nhan.") {
				out[c.ID] = c.Status
			}
		}
		return out
	}
	ok := run(line("Nơi nhận:", 12, true, true), line("- Như trên;", 11, false, false),
		line("- Giám đốc (để b/c);", 11, false, false), line("- Lưu: VT, VP.", 11, false, false))
	for _, id := range []string{"noi_nhan.label.size", "noi_nhan.label.italic", "noi_nhan.label.bold", "noi_nhan.list.size", "noi_nhan.list.italic"} {
		if ok[id] != StatusPass {
			t.Errorf("%s = %s, want pass", id, ok[id])
		}
	}
	// the label is only checked on line 1, the list only on the others;
	// one list line of four in 12pt still passes (≥70%)
	bad := run(line("Nơi nhận:", 11, false, true), line("- Như trên;", 12, false, false),
		line("- A;", 11, false, false), line("- B;", 11, false, false), line("- Lưu: VT.", 11, false, false))
	if bad["noi_nhan.label.size"] != StatusWarn || bad["noi_nhan.label.bold"] != StatusWarn || bad["noi_nhan.label.italic"] != StatusPass {
		t.Errorf("label checks: %v", bad)
	}
	if bad["noi_nhan.list.size"] != StatusPass {
		t.Errorf("rest is a majority: %v", bad)
	}
	allBig := run(line("Nơi nhận:", 12, true, true), line("- Như trên;", 13, false, true), line("- Lưu: VT.", 13, false, true))
	if allBig["noi_nhan.list.size"] != StatusWarn || allBig["noi_nhan.list.italic"] != StatusWarn {
		t.Errorf("list checks: %v", allBig)
	}
	only := run(line("Nơi nhận:", 12, true, true))
	if only["noi_nhan.list.size"] != StatusSkip || only["noi_nhan.label.size"] != StatusPass {
		t.Errorf("one-line nơi nhận: %v", only)
	}
	// a fix touches the lines the rule measured
	l, seg := compLayout("noi_nhan", line("Nơi nhận:", 12, true, true), line("- A;", 11, false, false), line("- B.", 11, false, false))
	if got := RuleLines(l, seg.Comp("noi_nhan"), *rs.CheckByID("noi_nhan.list.size")); fmt.Sprint(got) != "[1 2]" {
		t.Errorf("RuleLines rest = %v", got)
	}
	if got := RuleLines(l, seg.Comp("noi_nhan"), *rs.CheckByID("noi_nhan.label.bold")); fmt.Sprint(got) != "[0]" {
		t.Errorf("RuleLines first = %v", got)
	}
}

func TestRegexPropReadsTheMatchedRuns(t *testing.T) {
	rs := mustRuleSet(t, "quyet_dinh")
	rule := *rs.CheckByID("noi_dung.dieu.bold")
	if rule.Kind != "regex_prop" {
		t.Fatalf("kind %q", rule.Kind)
	}
	dieu := func(boldHead bool) Para {
		runs := []Run{{Text: "Điều 1. ", Bold: ptr(boldHead)}, {Text: "Ban hành kèm theo Quyết định này Kế hoạch năm 2026.", Bold: ptr(false)}}
		return Para{Text: runs[0].Text + runs[1].Text, Runs: runs, Bold: ptr(false)}
	}
	eval := func(lines ...Para) CheckResult {
		l, seg := compLayout("noi_dung", lines...)
		r := Evaluate(l, seg, &RuleSet{Checks: []RuleCheck{rule}})
		return r[0]
	}
	plain := Para{Text: "Căn cứ đề nghị của Chánh Văn phòng.", Bold: ptr(false)}
	if r := eval(plain, dieu(true)); r.Status != StatusPass || fmt.Sprint(r.Actual) != "map[of:1 satisfied:1]" {
		t.Errorf("bold Điều head: %s %v", r.Status, r.Actual)
	}
	if r := eval(dieu(true), dieu(false)); r.Status != StatusWarn || r.Evidence[0]["actual"] != false {
		t.Errorf("regular Điều head: %s %v", r.Status, r.Evidence)
	}
	mixed := dieu(true)
	mixed.Runs = []Run{{Text: "Điều ", Bold: ptr(true)}, {Text: "1. Ban hành", Bold: ptr(false)}}
	mixed.Text = "Điều 1. Ban hành"
	if r := eval(mixed); r.Status != StatusWarn || r.Evidence[0]["actual"] != "mixed" {
		t.Errorf("half-bold head: %s %v", r.Status, r.Evidence)
	}
	if r := eval(plain); r.Status != StatusSkip {
		t.Errorf("no Điều: %s", r.Status)
	}
}

func TestSkipWhenUnmeasured(t *testing.T) {
	rs := mustRuleSet(t, "cong_van")
	rule := *rs.CheckByID("noi_dung.first_line_indent")
	eval := func(lines ...Para) CheckResult {
		l, seg := compLayout("noi_dung", lines...)
		return Evaluate(l, seg, &RuleSet{Checks: []RuleCheck{rule}})[0]
	}
	if r := eval(Para{Text: "a"}, Para{Text: "b"}); r.Status != StatusSkip {
		t.Errorf("no indent set anywhere is not measured: %s", r.Status)
	}
	in := Para{Text: "a", IndentFirstLineMM: ptr(10.0)}
	hanging := Para{Text: "- b", IndentFirstLineMM: ptr(0.0), IndentHangingMM: ptr(6.35)}
	cell := Para{Text: "c", IndentFirstLineMM: ptr(0.0), InTable: true}
	if r := eval(in, hanging, cell, Para{Text: "d"}); r.Status != StatusPass || fmt.Sprint(r.Actual) != "map[of:1 satisfied:1]" {
		t.Errorf("hanging / table / unset lines must not count: %s %v", r.Status, r.Actual)
	}
	if r := eval(in, Para{Text: "e", IndentFirstLineMM: ptr(0.0)}, Para{Text: "f", IndentFirstLineMM: ptr(0.0)}); r.Status != StatusWarn {
		t.Errorf("unindented body: %s", r.Status)
	}
}

func TestRunColor(t *testing.T) {
	body := tp("Đoạn một màu mặc định của văn bản.", "both", 14) +
		tp("#Đoạn hai tô màu đỏ trực tiếp trên run.", "both", 14) +
		`<w:p><w:r><w:rPr><w:rStyle w:val="Red"/></w:rPr><w:t>Chữ đỏ</w:t></w:r></w:p>` +
		`<w:p><w:r><w:rPr><w:color w:val="000000"/></w:rPr><w:t>Chữ đen</w:t></w:r></w:p>`
	l := InspectDocx(testDocx(t, body, "", "", false))
	var got []string
	for _, p := range l.Paragraphs {
		got = append(got, *p.Color)
	}
	// a character style is not resolved for runs (like fonts and sizes):
	// only paragraph styles and direct formatting count
	if strings.Join(got, ",") != "auto,FF0000,auto,000000" {
		t.Errorf("colours = %v", got)
	}
	rule := *mustRuleSet(t, "cong_van").CheckByID("noi_dung.color")
	l2, seg := compLayout("noi_dung", Para{Text: "a", Color: ptr("auto")}, Para{Text: "b", Color: ptr("000000")}, Para{Text: "c", Color: ptr("auto")})
	if r := Evaluate(l2, seg, &RuleSet{Checks: []RuleCheck{rule}})[0]; r.Status != StatusPass {
		t.Errorf("black text: %s", r.Status)
	}
	l3, seg3 := compLayout("noi_dung", Para{Text: "a", Color: ptr("FF0000")}, Para{Text: "b", Color: ptr("auto")})
	if r := Evaluate(l3, seg3, &RuleSet{Checks: []RuleCheck{rule}})[0]; r.Status != StatusWarn {
		t.Errorf("red text: %s", r.Status)
	}
}

func TestPageNumber(t *testing.T) {
	body := tp("Nội dung.", "both", 14)
	fld := `<w:p><w:pPr><w:pStyle w:val="Header"/></w:pPr><w:fldSimple w:instr=" PAGE   \* MERGEFORMAT"><w:r><w:t>2</w:t></w:r></w:fldSimple></w:p>`
	complexLeft := `<w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> PAGE </w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`
	numPages := `<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:fldSimple w:instr="NUMPAGES"><w:r><w:t>3</w:t></w:r></w:fldSimple></w:p>`
	cases := []struct {
		name           string
		header, footer string
		titlePg        bool
		status, actual string
	}{
		{"header centred, no number on page 1", fld, "", true, StatusPass,
			"map[centered:true footer_page_field:false header_page_field:true title_pg:true]"},
		{"number on page 1", fld, "", false, StatusWarn,
			"map[centered:true footer_page_field:false header_page_field:true title_pg:false]"},
		{"left-aligned complex field", complexLeft, "", true, StatusWarn,
			"map[centered:false footer_page_field:false header_page_field:true title_pg:true]"},
		{"number in the footer", "", fld, true, StatusWarn,
			"map[centered:false footer_page_field:true header_page_field:false title_pg:true]"},
		{"NUMPAGES is not a page number", numPages, "", true, StatusWarn,
			"map[centered:false footer_page_field:false header_page_field:false title_pg:true]"},
		{"no header or footer at all", "", "", false, StatusWarn,
			"map[centered:false footer_page_field:false header_page_field:false title_pg:false]"},
	}
	for _, c := range cases {
		r := Check(context.Background(), testDocx(t, body, c.header, c.footer, c.titlePg), Options{Segmenter: SegmenterHeuristic})
		got := checkByID(t, r.Checks, "page.number")
		if got.Status != c.status || fmt.Sprint(got.Actual) != c.actual {
			t.Errorf("%s: %s %v", c.name, got.Status, got.Actual)
		}
	}
}

// bottomDoc is a công văn whose nơi nhận column ends with extra lines.
func bottomDoc(t *testing.T, noiNhanTail, after string) []byte {
	head := ttbl(
		[2]string{tp("*SỞ NỘI VỤ", "center", 13), tp("*CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", "center", 13) + tp("*Độc lập - Tự do - Hạnh phúc", "center", 14)},
		[2]string{tp("Số: 45/SNV-VP", "center", 13) + tp("V/v rà soát vị trí việc làm", "center", 12), tp("_Huế, ngày 05 tháng 10 năm 2026", "center", 14)},
	)
	body := tp("Kính gửi: Các phòng, ban thuộc Sở.", "center", 14) +
		tp("Sở Nội vụ đề nghị các phòng, ban rà soát vị trí việc làm, điện thoại về Văn phòng khi cần; email báo cáo trước ngày 20.", "both", 14) +
		tp("LƯU HÀNH NỘI BỘ", "center", 13) +
		tp("Đề nghị các phòng, ban thực hiện./.", "both", 14)
	left := tp("*_Nơi nhận:", "", 12) + tp("- Như trên;", "", 11) + tp("- Lưu: VT, VP, NVA.15.", "", 11) + noiNhanTail
	right := tp("*GIÁM ĐỐC", "center", 14) + tp("", "center", 14) + tp("*Lê Thị Dung", "center", 14)
	return testDocx(t, head+body+ttbl([2]string{left, right})+after, "", "", false)
}

func TestSegmentBottomComponents(t *testing.T) {
	seg := func(content []byte) *Segmentation { return Segment(InspectDocx(content)) }
	s := seg(bottomDoc(t, tp("NVA.15", "", 11),
		tp("Địa chỉ: 01 Lê Lai, phường Thuận Hóa, Huế; ĐT: 0234 3822 111; Email: snv@hue.gov.vn", "center", 11)))
	for key, want := range map[string]string{
		"chi_dan_luu_hanh":  "LƯU HÀNH NỘI BỘ",
		"ky_hieu_soan_thao": "NVA.15",
		"dia_chi_co_quan":   "Địa chỉ: 01 Lê Lai",
	} {
		if c := s.Comp(key); !c.Found || !strings.HasPrefix(c.Text, want) {
			t.Errorf("%s = %+v", key, c)
		}
	}
	if nn := s.Comp("noi_nhan").Text; strings.Contains(nn, "NVA.15\n") || !strings.HasSuffix(nn, "- Lưu: VT, VP, NVA.15.") {
		t.Errorf("nơi nhận = %q", nn)
	}
	if nd := s.Comp("noi_dung").Text; !strings.Contains(nd, "điện thoại về Văn phòng") {
		t.Errorf("a body line mentioning điện thoại / email stays nội dung: %q", nd)
	}

	// the "- Lưu" line, or a short code above the last line, is not ô 12
	s = seg(bottomDoc(t, "", ""))
	for _, key := range []string{"ky_hieu_soan_thao", "dia_chi_co_quan"} {
		if s.Comp(key).Found {
			t.Errorf("%s found in %q", key, s.Comp(key).Text)
		}
	}
	s = seg(bottomDoc(t, tp("TTr/2b", "", 11)+tp("- Giám đốc (để b/c).", "", 11), ""))
	if s.Comp("ky_hieu_soan_thao").Found {
		t.Error("ô 12 is only the last line of the nơi nhận column")
	}
	s = seg(bottomDoc(t, tp("TTr/2b", "", 11), ""))
	if c := s.Comp("ky_hieu_soan_thao"); c.Text != "TTr/2b" {
		t.Errorf("TTr/2b: %+v", c)
	}

	// the brief shows them under their ô numbers
	r := Check(context.Background(), bottomDoc(t, tp("NVA.15", "", 11),
		tp("Địa chỉ: 01 Lê Lai; ĐT: 0234 3822 111", "center", 11)), Options{Segmenter: SegmenterHeuristic})
	for _, want := range []string{`[11] Chỉ dẫn phạm vi lưu hành: "LƯU HÀNH NỘI BỘ"`, `[12] Ký hiệu người soạn thảo, số bản: "NVA.15"`, `[13] Địa chỉ cơ quan: "Địa chỉ: 01 Lê Lai`} {
		if !strings.Contains(r.Skeleton, want) {
			t.Errorf("brief lacks %q:\n%s", want, r.Skeleton)
		}
	}
	for id, want := range map[string]string{"ky_hieu_soan_thao.size": StatusPass, "dia_chi_co_quan.size": StatusPass, "chi_dan_luu_hanh.bold": StatusWarn} {
		if got := checkByID(t, r.Checks, id).Status; got != want {
			t.Errorf("%s = %s, want %s", id, got, want)
		}
	}
}
