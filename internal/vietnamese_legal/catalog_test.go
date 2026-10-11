package vietnamese_legal

import "testing"

func TestCatalogNumberKey(t *testing.T) {
	want := "30/2020/ND-CP"
	for _, in := range []string{"30/2020/NĐ-CP", "30/2020/nđ-cp", " 30 / 2020 / ND–CP ", "30/2020/ND-CP"} {
		if got := CatalogNumberKey(in); got != want {
			t.Errorf("CatalogNumberKey(%q) = %q, want %q", in, got, want)
		}
	}
}
