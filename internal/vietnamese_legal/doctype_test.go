package vietnamese_legal

import (
	"strings"
	"testing"
)

func TestDocTypesRegistry(t *testing.T) {
	nd30 := 0
	slugs := map[string]bool{}
	symbols := map[string]string{}
	for i, d := range DocTypes {
		if slugs[d.Slug] {
			t.Errorf("duplicate slug %q", d.Slug)
		}
		slugs[d.Slug] = true
		for _, s := range d.Symbols {
			k := strings.ToUpper(s)
			if prev, ok := symbols[k]; ok {
				t.Errorf("symbol %q used by %s and %s", s, prev, d.Slug)
			}
			symbols[k] = d.Slug
		}
		if d.Group == DocTypeGroupHanhChinh {
			nd30++
			if i >= ND30DocTypeCount {
				t.Errorf("NĐ30 type %s must be in the first %d entries", d.Slug, ND30DocTypeCount)
			}
		}
	}
	if nd30 != ND30DocTypeCount {
		t.Fatalf("NĐ30 types = %d, want %d", nd30, ND30DocTypeCount)
	}
	// Đ is significant: HĐ (hợp đồng) ≠ HD (hướng dẫn), ĐA ≠ DA.
	for code, want := range map[string]string{
		"HĐ": "hop_dong", "HD": "huong_dan", "ĐA": "de_an", "DA": "du_an",
		"QĐ": "quyet_dinh", "QyĐ": "quy_dinh", "TTr": "to_trinh",
		"TTh": "ban_thoa_thuan", "TT": "thong_tu", "CTr": "chuong_trinh",
		"CT": "chi_thi", "NĐ": "nghi_dinh",
	} {
		if d := DocTypeBySymbol(code); d == nil || d.Slug != want {
			t.Errorf("DocTypeBySymbol(%q) = %v, want %s", code, d, want)
		}
	}
}

const quocHieu = "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM\nĐộc lập - Tự do - Hạnh phúc\n"

func TestDetectDocTypeHeadingAllND30(t *testing.T) {
	for _, d := range DocTypes[:ND30DocTypeCount] {
		header := "ỦY BAN NHÂN DÂN\nTỈNH THỪA THIÊN HUẾ\n" + quocHieu +
			"Huế, ngày 05 tháng 10 năm 2026\n\n" +
			strings.ToUpper(d.Name) + "\nVề việc triển khai nhiệm vụ năm 2026\n\nNội dung…"
		got := DetectDocType(header)
		if got.Slug != d.Slug || got.Source != "heading" {
			t.Errorf("%s: got %+v", d.Slug, got)
		}
	}
}

func TestDetectDocTypeSignals(t *testing.T) {
	cases := []struct {
		name, header, slug, source string
	}{
		{"markdown table header + bold heading",
			"| ỦY BAN NHÂN DÂN | CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM |\n| Số: 45/KH-UBND | Độc lập - Tự do - Hạnh phúc |\n\n**KẾ HOẠCH**\nTriển khai …",
			"ke_hoach", "heading"},
		{"heading beats a cited type in the trích yếu",
			quocHieu + "Số: 12/BC-SYT\n\nBÁO CÁO\nKết quả thực hiện Nghị quyết số 30/NQ-HĐND\n",
			"bao_cao", "heading"},
		{"preamble heading is ignored",
			quocHieu + "Số: 7/QĐ-UBND\nQUYẾT ĐỊNH\nBan hành Quy chế …\nCăn cứ Luật …\nQUY ĐỊNH\n",
			"quyet_dinh", "heading"},
		{"nghị định",
			"CHÍNH PHỦ\n" + quocHieu + "Số: 53/2022/NĐ-CP\n\nNGHỊ ĐỊNH\nQUY ĐỊNH CHI TIẾT …\nCăn cứ …",
			"nghi_dinh", "heading"},
		{"luật",
			"QUỐC HỘI\n" + quocHieu + "Luật số: 24/2018/QH14\n\nLUẬT\nAN NINH MẠNG\nCăn cứ Hiến pháp …",
			"luat", "heading"},
		{"thông tư liên tịch before thông tư",
			quocHieu + "Số: 05/2020/TTLT-BGDĐT-BTC\nTHÔNG TƯ LIÊN TỊCH\nHướng dẫn …",
			"thong_tu_lien_tich", "heading"},
		{"symbol only",
			quocHieu + "Số: 08/TTr-UBND\nHuế, ngày 1 tháng 2 năm 2026\nVề việc phê duyệt …",
			"to_trinh", "symbol"},
		{"unit-only ký hiệu is công văn",
			quocHieu + "Số: 1234/UBND-VP\nV/v triển khai …\nKính gửi: Các sở",
			"cong_van", "symbol"},
		{"V/v without số is công văn",
			quocHieu + "V/v báo cáo số liệu\nKính gửi: …",
			"cong_van", "vv"},
		{"QH ký hiệu is not công văn",
			quocHieu + "Số: 24/2018/QH14\nnội dung không có tên loại",
			"", ""},
		{"VBHN ký hiệu is not công văn",
			quocHieu + "Số: 05/VBHN-BCT\nnội dung không có tên loại",
			"", ""},
		{"unframed file titled like a type",
			"BÁO CÁO\nTài chính quý 3 của công ty ABC",
			"", ""},
		{"agency line is not a heading",
			"BAN QUẢN LÝ DỰ ÁN\n" + quocHieu + "Số: 3/GM-BQL\nGIẤY MỜI\n",
			"giay_moi", "heading"},
	}
	for _, c := range cases {
		got := DetectDocType(c.header)
		if got.Slug != c.slug || got.Source != c.source {
			t.Errorf("%s: got %+v, want %s/%s", c.name, got, c.slug, c.source)
		}
	}
}

func TestParseReferenceND30Types(t *testing.T) {
	cases := map[string]string{
		"Kế hoạch 45/KH-UBND":             "ke_hoach",
		"tóm tắt giấy mời số 3":           "giay_moi",
		"Báo cáo thực hiện Nghị quyết 12": "bao_cao",
		"45/KH-UBND":          "ke_hoach",
		"văn bản 08/TTr-UBND": "to_trinh",
		"Thông tư 15 của Bộ Công an năm 2023":   "thong_tu",
		"quy định về thuế thu nhập cá nhân":     "",
		"cho tôi xem hướng dẫn nộp hồ sơ":       "",
		"Công điện 12/CĐ-TTg về phòng chống lũ": "cong_dien",
	}
	for in, want := range cases {
		if got := ParseReference(in).DocTypeSlug; got != want {
			t.Errorf("ParseReference(%q).DocTypeSlug = %q, want %q", in, got, want)
		}
	}
}

func TestIsLegalDocNameND30(t *testing.T) {
	cases := map[string]bool{
		"Kế hoạch 45/KH-UBND":         true,
		"Công văn số 1234":            true,
		"Thông tư hướng dẫn thi hành": true,
		"Chỉ thị về phòng chống dịch": true,
		"Luật Đất đai":                true,
		"Dự án đường cao tốc":         false,
		"Hợp đồng lao động":           false,
		"Kế hoạchX 12/KH":             false,
	}
	for in, want := range cases {
		if got := IsLegalDocName(in); got != want {
			t.Errorf("IsLegalDocName(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestBuildLegalDocContextDocType(t *testing.T) {
	header := "UBND TỈNH\n" + quocHieu + "Số: 45/KH-UBND\nKẾ HOẠCH\nTriển khai chuyển đổi số"
	ctx := BuildLegalDocContext(header, "", "kh.pdf", false)
	if ctx.DocType != "ke_hoach" || ctx.DocTypeName != "Kế hoạch" || ctx.DocTypeSource != "heading" {
		t.Fatalf("got %+v", ctx)
	}
	if !ctx.IsLegal {
		t.Fatal("an NĐ30-framed kế hoạch must be legal")
	}
	ctx = BuildLegalDocContext("", "Nghị định 13/2023/NĐ-CP", "", false)
	if ctx.DocType != "nghi_dinh" || ctx.DocTypeSource != "title" {
		t.Fatalf("title fallback: got %+v", ctx)
	}
}

// Real OCR output from the knowledge base: tone marks damaged, Đ lost.
func TestDetectDocTypeRealOCR(t *testing.T) {
	nd := "TĐT\nCHÍNH PHỦ\nCỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM\nĐộc lập - Tự do - Hạnh phúc\n" +
		"Số: 361/2025/ND-CP\nHà Nội, ngày 31 tháng 12 năm 2025\nCỘNG THÔNG TIN ĐIỆN TỦ CHÍNH PHỦ\n" +
		"NGHI ĐỊNH\nQuy định về vị trí việc làm công chức\nCăn cứ Luật Tổ chức Chính phủ số 63/2025/QH15;"
	if got := DetectDocType(nd); got.Slug != "nghi_dinh" || got.Source != "heading" {
		t.Errorf("NĐ 361: got %+v", got)
	}
	if got := RecoverDocumentNumber(nd); got != "361/2025/ND-CP" {
		t.Errorf("NĐ 361 number = %q", got)
	}
	// Without the heading, the Đ-less ký hiệu must still read as nghị định,
	// never as công văn.
	noHeading := strings.Replace(nd, "NGHI ĐỊNH\n", "", 1)
	if got := DetectDocType(noHeading); got.Slug != "nghi_dinh" || got.Source != "symbol" {
		t.Errorf("ND symbol fallback: got %+v", got)
	}

	party := "BAN CHÁP HÀNH TRUNG UƠNG\n*\nSố 57-NQ/TW\nĐẦNG CỘNG SẢN VIỆT NAM\n" +
		"Hà Nội, ngày 22 tháng 12 năm 2024\nNGHỊ QUYẾT\nCỦA BỘ CHÍNH TRỊ\nvề đột phá phát triển khoa học"
	if got := DetectDocType(party); got.Slug != "nghi_quyet" || got.Source != "heading" {
		t.Errorf("NQ 57: got %+v", got)
	}
	if got := RecoverDocumentNumber(party); got != "57-NQ/TW" {
		t.Errorf("NQ 57 number = %q", got)
	}
	if got := NormalizeOwnDocumentNumber("Số 57-NQ/TW"); got != "57-NQ/TW" {
		t.Errorf("party number normalised to %q", got)
	}
	if d := DocTypeFromNumber("57-NQ/TW"); d == nil || d.Slug != "nghi_quyet" {
		t.Errorf("DocTypeFromNumber(57-NQ/TW) = %v", d)
	}
	// HD stays hướng dẫn: the folded fallback never overrides a real code.
	if d := DocTypeBySymbol("HD"); d == nil || d.Slug != "huong_dan" {
		t.Errorf("HD = %v", d)
	}
}

func TestRestoreDocumentNumberSymbol(t *testing.T) {
	cases := []struct{ num, slug, want string }{
		{"361/2025/ND-CP", "", "361/2025/NĐ-CP"},          // unambiguous fold
		{"361/2025/ND-CP", "nghi_dinh", "361/2025/NĐ-CP"}, // by type
		{"12/QD-UBND", "", "12/QĐ-UBND"},
		{"7/CD-TTg", "", "7/CĐ-TTg"},
		{"1/QYD-UBND", "", "1/QyĐ-UBND"},
		{"3/HD-UBND", "", "3/HD-UBND"},         // HD is hướng dẫn — left alone
		{"3/HD-UBND", "hop_dong", "3/HĐ-UBND"}, // …unless the type says hợp đồng
		{"9/DA-UBND", "de_an", "9/ĐA-UBND"},
		{"9/DA-UBND", "du_an", "9/DA-UBND"},
		{"53/2022/NĐ-CP", "nghi_dinh", "53/2022/NĐ-CP"}, // already correct
		{"8/Ttr-SYT", "to_trinh", "8/TTr-SYT"},
		{"57-NQ/TW", "nghi_quyet", "57-NQ/TW"},
		{"1234/UBND-VP", "cong_van", "1234/UBND-VP"}, // issuer part untouched
		{"24/2018/QH14", "luat", "24/2018/QH14"},
	}
	for _, c := range cases {
		if got := RestoreDocumentNumberSymbol(c.num, c.slug); got != c.want {
			t.Errorf("RestoreDocumentNumberSymbol(%q, %q) = %q, want %q", c.num, c.slug, got, c.want)
		}
	}
}

func TestBuildLegalDocContextRestoresSymbol(t *testing.T) {
	header := "CHÍNH PHỦ\nCỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM\nSố: 361/2025/ND-CP\nNGHI ĐỊNH\nQuy định về vị trí việc làm"
	ctx := BuildLegalDocContext(header, "", "361-ndcp.pdf", false)
	if ctx.DocumentNumber != "361/2025/NĐ-CP" || ctx.RootName != "361/2025/NĐ-CP" {
		t.Fatalf("got number=%q root=%q", ctx.DocumentNumber, ctx.RootName)
	}
}
