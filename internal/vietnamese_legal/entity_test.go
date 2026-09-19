package vietnamese_legal

import "testing"

func TestNormalizeOrgName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Sở Thông tin và Truyền thông", "Sở Thông Tin và Truyền Thông"},
		{"SỞ THÔNG TIN VÀ TRUYỀN THÔNG", "Sở Thông Tin và Truyền Thông"},
		{"sở thông tin và truyền thông", "Sở Thông Tin và Truyền Thông"},
		{"UBND   Tỉnh  Nghệ An", "Ubnd Tỉnh Nghệ An"},
		{"#Bộ Tài chính", "Bộ Tài Chính"},
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeOrgName(c.in); got != c.want {
			t.Errorf("NormalizeOrgName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeEntityID(t *testing.T) {
	cases := []struct {
		name, etype, want string
	}{
		// Document: số hiệu becomes the merge key, folded to uppercase
		{"Nghị định 53/2022/NĐ-CP (Chính phủ, 2022)", "Document", "53/2022/NĐ-CP"},
		{"53/2022/nđ-cp", "Document", "53/2022/NĐ-CP"},
		{"Luật An ninh mạng", "Document", "Luật An Ninh Mạng"},
		// Organization/Article/Location: canonical title-case
		{"sở thông tin và truyền thông", "Organization", "Sở Thông Tin và Truyền Thông"},
		{"điều 5", "Article", "Điều 5"},
		{"tỉnh nghệ an", "Location", "Tỉnh Nghệ An"},
		// Person/Task: untouched beyond whitespace
		{"Nguyễn Văn A (01/01/1980)", "Person", "Nguyễn Văn A (01/01/1980)"},
		{"  giám sát  an ninh mạng ", "Task", "giám sát an ninh mạng"},
	}
	for _, c := range cases {
		if got := NormalizeEntityID(c.name, c.etype); got != c.want {
			t.Errorf("NormalizeEntityID(%q, %q) = %q, want %q", c.name, c.etype, got, c.want)
		}
	}
}

func TestForceLegalType(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"Điều 5", "Organization", "Article"},
		{"Khoản 4 Điều 3 Nghị định 53/2022/NĐ-CP", "Document", "Article"},
		{"Nghị định 123/2024/NĐ-CP", "Organization", "Document"},
		{"Luật Bảo vệ Bí mật nhà nước", "Organization", "Document"},
		{"Bộ Tài chính", "Organization", "Organization"},
		{"Bộ Tài chính", "document", "Document"}, // canonicalizes casing
	}
	for _, c := range cases {
		if got := ForceLegalType(c.name, c.in); got != c.want {
			t.Errorf("ForceLegalType(%q, %q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestIsGenericOrJunkEntity(t *testing.T) {
	junk := []string{
		"Bộ", "Cơ quan nhà nước", "Cơ quan ngang Bộ", "các cơ quan Đảng",
		"tổ chức, cá nhân có liên quan", "(tên đơn vị đề nghị)", "Mẫu số 02",
		"Tờ trình", "Đơn đề nghị", "",
	}
	for _, n := range junk {
		if !IsGenericOrJunkEntity(n, "Organization") {
			t.Errorf("expected junk: %q", n)
		}
	}
	// Not junk
	keep := []string{"Bộ Tài chính", "UBND Tỉnh Nghệ An",
		"Doanh nghiệp viễn thông, doanh nghiệp cung cấp dịch vụ"}
	for _, n := range keep {
		if IsGenericOrJunkEntity(n, "Organization") {
			t.Errorf("expected keep: %q", n)
		}
	}
	// Stoplist only applies to Organization/unknown types
	if IsGenericOrJunkEntity("Điều 5", "Article") {
		t.Error("article ref wrongly dropped")
	}
	if !IsGenericOrJunkEntity("Mẫu số 02", "Document") {
		t.Error("form name should be junk for any type")
	}
}

func TestSelfRefAndDocRootAlias(t *testing.T) {
	for _, n := range []string{"văn bản này", "Nghị định này", "quyết định này"} {
		if !IsSelfReferenceName(n) {
			t.Errorf("expected self-ref: %q", n)
		}
	}
	if IsSelfReferenceName("Nghị định 13/2024/NĐ-CP") {
		t.Error("numbered doc is not a self-ref")
	}
	if !HasTrailingSelfRef("Quyết định này") || !HasTrailingSelfRef("này") {
		t.Error("trailing self-ref not detected")
	}

	// Doc root alias: exact match after normalization + dropping "(issuer, year)"
	if !IsDocRootAlias("Luật Bảo Vệ Bí Mật Nhà Nước Số 29/2018/QH14",
		"29/2018/QH14", "Luật Bảo Vệ Bí Mật Nhà Nước Số 29/2018/QH14") {
		t.Error("title should fold onto doc root")
	}
	if IsDocRootAlias("Luật An ninh mạng", "29/2018/QH14",
		"Luật Bảo Vệ Bí Mật Nhà Nước Số 29/2018/QH14") {
		t.Error("different document must NOT fold onto doc root")
	}
	if !IsDocRootAlias("Luật An ninh mạng (Quốc hội, 2018)", "", "Luật An ninh mạng") {
		t.Error("title + trailing qualifier should fold onto doc root")
	}
}

func TestNormalizeLegalDate(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1/1/1980", "01/01/1980"},
		{"1980-1-2", "02/01/1980"},
		{"1980", "không xác định"},
		{"", "không xác định"},
		{"ngày 05 tháng 3", "ngày 05 tháng 3"},
	}
	for _, c := range cases {
		if got := NormalizeLegalDate(c.in); got != c.want {
			t.Errorf("NormalizeLegalDate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildPersonCompositeKey(t *testing.T) {
	cases := []struct {
		name  string
		props map[string]string
		want  string
	}{
		{"Nguyễn Văn A", map[string]string{"ngay_sinh": "1/1/1980"}, "Nguyễn Văn A (01/01/1980)"},
		{"Nguyễn Văn A", map[string]string{"cccd": "012345678901"}, "Nguyễn Văn A (012345678901)"},
		{"Nguyễn Văn A", map[string]string{"don_vi_moi": "Sở Tài chính Nghệ An"},
			"Nguyễn Văn A (Sở Tài chính Nghệ An)"},
		{"Nguyễn Văn A", map[string]string{}, "Nguyễn Văn A (không xác định)"},
		{"Nguyễn Văn A (01/01/1980)", map[string]string{}, "Nguyễn Văn A (01/01/1980)"},
	}
	for _, c := range cases {
		if got := BuildPersonCompositeKey(c.name, c.props); got != c.want {
			t.Errorf("BuildPersonCompositeKey(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseLegalHeaderMeta(t *testing.T) {
	header := `BỘ THÔNG TIN VÀ TRUYỀN THÔNG
Số: 12/2024/TT-BTTTT

Hà Nội, ngày 5 tháng 3 năm 2024

THÔNG TƯ:
Căn cứ Luật An ninh mạng;`
	meta := ParseLegalHeaderMeta(header)
	if meta.SoHieu != "12/2024/TT-BTTTT" {
		t.Errorf("SoHieu = %q, want 12/2024/TT-BTTTT", meta.SoHieu)
	}
	if meta.NgayBanHanh != "05/03/2024" {
		t.Errorf("NgayBanHanh = %q, want 05/03/2024", meta.NgayBanHanh)
	}
	if meta.CoQuanBanHanh != "BỘ THÔNG TIN VÀ TRUYỀN THÔNG" {
		t.Errorf("CoQuanBanHanh = %q", meta.CoQuanBanHanh)
	}
	if meta.DocumentName != "12/2024/TT-BTTTT" {
		t.Errorf("DocumentName = %q", meta.DocumentName)
	}
}

func TestExtractPreambleCanCu(t *testing.T) {
	preamble := `BỘ CÔNG AN
Số: 13/2024/TT-BCA
Căn cứ Luật An ninh mạng ngày 12 tháng 6 năm 2018;
Căn cứ Nghị định số 53/2022/NĐ-CP ngày 15 tháng 8 năm 2022;
Theo đề nghị của Cục trưởng Cục An ninh mạng,

THÔNG TƯ:`
	got := ExtractPreambleCanCu(ExtractPreamble(preamble))
	if len(got) != 2 {
		t.Fatalf("expected 2 CAN_CU refs, got %v", got)
	}
	if got[0] != "Luật An ninh mạng ngày 12 tháng 6 năm 2018" {
		t.Errorf("got[0] = %q", got[0])
	}
}

func TestBuildLegalDocContext(t *testing.T) {
	header := `CHÍNH PHỦ
Số: 53/2022/NĐ-CP
Hà Nội, ngày 15 tháng 8 năm 2022
NGHỊ ĐỊNH:
Căn cứ Hiến pháp nước Cộng hòa xã hội chủ nghĩa Việt Nam;

Điều 1. Phạm vi điều chỉnh`
	ctx := BuildLegalDocContext(header, "Nghị định 53/2022/NĐ-CP về an ninh mạng", "nd53.pdf", true)
	if !ctx.IsLegal {
		t.Error("expected legal document")
	}
	if ctx.DocumentNumber != "53/2022/NĐ-CP" {
		t.Errorf("DocumentNumber = %q", ctx.DocumentNumber)
	}
	if ctx.RootName != "53/2022/NĐ-CP" {
		t.Errorf("RootName = %q", ctx.RootName)
	}
	if ctx.IssuingAgency != "CHÍNH PHỦ" {
		t.Errorf("IssuingAgency = %q", ctx.IssuingAgency)
	}
	if len(ctx.CanCu) != 1 {
		t.Errorf("CanCu = %v", ctx.CanCu)
	}

	// Non-legal content stays non-legal
	ctx2 := BuildLegalDocContext("Một đoạn văn bình thường không có cấu trúc pháp luật.",
		"Báo cáo tuần", "report.md", false)
	if ctx2.IsLegal {
		t.Error("non-legal doc wrongly detected as legal")
	}
}
