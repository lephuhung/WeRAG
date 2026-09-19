package vietnamese_legal

import (
	"strings"
	"testing"
)

const legalDoc = `QUỐC HỘI
CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM

Luật số: 24/2018/QH14

Chương I
NHỮNG QUY ĐỊNH CHUNG

Điều 1. Phạm vi điều chỉnh
Luật này quy định về an ninh mạng.

Điều 2. Giải thích từ ngữ
1. Trong Luật này, các từ ngữ dưới đây được hiểu như sau:
a) An ninh mạng là sự bảo đảm.
b) Không gian mạng là môi trường.
2. Tội phạm công nghệ cao là tội phạm.

Điều 3. Áp dụng pháp luật
1. Hoạt động an ninh mạng tuân theo luật này.
2. Trường hợp có điều ước quốc tế khác thì áp dụng điều ước đó.

Chương II
BIỆN PHÁP BẢO ĐẢM AN NINH MẠNG

Điều 4. Nguyên tắc bảo đảm
Việc bảo đảm an ninh mạng tuân theo nguyên tắc.

Điều 5. Bảo vệ hệ thống thông tin quan trọng
1. Hệ thống thông tin quan trọng được bảo vệ.
2. Bộ trưởng Bộ Công an quy định chi tiết.
`

func TestFindHeadings_BasicStructure(t *testing.T) {
	headings := FindHeadings(legalDoc)
	var levels []int
	var dieuTitles []string
	for _, h := range headings {
		levels = append(levels, h.Level)
		if h.Level == LevelDieu {
			dieuTitles = append(dieuTitles, h.Title)
		}
	}
	// Expect: Chương I, Điều 1-3, Chương II, Điều 4-5 → 2 chương + 5 điều
	if len(headings) != 7 {
		t.Fatalf("expected 7 headings, got %d: %+v", len(headings), headings)
	}
	if len(dieuTitles) != 5 {
		t.Fatalf("expected 5 điều headings, got %v", dieuTitles)
	}
	if !strings.HasPrefix(dieuTitles[0], "Điều 1") {
		t.Errorf("first điều title = %q", dieuTitles[0])
	}
}

func TestFindHeadings_RuneOffsets(t *testing.T) {
	headings := FindHeadings(legalDoc)
	runes := []rune(legalDoc)
	for _, h := range headings {
		// The heading's title keyword should appear at its rune offset.
		line := string(runes[h.Start:])
		if nl := strings.IndexRune(line, '\n'); nl >= 0 {
			line = line[:nl]
		}
		if !strings.Contains(line, strings.Fields(h.Title)[0]) {
			t.Errorf("heading %q not found at rune offset %d: %q", h.Title, h.Start, line)
		}
	}
}

func TestFindHeadings_RejectsReferences(t *testing.T) {
	// Body lines opening with a reference must not become headings.
	text := `Điều 1. Phạm vi
Nội dung điều 1.
Điều 5 và Điều 6 của Nghị định này quy định chi tiết.
Điều 2. Giải thích
Nội dung điều 2.
Điều 3. Áp dụng
Nội dung điều 3.`
	headings := FindHeadings(text)
	for _, h := range headings {
		if strings.Contains(h.Title, "và Điều 6") {
			t.Errorf("reference line wrongly accepted as heading: %q", h.Title)
		}
	}
	if len(headings) != 3 {
		t.Fatalf("expected 3 headings, got %d: %+v", len(headings), headings)
	}
}

func TestFindHeadings_BareDieuNextLineTitle(t *testing.T) {
	// OCR dropped the dot: bare "Điều 17" + next line starts uppercase → heading.
	text := `Điều 1. Phạm vi
abc
Điều 2. Giải thích
abc
Điều 17
Hồ sơ đăng ký lưu hành
abc`
	headings := FindHeadings(text)
	var dieu []string
	for _, h := range headings {
		if h.Level == LevelDieu {
			dieu = append(dieu, h.Title)
		}
	}
	if len(dieu) != 3 {
		t.Fatalf("expected 3 điều (incl. bare Điều 17), got %v", dieu)
	}
}

func TestFindHeadings_BareDieuNextLineLower(t *testing.T) {
	// Bare "Điều 17" + next line lowercase → wrapped reference, not a heading.
	text := `Điều 1. Phạm vi
abc
Điều 2. Giải thích
abc
Điều 17
và khoản 2 Điều 5 Nghị định này`
	headings := FindHeadings(text)
	for _, h := range headings {
		if strings.HasPrefix(h.Title, "Điều 17") {
			t.Errorf("wrapped reference wrongly accepted: %q", h.Title)
		}
	}
}

func TestHasLegalStructure(t *testing.T) {
	if !HasLegalStructure(legalDoc) {
		t.Error("legalDoc should have legal structure (5 Điều)")
	}
	if HasLegalStructure("Điều 1. A\nx\nĐiều 2. B\ny") {
		t.Error("2 Điều < MinDieuHeadings should not be legal structure")
	}
	if HasLegalStructure("plain text without headings") {
		t.Error("plain text should not be legal structure")
	}
}

func TestParseSubdivisions_Sequencing(t *testing.T) {
	text := `Điều 2. Giải thích từ ngữ
1. Trong Luật này, các từ ngữ dưới đây được hiểu như sau:
a) An ninh mạng là sự bảo đảm.
b) Không gian mạng là môi trường.
2. Tội phạm công nghệ cao là tội phạm.`
	res := ParseSubdivisions(text)
	var khoans, diems []string
	for _, s := range res.Subdivisions {
		if s.Kind == "khoan" {
			khoans = append(khoans, s.Label)
		} else {
			diems = append(diems, s.Label)
		}
	}
	if len(khoans) != 2 || khoans[0] != "1" || khoans[1] != "2" {
		t.Errorf("khoans = %v, want [1 2]", khoans)
	}
	if len(diems) != 2 || diems[0] != "a" || diems[1] != "b" {
		t.Errorf("diems = %v, want [a b]", diems)
	}
	for _, s := range res.Subdivisions {
		if s.ArticleNo != "2" {
			t.Errorf("subdivision bound to article %q, want 2", s.ArticleNo)
		}
		if s.Kind == "diem" && s.ParentKhoan != "1" {
			t.Errorf("điểm %s bound to khoản %q, want 1", s.Label, s.ParentKhoan)
		}
	}
}

func TestParseSubdivisions_MustStartAtOne(t *testing.T) {
	text := `Điều 3. Áp dụng
3. Khoản ba trước (khoản 1-2 bị thiếu do OCR).`
	res := ParseSubdivisions(text)
	if len(res.Subdivisions) != 0 {
		t.Errorf("first khoản != 1 must be rejected, got %+v", res.Subdivisions)
	}
	if res.Stats.AmbiguousRejected == 0 {
		t.Error("rejected candidate not counted")
	}
}

func TestParseSubdivisions_DiemNeedsKhoan(t *testing.T) {
	// A điểm without an open khoản is rejected.
	text := `Điều 3. Áp dụng
a) điểm a lẻ không thuộc khoản nào`
	res := ParseSubdivisions(text)
	if len(res.Subdivisions) != 0 {
		t.Errorf("điểm without khoản must be rejected, got %+v", res.Subdivisions)
	}
}

func TestParseSubdivisions_GluedTailRescue(t *testing.T) {
	// "1. a)" where the line is JUST "1. a)" — footnote glue: the digit is a
	// footnote number, "a" is the real điểm. But a real khoản "1. a) abc" has
	// text after the letter.
	text := `Điều 2. Giải thích
1. Khoản một có nội dung.
a) điểm a của khoản 1`
	res := ParseSubdivisions(text)
	if len(res.Subdivisions) != 2 {
		t.Fatalf("expected 1 khoản + 1 điểm, got %+v", res.Subdivisions)
	}
}

func TestDeriveHeadingPaths_CarryForward(t *testing.T) {
	chunks := []string{
		"Chương I\nNHỮNG QUY ĐỊNH CHUNG\n\nĐiều 1. Phạm vi\nLuật này quy định.",
		"Điều 2. Giải thích\n1. Trong Luật này.",
		"phần tiếp theo của điều 2 không có heading riêng",
		"Điều 3. Áp dụng\n2. Trường hợp điều ước.",
	}
	paths := DeriveHeadingPaths(chunks)
	if len(paths) != 4 {
		t.Fatalf("got %d paths", len(paths))
	}
	// Chunk 0: Chương I + Điều 1
	if len(paths[0]) != 2 || !strings.HasPrefix(paths[0][0], "Chương I") || !strings.HasPrefix(paths[0][1], "Điều 1") {
		t.Errorf("path[0] = %v", paths[0])
	}
	// Chunk 1: carried Chương I + Điều 2
	if len(paths[1]) != 2 || !strings.HasPrefix(paths[1][0], "Chương I") || !strings.HasPrefix(paths[1][1], "Điều 2") {
		t.Errorf("path[1] = %v", paths[1])
	}
	// Chunk 2: no heading — inherits carried Chương + open Điều 2
	if len(paths[2]) != 2 || !strings.HasPrefix(paths[2][1], "Điều 2") {
		t.Errorf("path[2] = %v (should inherit Điều 2)", paths[2])
	}
	// Chunk 3: Điều 3 replaces Điều 2
	if len(paths[3]) != 2 || !strings.HasPrefix(paths[3][1], "Điều 3") {
		t.Errorf("path[3] = %v", paths[3])
	}
}

func TestDeriveHeadingPaths_MidArticleChunk(t *testing.T) {
	// Content before the chunk's first header still belongs to the open Điều.
	chunks := []string{
		"Điều 1. Phạm vi\nLuật này quy định về an ninh mạng.",
		"tiếp tục nội dung điều 1\nĐiều 2. Giải thích\n1. Trong luật này.",
	}
	paths := DeriveHeadingPaths(chunks)
	// Chunk 1 should list BOTH Điều 1 (mid-article prefix) and Điều 2.
	var hasD1, hasD2 bool
	for _, c := range paths[1] {
		if strings.HasPrefix(c, "Điều 1") {
			hasD1 = true
		}
		if strings.HasPrefix(c, "Điều 2") {
			hasD2 = true
		}
	}
	if !hasD1 || !hasD2 {
		t.Errorf("path[1] = %v, want both Điều 1 and Điều 2", paths[1])
	}
}

func TestExtractArticleNos(t *testing.T) {
	got := ExtractArticleNos([]string{"Chương II ABC", "Điều 17. Hồ sơ", "Điều 5a: X"})
	if len(got) != 2 || got[0] != "17" || got[1] != "5a" {
		t.Errorf("article nos = %v", got)
	}
	got2 := ExtractArticleNosString("Chương II > Điều 30. X")
	if len(got2) != 1 || got2[0] != "30" {
		t.Errorf("article nos from string = %v", got2)
	}
}

func TestDeriveSubdivisionMetadata_InheritAcrossChunks(t *testing.T) {
	chunks := []string{
		"Điều 2. Giải thích\n1. Trong Luật này:\na) an ninh mạng",
		"phần còn lại của khoản 1 điều 2",
		"Điều 3. Áp dụng\n1. Hoạt động an ninh mạng.",
	}
	paths := DeriveHeadingPaths(chunks)
	metas := DeriveSubdivisionMetadata(chunks, paths)
	if len(metas) != 3 {
		t.Fatalf("got %d metas", len(metas))
	}
	// Chunk 0: khoản 1 + điểm a
	if len(metas[0].KhoanNos) != 1 || metas[0].KhoanNos[0] != "1" {
		t.Errorf("chunk0 khoans = %v", metas[0].KhoanNos)
	}
	if len(metas[0].DiemLabels) != 1 || metas[0].DiemLabels[0] != "a" {
		t.Errorf("chunk0 diems = %v", metas[0].DiemLabels)
	}
	// Chunk 1: continuation — inherits khoản 1 (+ open điểm a? điểm state
	// carries as part of the open ref)
	if len(metas[1].KhoanNos) != 1 || metas[1].KhoanNos[0] != "1" {
		t.Errorf("chunk1 should inherit khoản 1, got %v", metas[1].KhoanNos)
	}
	// Chunk 2: new Điều resets; its own khoản 1
	if len(metas[2].KhoanNos) != 1 || metas[2].KhoanNos[0] != "1" {
		t.Errorf("chunk2 khoans = %v", metas[2].KhoanNos)
	}
	// Refs format
	found := false
	for _, r := range metas[0].SubdivisionRefs {
		if r == "khoan:1/diem:a" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ref khoan:1/diem:a, got %v", metas[0].SubdivisionRefs)
	}
}
