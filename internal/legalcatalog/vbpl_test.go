package legalcatalog

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestVBPLRelationEdge(t *testing.T) {
	cases := []struct {
		label    string
		from, to string
		rel      types.LegalRelation
	}{
		// Luật Đất đai 2024 (A) lists 2013 (B) as "Văn bản hết hiệu lực":
		// A ends B.
		{"Văn bản hết hiệu lực", "A", "B", types.LegalRelationExpires},
		// B's page lists A as "Văn bản quy định hết hiệu lực": still A ends B.
		{"Văn bản quy định hết hiệu lực", "B", "A", types.LegalRelationExpires},
		{"Thay thế", "A", "B", types.LegalRelationReplaces},
		{"Văn bản HD, QĐ chi tiết", "B", "A", types.LegalRelationGuides},
		{"Văn bản sửa đổi", "B", "A", types.LegalRelationAmends},
		{"Căn cứ", "A", "B", types.LegalRelationBasedOn},
	}
	for _, c := range cases {
		from, to, rel, ok := VBPLRelationEdge("A", "B", c.label)
		if !ok || from != c.from || to != c.to || rel != c.rel {
			t.Errorf("%q: got %s %s %s ok=%v, want %s %s %s", c.label, from, rel, to, ok, c.from, c.rel, c.to)
		}
	}
	if _, _, _, ok := VBPLRelationEdge("A", "B", "Bản dịch"); ok {
		t.Error("Bản dịch must not become a relation")
	}
}

func TestCleanVBPLNumber(t *testing.T) {
	for in, want := range map[string]string{
		"số 99/2025/QH15":        "99/2025/QH15",
		"Luật số: 118/2025/QH15": "118/2025/QH15",
		"'41/2025/NQ-HĐND":       "41/2025/NQ-HĐND",
		"30/2020/NĐ-CP":          "30/2020/NĐ-CP",
		" Không số ":             "Không số",
	} {
		if got := CleanVBPLNumber(in); got != want {
			t.Errorf("CleanVBPLNumber(%q) = %q, want %q", in, got, want)
		}
	}
}
