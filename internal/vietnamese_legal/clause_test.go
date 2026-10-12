package vietnamese_legal

import "testing"

func TestSplitClauseRef(t *testing.T) {
	cases := []struct{ in, lead, doc string }{
		{"Điều 5", "Điều 5", ""},
		{"điều 17a", "Điều 17a", ""},
		{"Khoản 2 Điều 3", "Khoản 2 Điều 3", ""},
		{"điểm a khoản 2 Điều 17", "Điểm a Khoản 2 Điều 17", ""},
		{"Điểm đ Khoản 1 Điều 4", "Điểm đ Khoản 1 Điều 4", ""},
		{"Khoản 3 Điều 49 của Luật An ninh mạng", "Khoản 3 Điều 49", "Luật An ninh mạng"},
		{"Điều 5 Nghị định 53/2022/NĐ-CP", "Điều 5", "Nghị định 53/2022/NĐ-CP"},
		{"Điều 5 của Nghị định này", "Điều 5", "Nghị định này"},
		{"Chương II", "Chương ii", ""},
		{"Bộ Công an", "", "Bộ Công an"},
		{"Điều lệ Đảng", "", "Điều lệ Đảng"},
	}
	for _, c := range cases {
		lead, doc := SplitClauseRef(c.in)
		if lead != c.lead || doc != c.doc {
			t.Errorf("SplitClauseRef(%q) = (%q, %q), want (%q, %q)", c.in, lead, doc, c.lead, c.doc)
		}
	}
}

func TestScopeClauseName(t *testing.T) {
	const root = "53/2022/NĐ-CP"
	const title = "Nghị định 53/2022/NĐ-CP về an ninh mạng"
	cases := []struct {
		in       string
		key      string
		external bool
	}{
		{"Điều 5", "Điều 5 (53/2022/NĐ-CP)", false},
		{"khoản 2 điều 5", "Khoản 2 Điều 5 (53/2022/NĐ-CP)", false},
		{"Điều 5 của Nghị định này", "Điều 5 (53/2022/NĐ-CP)", false},
		{"Điều 5 Nghị định 53/2022/nđ-cp", "Điều 5 (53/2022/NĐ-CP)", false},
		{"Điều 5 (53/2022/NĐ-CP)", "Điều 5 (53/2022/NĐ-CP)", false}, // idempotent
		{"Khoản 3 Điều 49 của Luật An ninh mạng", "Khoản 3 Điều 49 của Luật An Ninh Mạng", true},
		{"Điều 7 Nghị định 13/2023/NĐ-CP", "Điều 7 Nghị Định 13/2023/nđ-cp", true},
		{"Bộ Công an", "Bộ Công An", false},
	}
	for _, c := range cases {
		key, external := ScopeClauseName(c.in, root, title)
		if key != c.key || external != c.external {
			t.Errorf("ScopeClauseName(%q) = (%q, %v), want (%q, %v)", c.in, key, external, c.key, c.external)
		}
	}
	if key, _ := ScopeClauseName("Điều 5", "", ""); key != "Điều 5" {
		t.Errorf("no root: got %q", key)
	}
}

func TestScopeAmendedClause(t *testing.T) {
	amends := []AmendRef{{Document: "Luật Phòng, chống thiên tai", Article: "9"}}
	own := []string{"1"}
	if key, ok := ScopeAmendedClause("khoản 2 Điều 9", own, amends); !ok || key != NormalizeOrgName("Khoản 2 Điều 9 Luật Phòng, chống thiên tai") {
		t.Fatalf("got %q %v", key, ok)
	}
	for _, name := range []string{"Điều 1", "Điều 10", "Điều 9 Luật Đê điều", "Bộ Công an"} {
		if _, ok := ScopeAmendedClause(name, own, amends); ok {
			t.Errorf("%q must not be scoped to the amended document", name)
		}
	}
	if _, ok := ScopeAmendedClause("Điều 9", own, nil); ok {
		t.Error("no amendments, no scoping")
	}
}
