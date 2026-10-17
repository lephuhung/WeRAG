package types

import (
	"errors"
	"testing"
)

func TestParseTemplateFontSizes(t *testing.T) {
	f, err := ParseTemplateFontSizes([]byte(`{"tieu_ngu":13,"quoc_hieu":12}`))
	if err != nil || f["tieu_ngu"] != 13 || f["quoc_hieu"] != 12 {
		t.Fatalf("got %v, %v", f, err)
	}
	for _, raw := range []string{`{"tieu_ngu":12}`, `{"quoc_hieu":14}`, `{"so_ky_hieu":13}`, `{"le_trai":25}`, `{"tieu_ngu":"14"}`} {
		if _, err := ParseTemplateFontSizes([]byte(raw)); !errors.Is(err, ErrTemplateFontSizesInvalid) {
			t.Errorf("%s: want invalid, got %v", raw, err)
		}
	}
	if f, err := ParseTemplateFontSizes(nil); err != nil || len(f) != 0 {
		t.Errorf("empty: got %v, %v", f, err)
	}
}

func TestResolveTemplateFontSizes(t *testing.T) {
	got := ResolveTemplateFontSizes(TemplateFontSizes{"tieu_ngu": 13, "quoc_hieu": 12}, TemplateFontSizes{"quoc_hieu": 13})
	if got["tieu_ngu"] != 13 || got["quoc_hieu"] != 13 || got["co_quan_ban_hanh"] != 13 || got["noi_dung"] != 14 {
		t.Fatalf("got %v", got)
	}
	if len(got) != len(TemplateFontSizeSpecs) {
		t.Fatalf("want every component, got %d", len(got))
	}
	for _, s := range TemplateFontSizeSpecs {
		if s.Default < s.Min || s.Default > s.Max {
			t.Errorf("%s default %d outside %d–%d", s.Key, s.Default, s.Min, s.Max)
		}
	}
}
