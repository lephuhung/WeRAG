package vietnamese_legal

import "testing"

func TestParseSectionRef(t *testing.T) {
	cases := []struct {
		in   string
		want SectionRef
	}{
		{"Điều 17", SectionRef{Article: "17"}},
		{"điều 5a", SectionRef{Article: "5a"}},
		{"khoản 2 Điều 17", SectionRef{Article: "17", Khoan: "2"}},
		{"Điều 17, khoản 02", SectionRef{Article: "17", Khoan: "2"}},
		{"điểm a khoản 2 Điều 17", SectionRef{Article: "17", Khoan: "2", Diem: "a"}},
		{"Điểm đ Khoản 1 Điều 4", SectionRef{Article: "4", Khoan: "1", Diem: "đ"}},
		{"Chương II", SectionRef{Heading: "chương ii"}},
		{"Mục 2 Chương III", SectionRef{Heading: "chương iii"}},
		{"khoản 2 và khoản 3 Điều 17", SectionRef{Article: "17", Khoan: "2", Multi: true}},
	}
	for _, c := range cases {
		got, err := ParseSectionRef(c.in)
		if err != nil {
			t.Errorf("ParseSectionRef(%q) error: %v", c.in, err)
			continue
		}
		if c.in == "Mục 2 Chương III" {
			// The first heading keyword wins; both are headings, so Multi.
			if got.Heading != "mục 2" || !got.Multi {
				t.Errorf("ParseSectionRef(%q) = %#v", c.in, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("ParseSectionRef(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
	if _, err := ParseSectionRef("phạm vi điều chỉnh"); err == nil {
		t.Error("prose without a section must not parse")
	}
	if _, err := ParseSectionRef(""); err == nil {
		t.Error("empty input must not parse")
	}
}

func TestSectionRefString(t *testing.T) {
	if s := (SectionRef{Article: "17", Khoan: "2", Diem: "a"}).String(); s != "Điểm a Khoản 2 Điều 17" {
		t.Errorf("String() = %q", s)
	}
	if s := (SectionRef{Heading: "chương ii"}).String(); s != "Chương II" {
		t.Errorf("String() = %q", s)
	}
}

func TestSectionRefMatches(t *testing.T) {
	meta := &LegalMetadata{
		HeadingPath:     []string{"Chương II. TỔ CHỨC THỰC HIỆN", "Điều 17. Trách nhiệm"},
		ArticleNos:      []string{"17"},
		KhoanNos:        []string{"1", "2"},
		DiemLabels:      []string{"a", "b"},
		SubdivisionRefs: []string{"khoan:1", "khoan:2", "khoan:2/diem:a", "khoan:2/diem:b"},
	}
	yes := []SectionRef{
		{Article: "17"}, {Article: "17", Khoan: "2"}, {Article: "17", Khoan: "2", Diem: "a"},
		{Heading: "chương ii"}, {Heading: "chương ii", Article: "17"}, {Khoan: "1"}, {Diem: "b"},
	}
	no := []SectionRef{
		{Article: "1"}, {Article: "170"}, {Article: "17", Khoan: "3"},
		{Article: "17", Khoan: "1", Diem: "a"}, // điểm a belongs to khoản 2, never cross-product
		{Heading: "chương i"}, {Heading: "chương iii"}, {},
	}
	for _, r := range yes {
		if !r.Matches(meta) {
			t.Errorf("%+v must match", r)
		}
	}
	for _, r := range no {
		if r.Matches(meta) {
			t.Errorf("%+v must not match", r)
		}
	}
	if (SectionRef{Article: "17"}).Matches(nil) {
		t.Error("nil metadata never matches")
	}
}

func TestSectionRefContentPattern(t *testing.T) {
	re := (SectionRef{Article: "3"}).ContentPattern()
	if !re.MatchString("## Điều 3. Giải thích từ ngữ") || re.MatchString("Điều 30. Hiệu lực") ||
		re.MatchString("theo Điều 3 của Luật") {
		t.Error("article pattern must match only a heading line with that exact number")
	}
	if (SectionRef{Khoan: "2"}).ContentPattern() != nil {
		t.Error("khoản alone has no content fallback")
	}
	if (SectionRef{Heading: "chương ii"}).ContentPattern() == nil {
		t.Error("heading fallback missing")
	}
}
