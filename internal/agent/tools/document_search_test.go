package tools

import (
	"strings"
	"testing"
)

func unitsOf(texts ...string) []searchUnit {
	out := make([]searchUnit, len(texts))
	for i, t := range texts {
		out[i] = searchUnit{Index: i * 2, Text: t} // indexes need not be contiguous
	}
	return out
}

func TestFoldSearch(t *testing.T) {
	for in, want := range map[string]string{
		"Đề nghị  CẢI CÁCH":     "de nghi cai cach",
		"thu 1.250.000 đồng":    "thu 1250000 dong",
		"Số: 45/KH-UBND":        "so: 45/kh-ubnd",
		"Phường Thuận Hòa, 1,5": "phuong thuan hoa, 15",
	} {
		if got := foldSearch(in); got != want {
			t.Errorf("foldSearch(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseSearchQueryFindsCodes(t *testing.T) {
	q := parseSearchQuery("Theo Kế hoạch 45/KH-UBND, Điều 5 giao PA05 và CNTT bao nhiêu? Chi 430 tỷ đồng, đạt 15%")
	var codes []string
	for _, c := range q.codes {
		codes = append(codes, c.text)
	}
	got := strings.Join(codes, ",")
	for _, want := range []string{"45/kh-ubnd", "dieu 5", "pa05", "cntt", "430 ty", "15%"} {
		if !strings.Contains(","+got+",", ","+want+",") {
			t.Errorf("codes %q lack %q", got, want)
		}
	}
	for _, term := range q.terms {
		if term.lower == "bao" || term.lower == "nhiêu" || term.lower == "và" {
			t.Errorf("stop word %q kept as a term", term.lower)
		}
	}
}

func TestSearchMatchesWithAndWithoutDiacritics(t *testing.T) {
	units := unitsOf(
		"Sở Nội vụ triển khai công tác cải cách hành chính năm 2026.",
		"Phòng chống thiên tai và tìm kiếm cứu nạn.",
		"Báo cáo kết quả thực hiện nhiệm vụ.",
	)
	for _, query := range []string{"phòng chống thiên tai", "phong chong thien tai"} {
		hits := searchDocuments(query, nil, units)
		if len(hits) == 0 || hits[0].Pos != 1 {
			t.Fatalf("%q: hits %+v", query, hits)
		}
	}
	// a term typed with diacritics prefers its own spelling
	accent := unitsOf("Ban hành quy chế.", "Bán đấu giá tài sản.")
	hits := searchDocuments("bán", nil, accent)
	if len(hits) == 0 || hits[0].Pos != 1 {
		t.Fatalf("bán: %+v", hits)
	}
}

func TestSearchCodesOutweighWords(t *testing.T) {
	units := unitsOf(
		"Các phòng nghiệp vụ phối hợp kiểm tra an ninh mạng thường xuyên.",
		"Giao Phòng PA05 kiểm tra lỗ hổng bảo mật.",
		"Phòng PA051 lưu trữ hồ sơ.",
		"Thực hiện theo Kế hoạch số 45/KH-UBND ngày 3/2/2026.",
	)
	hits := searchDocuments("PA05 kiểm tra gì", nil, units)
	if len(hits) == 0 || hits[0].Pos != 1 {
		t.Fatalf("PA05 must rank its paragraph first: %+v", hits)
	}
	for _, h := range hits {
		if h.Pos == 2 && len(h.Codes) > 0 {
			t.Fatal("PA05 is not in PA051")
		}
	}
	hits = searchDocuments("kế hoạch 45/kh-ubnd", nil, units)
	if len(hits) == 0 || hits[0].Pos != 3 || hits[0].Codes[0] != "45/kh-ubnd" {
		t.Fatalf("số ký hiệu: %+v", hits)
	}
	// figures match across thousand separators
	money := unitsOf("Tổng thu đạt 1.250 tỷ đồng.", "Tổng chi 430 tỷ đồng.")
	if hits := searchDocuments("thu 1250 tỷ", nil, money); len(hits) == 0 || hits[0].Pos != 0 {
		t.Fatalf("figure: %+v", hits)
	}
}

func TestSearchPhraseAndHeaderBonus(t *testing.T) {
	units := unitsOf(
		"Hành chính các đơn vị cải tiến, cách làm mới.",
		"Đẩy mạnh cải cách hành chính trong toàn tỉnh.",
	)
	hits := searchDocuments("cải cách hành chính", nil, units)
	if len(hits) < 2 || hits[0].Pos != 1 {
		t.Fatalf("the phrase must win: %+v", hits)
	}
	head := []searchUnit{
		{Index: 0, Text: "Giao các sở thực hiện.", Header: "Điều 3. Tổ chức thực hiện"},
		{Index: 1, Text: "Giao các sở thực hiện.", Header: "Điều 2. Nhiệm vụ"},
	}
	hits = searchDocuments("tổ chức thực hiện", nil, head)
	if len(hits) < 2 || hits[0].Pos != 0 {
		t.Fatalf("header match must win: %+v", hits)
	}
	if hits := searchDocuments("điều 3", nil, head); len(hits) == 0 || hits[0].Pos != 0 {
		t.Fatalf("a reference in the header: %+v", hits)
	}
}

func TestSearchNeighboursStayInTheirDocument(t *testing.T) {
	units := []searchUnit{
		{Doc: 0, Index: 0, Text: "Mở đầu."},
		{Doc: 0, Index: 4, Text: "Ngân sách tỉnh năm 2026."},
		{Doc: 1, Index: 0, Text: "Ngân sách huyện."},
		{Doc: 1, Index: 1, Text: "Kết luận."},
	}
	hits := searchDocuments("ngân sách", nil, units)
	if len(hits) != 2 {
		t.Fatalf("hits: %+v", hits)
	}
	for _, h := range hits {
		switch h.Pos {
		case 1:
			if h.Prev != 0 || h.Next != -1 {
				t.Fatalf("doc 0 neighbours: %+v", h)
			}
		case 2:
			if h.Prev != -1 || h.Next != 3 {
				t.Fatalf("doc 1 neighbours: %+v", h)
			}
		}
	}
	if searchDocuments("và của các", nil, units) != nil {
		t.Fatal("a query of stop words finds nothing")
	}
}

func TestPassageTerms(t *testing.T) {
	got := passageTerms("Năm 2025 thu ngân sách đạt 1.250 tỷ đồng theo Báo cáo 15/BC-STC.", 6)
	joined := strings.Join(got, ",")
	for _, want := range []string{"15/bc-stc", "1250 ty", "ngân"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("passage terms %q lack %q", joined, want)
		}
	}
	if len(got) > 6 {
		t.Fatalf("limit: %v", got)
	}
}
