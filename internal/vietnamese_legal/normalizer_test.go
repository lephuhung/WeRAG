package vietnamese_legal

import (
	"strings"
	"testing"
)

func TestFixScatteredVietnamese_NonVietnamese(t *testing.T) {
	text := "Hello world. T h i s  i s  E n g l i s h  spaced text."
	if got := FixScatteredVietnamese(text); got != text {
		t.Errorf("non-Vietnamese text must pass through untouched, got %q", got)
	}
}

func TestFixScatteredVietnamese_RejoinsScatteredLine(t *testing.T) {
	// A line that is ≥50% single-char tokens with VN diacritics rejoins.
	in := "Đ i ề u  1 .  P h ạ m  v i  đ i ề u  c h ỉ n h"
	got := FixScatteredVietnamese(in)
	if !strings.Contains(got, "Điều") {
		t.Errorf("scattered heading not rejoined: %q", got)
	}
	if !strings.Contains(got, "chỉnh") {
		t.Errorf("expected rejoined chỉnh: %q", got)
	}
}

func TestFixScatteredVietnamese_PreservesNormalLine(t *testing.T) {
	in := "Điều 1. Phạm vi điều chỉnh\nLuật này quy định về an ninh mạng."
	got := FixScatteredVietnamese(in)
	if got != in {
		t.Errorf("normal VN text must pass through: %q", got)
	}
}

func TestFixScatteredVietnamese_StripsCongBaoFurniture(t *testing.T) {
	in := "CÔNG BÁO/Số 10-11/Ngày 15-3-2025\nĐiều 1. Phạm vi điều chỉnh"
	got := FixScatteredVietnamese(in)
	if strings.Contains(got, "CÔNG BÁO") {
		t.Errorf("CÔNG BÁO furniture line not stripped: %q", got)
	}
	if !strings.Contains(got, "Điều 1.") {
		t.Errorf("content line lost: %q", got)
	}
}

func TestFixScatteredVietnamese_KeepsCongBaoBodySentence(t *testing.T) {
	// A real body sentence mentioning Công báo must NOT be deleted.
	in := "Công báo/Số 1133 đã đăng toàn văn nội dung luật này cho mọi người biết"
	got := FixScatteredVietnamese(in)
	if !strings.Contains(got, "Công báo/Số 1133") {
		t.Errorf("body sentence wrongly deleted: %q", got)
	}
}

func TestFixScatteredVietnamese_LetterDigitBoundary(t *testing.T) {
	// Uniformly scattered heading: letter↔digit transitions break the glue so
	// "ĐIỀU 5" stays separable instead of becoming "ĐIỀU5".
	in := "Đ I Ề U  5 .  P h ạ m  v i"
	got := FixScatteredVietnamese(in)
	if !strings.Contains(got, "ĐIỀU 5") {
		t.Errorf("letter/digit boundary lost: %q", got)
	}
}

func TestFixScatteredVietnamese_PreservesStructureLines(t *testing.T) {
	in := "| a | b | c |\n|---|---|---|\n| 1 | 2 | 3 |"
	got := FixScatteredVietnamese(in)
	if got != in {
		t.Errorf("table lines must pass through: %q", got)
	}
}

func TestHasBrokenVNTextLayer(t *testing.T) {
	// Corrupt layer: VN base letters (đ in "đnh"/"đc") survive but the
	// complex tone chars (U+1EA0–1EF9: ộ ệ ấ ợ …) are gone.
	broken := strings.Repeat("B lut dân s quy đnh đc lâp t do hnh phúc. ", 10)
	if !HasBrokenVNTextLayer(broken, 0.005) {
		t.Error("broken VN layer (no tone chars) should be detected")
	}
	// Healthy VN text is dense with tone chars.
	healthy := strings.Repeat("Luật này quy định về quyền sở hữu tài sản được bảo vệ. ", 10)
	if HasBrokenVNTextLayer(healthy, 0.005) {
		t.Error("healthy VN text wrongly flagged as broken")
	}
	// Too little text → can't decide.
	if HasBrokenVNTextLayer("Bộ", 0.005) {
		t.Error("tiny sample should return false")
	}
}

func TestPageMarker(t *testing.T) {
	m := PageMarker.FindStringSubmatch("<!-- page 12 -->")
	if len(m) != 2 || m[1] != "12" {
		t.Errorf("page marker = %v", m)
	}
}
