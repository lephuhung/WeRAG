package vietnamese_legal

import "testing"

func TestCitation(t *testing.T) {
	cases := []struct {
		m    LegalMetadata
		want string
	}{
		{LegalMetadata{ArticleNos: []string{"14"}}, "Điều 14"},
		{LegalMetadata{ArticleNos: []string{"14"}, KhoanNos: []string{"2"}}, "khoản 2 Điều 14"},
		{LegalMetadata{ArticleNos: []string{"14"}, KhoanNos: []string{"2"}, DiemLabels: []string{"a"}}, "điểm a khoản 2 Điều 14"},
		{LegalMetadata{ArticleNos: []string{"1"}, KhoanNos: []string{"5", "6"}}, "Điều 1 (khoản 5, 6)"},
		{LegalMetadata{Appendix: "PHỤ LỤC I"}, "PHỤ LỤC I"},
		{LegalMetadata{ArticleNos: []string{"3"}, Appendix: "QUY CHẾ văn thư"}, "Điều 3 – QUY CHẾ văn thư"},
		{LegalMetadata{ArticleNos: []string{"1"}, KhoanNos: []string{"6"},
			Amends: []AmendRef{{Document: "Luật A", Article: "9"}, {Document: "Nghị định B", Article: "2", Khoan: "1"}}},
			"khoản 6 Điều 1 (sửa đổi Điều 9 Luật A; khoản 1 Điều 2 Nghị định B)"},
		{LegalMetadata{}, ""},
	}
	for _, c := range cases {
		if got := c.m.Citation(); got != c.want {
			t.Errorf("Citation(%+v) = %q, want %q", c.m, got, c.want)
		}
	}
}

func TestSourceURL(t *testing.T) {
	m := LegalMetadata{Source: "vbpl", SourceDocID: "142846", ArticleNos: []string{"1"}, KhoanNos: []string{"6"},
		ProvisionIDs: map[string]string{"dieu": "d1", "khoan:6": "k6"}}
	if got := m.SourceURL(); got != "https://vbpl.vn/van-ban/chi-tiet/142846?go=k6" {
		t.Fatal(got)
	}
	m.KhoanNos = []string{"5", "6"}
	if got := m.SourceURL(); got != "https://vbpl.vn/van-ban/chi-tiet/142846?go=d1" {
		t.Fatal(got)
	}
	m.Source = ""
	if got := m.SourceURL(); got != "" {
		t.Fatal(got)
	}
}

func TestMatchesAmendment(t *testing.T) {
	meta := &LegalMetadata{ArticleNos: []string{"1"}, KhoanNos: []string{"6", "7"},
		Amends: []AmendRef{{Document: "Luật A", Article: "9"}, {Document: "Luật A", Article: "10", Khoan: "1"}}}
	for ref, want := range map[string]bool{
		"Điều 9":                 true,
		"khoản 2 Điều 9":         true, // the whole Điều 9 is rewritten
		"Điều 10":                true,
		"khoản 1 Điều 10":        true,
		"khoản 2 Điều 10":        false,
		"điểm a khoản 1 Điều 10": true,
		"Điều 11":                false,
		"khoản 6":                false, // no Điều: not an amendment lookup
	} {
		r, err := ParseSectionRef(ref)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.MatchesAmendment(meta); got != want {
			t.Errorf("%s: %v, want %v", ref, got, want)
		}
	}
}

func TestAppendixFileURLAndName(t *testing.T) {
	f := AppendixFile{Label: "II", FileName: "Thông tư 148.Phụ lục II.docx"}
	want := VBPLFileURL + "2ea7f330/Th%C3%B4ng%20t%C6%B0%20148.Ph%E1%BB%A5%20l%E1%BB%A5c%20II.docx/download"
	if got := f.URL("2ea7f330"); got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
	if got := f.URL(""); got != "" {
		t.Errorf("URL without document id = %q", got)
	}
	if got := f.Name(); got != "Phụ lục II" {
		t.Errorf("Name = %q", got)
	}
	f.Title = "Phụ lục II. Nội dung HTCT Xã"
	if got := f.Name(); got != f.Title {
		t.Errorf("Name with title = %q", got)
	}
}
