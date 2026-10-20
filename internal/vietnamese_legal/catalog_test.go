package vietnamese_legal

import (
	"strings"
	"testing"
)

func TestCatalogNumberKey(t *testing.T) {
	want := "30/2020/ND-CP"
	for _, in := range []string{"30/2020/NĐ-CP", "30/2020/nđ-cp", " 30 / 2020 / ND–CP ", "30/2020/ND-CP", "30/2020/NÐ-CP", "30/2020/nð-cp"} {
		if got := CatalogNumberKey(in); got != want {
			t.Errorf("CatalogNumberKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGraphSearchTerms(t *testing.T) {
	got := GraphSearchTerms([]string{"Nghị định 53/2022/nđ-cp", "53/2022/NĐ-CP", "Bộ Công an", " "})
	want := []string{"Nghị định 53/2022/nđ-cp", "53/2022/NĐ-CP", "Bộ Công an"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
}
