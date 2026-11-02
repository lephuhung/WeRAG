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

// TestCatalogNumberKeyLookalikes: the key folds the look-alikes
// FindCitations reads text with (Cyrillic "С" of 12 vbpl rows, any case,
// the non-breaking hyphen), so a number typed in Latin finds them.
func TestCatalogNumberKeyLookalikes(t *testing.T) {
	for in, want := range map[string]string{
		"51/2025/TT-BTС": "51/2025/TT-BTC",
		"07/2025/NĐ-СР":  "07/2025/ND-CP",
		"07/2025/nđ-ср":  "07/2025/ND-CP",
		"30/2020/NĐ‑CP":  "30/2020/ND-CP",
		"30/2020/NÐ-CP":  "30/2020/ND-CP",
	} {
		if got := CatalogNumberKey(in); got != want {
			t.Errorf("CatalogNumberKey(%q) = %q, want %q", in, got, want)
		}
		if got := CatalogNumberKey(CatalogNumberKey(in)); got != want {
			t.Errorf("CatalogNumberKey is not idempotent on %q: %q", in, got)
		}
	}
	if got := FoldNumberLookalikes("02/2000/QÐ-BTC"); got != "02/2000/QĐ-BTC" {
		t.Errorf("FoldNumberLookalikes = %q", got)
	}
}

func TestCatalogTitleKey(t *testing.T) {
	a := CatalogTitleKey("Quyết định số 136/2001/QÐ-BTC Về việc bổ sung danh mục")
	b := CatalogTitleKey("Quyết định số 136/2001/QĐ-BTC về việc bổ sung, danh mục")
	if a != b {
		t.Errorf("title keys differ: %q vs %q", a, b)
	}
	if CatalogTitleKey("Thông tư số 17-TC/TCÐN hướng dẫn cơ chế") == CatalogTitleKey("Thông tư số 17-TC/TCĐN Hướng dẫn việc thu nộp") {
		t.Error("different titles share a key")
	}
	if DocTypeClass("Thông tư liên tịch") != DocTypeClass(" thông  tư ") {
		t.Error("DocTypeClass: liên tịch is its type")
	}
}
