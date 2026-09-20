package abbreviation

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestIsLikelyAbbreviation(t *testing.T) {
	cases := []struct {
		word string
		want bool
	}{
		{"UBND", true},    // all-uppercase → abbreviation
		{"BMNN", true},    // all-uppercase → abbreviation
		{"TTGT", true},    // all-uppercase
		{"bmnn", true},    // lowercase, 4 runes, 0 vowels → ratio 0 < 0.20
		{"ttgt", true},    // lowercase, no vowels
		{"là", false},     // stop word — never
		{"bộ", false},     // 2-letter stop word
		{"một", false},    // stop word
		{"không", false},  // stop word, also vowel-rich
		{"trong", false},  // stop word
		{"cho", false},    // stop word
		{"nghiệp", false}, // lowercase but vowel-rich
		{"điều", false},   // vowel ratio too high
		{"a", false},      // single char
		{"Phường", false}, // mixed case → not all-upper, not all-lower
	}
	for _, c := range cases {
		if got := IsLikelyAbbreviation(c.word); got != c.want {
			t.Errorf("IsLikelyAbbreviation(%q) = %v, want %v", c.word, got, c.want)
		}
	}
}

func TestExtractWordTokens(t *testing.T) {
	tokens := extractWordTokens("UBND cấp tỉnh và UBND cấp huyện — NĐ-CP 172/GM-UBND")
	// "UBND" appears twice but is deduplicated; 172 and GM-UBND parts are
	// word tokens too ("172", "GM", "UBND").
	want := map[string]bool{"UBND": true, "cấp": true, "tỉnh": true, "và": true,
		"huyện": true, "NĐ": true, "CP": true, "172": true, "GM": true}
	if len(tokens) != len(want) {
		t.Fatalf("got %v", tokens)
	}
	for _, tok := range tokens {
		if !want[tok] {
			t.Errorf("unexpected token %q", tok)
		}
	}
}

func active(short, full string) *types.Abbreviation {
	return &types.Abbreviation{ShortForm: short, FullForm: full, IsActive: true}
}

func TestExpand_SingleMeaning(t *testing.T) {
	res := Expand("UBND tỉnh đã ban hành quyết định", []*types.Abbreviation{
		active("UBND", "Ủy ban nhân dân"),
	})
	if res.Expanded != "Ủy ban nhân dân tỉnh đã ban hành quyết định" {
		t.Errorf("expanded = %q", res.Expanded)
	}
	if len(res.Applied) != 1 || res.Applied[0].ShortForm != "UBND" {
		t.Errorf("applied = %+v", res.Applied)
	}
	if len(res.Ambiguous) != 0 || len(res.Potential) != 0 {
		t.Errorf("unexpected ambiguous/potential: %+v %+v", res.Ambiguous, res.Potential)
	}
}

func TestExpand_CaseInsensitive(t *testing.T) {
	// lowercase candidate still expands — the vowel-poor heuristic catches
	// "bmnn" (0 vowels), then matches "BMNN" in the dict case-insensitively.
	// ("ubnd" itself is NOT a candidate: u is a vowel, ratio 0.25 >= 0.20.)
	res := Expand("theo bmnn tỉnh", []*types.Abbreviation{
		active("BMNN", "Bộ Nông nghiệp"),
	})
	if res.Expanded != "theo Bộ Nông nghiệp tỉnh" {
		t.Errorf("expanded = %q", res.Expanded)
	}
}

func TestExpand_AmbiguousNotExpanded(t *testing.T) {
	res := Expand("BCH đã họp", []*types.Abbreviation{
		active("BCH", "Ban chấp hành"),
		active("BCH", "Bệnh viện C Hòa"),
	})
	if res.Expanded != "BCH đã họp" {
		t.Errorf("ambiguous short form must not be expanded: %q", res.Expanded)
	}
	meanings, ok := res.Ambiguous["BCH"]
	if !ok || len(meanings) != 2 {
		t.Fatalf("ambiguous = %+v", res.Ambiguous)
	}
	if len(res.Applied) != 0 {
		t.Errorf("applied = %+v", res.Applied)
	}
}

func TestExpand_UnknownCandidate(t *testing.T) {
	res := Expand("XYZABC phải tuân thủ", nil)
	if res.Expanded != "XYZABC phải tuân thủ" {
		t.Errorf("expanded = %q", res.Expanded)
	}
	if len(res.Potential) != 1 || res.Potential[0] != "XYZABC" {
		t.Errorf("potential = %+v", res.Potential)
	}
}

func TestExpand_GuardsDocumentNumbers(t *testing.T) {
	// "172/GM-UBND": UBND sits between '-' and end — must not expand;
	// GM sits between '/' and '-' — must not expand.
	res := Expand("theo NĐ 172/GM-UBND thì UBND phải", []*types.Abbreviation{
		active("UBND", "Ủy ban nhân dân"),
		active("GM", "Giám đốc"),
		active("NĐ", "Nghị định"),
	})
	want := "theo Nghị định 172/GM-UBND thì Ủy ban nhân dân phải"
	if res.Expanded != want {
		t.Errorf("expanded = %q, want %q", res.Expanded, want)
	}
}

func TestExpand_NoPartialWordMatch(t *testing.T) {
	// "UBNDX" contains UBND but is one word token — no expansion inside it.
	res := Expand("mã UBNDX nội bộ", []*types.Abbreviation{
		active("UBND", "Ủy ban nhân dân"),
	})
	if res.Expanded != "mã UBNDX nội bộ" {
		t.Errorf("expanded = %q", res.Expanded)
	}
}

func TestExpand_StopWordNotCandidate(t *testing.T) {
	res := Expand("và một số nơi khác", []*types.Abbreviation{
		active("và", "và"),
	})
	if len(res.Applied) != 0 {
		t.Errorf("stop words must never expand: %+v", res.Applied)
	}
}

func TestExpand_EmptyAndNoDict(t *testing.T) {
	res := Expand("", []*types.Abbreviation{active("A", "B")})
	if res.Expanded != "" {
		t.Errorf("expanded = %q", res.Expanded)
	}
	res = Expand("UBND", nil)
	if res.Expanded != "UBND" {
		t.Errorf("expanded = %q", res.Expanded)
	}
}
