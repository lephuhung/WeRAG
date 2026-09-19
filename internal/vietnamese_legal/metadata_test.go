package vietnamese_legal

import (
	"strings"
	"testing"
)

func TestRecoverDocumentNumber_Labelled(t *testing.T) {
	text := "QUỐC HỘI\nCỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM\n\nLuật số: 24/2018/QH14\n\nCăn cứ Hiến pháp...\ntheo Nghị quyết số 203/2025/QH15"
	got := RecoverDocumentNumber(text)
	if got != "24/2018/QH14" {
		t.Errorf("doc number = %q, want 24/2018/QH14", got)
	}
}

func TestRecoverDocumentNumber_Bare(t *testing.T) {
	text := "BỘ CÔNG AN\n\nTHÔNG TƯ\nSố 15/2023/TT-BCA ngày 01/01/2023\n\nCăn cứ Nghị định..."
	got := RecoverDocumentNumber(text)
	if got != "15/2023/TT-BCA" {
		t.Errorf("doc number = %q, want 15/2023/TT-BCA", got)
	}
}

func TestRecoverDocumentNumber_PreambleCut(t *testing.T) {
	// No header number — the only number is in the Căn cứ preamble, which is
	// a citation, not this document's own số hiệu.
	text := "BỘ CÔNG AN\n\nTHÔNG TƯ\nHướng dẫn thi hành\n\nCăn cứ Nghị định số 53/2022/NĐ-CP ngày..."
	got := RecoverDocumentNumber(text)
	if got == "53/2022/NĐ-CP" {
		t.Error("preamble citation wrongly recovered as own số hiệu")
	}
}

func TestRecoverDocumentNumber_Empty(t *testing.T) {
	if got := RecoverDocumentNumber(""); got != "" {
		t.Errorf("empty input = %q", got)
	}
	if got := RecoverDocumentNumber("just text, no number"); got != "" {
		t.Errorf("no number = %q", got)
	}
}

func TestGenerateNumberCandidates(t *testing.T) {
	got := GenerateNumberCandidates("15", []string{"TT-BCA"}, "2023")
	want := map[string]bool{"15/2023/TT-BCA": true, "15/TT-BCA": true}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v", got)
	}
	for _, c := range got {
		if !want[c] {
			t.Errorf("unexpected candidate %q", c)
		}
	}
	// Without year: current-year + previous-year + bare candidates.
	got2 := GenerateNumberCandidates("15", []string{"TT-BCA"}, "")
	if len(got2) != 3 {
		t.Fatalf("yearless candidates = %v", got2)
	}
}

func TestParseReference_FullNumber(t *testing.T) {
	ref := ParseReference("Nghị định 53/2022/NĐ-CP")
	if ref.DocTypeSlug != "nghi_dinh" {
		t.Errorf("doc type = %q", ref.DocTypeSlug)
	}
	if ref.DocumentNumber != "53/2022/NĐ-CP" {
		t.Errorf("number = %q", ref.DocumentNumber)
	}
	if ref.Confidence != "high" {
		t.Errorf("confidence = %q", ref.Confidence)
	}
	if ref.NumberRaw != "53" {
		t.Errorf("number raw = %q", ref.NumberRaw)
	}
}

func TestParseReference_TypeNumberAgency(t *testing.T) {
	ref := ParseReference("Thông tư 15 của Bộ Công an năm 2023")
	if ref.DocTypeSlug != "thong_tu" {
		t.Errorf("doc type = %q", ref.DocTypeSlug)
	}
	if ref.NumberRaw != "15" {
		t.Errorf("number raw = %q", ref.NumberRaw)
	}
	if ref.Year != "2023" {
		t.Errorf("year = %q", ref.Year)
	}
	if ref.IssuingAgencyCode != "BCA" {
		t.Errorf("agency code = %q", ref.IssuingAgencyCode)
	}
	if len(ref.DocNumberCandidates) == 0 || ref.DocNumberCandidates[0] != "15/2023/TT-BCA" {
		t.Errorf("candidates = %v", ref.DocNumberCandidates)
	}
}

func TestParseReference_Section(t *testing.T) {
	ref := ParseReference("Điều 17 Nghị định 53/2022/NĐ-CP")
	if !strings.Contains(ref.SectionReference, "Điều 17") {
		t.Errorf("section ref = %q", ref.SectionReference)
	}
	if ref.DocumentNumber != "53/2022/NĐ-CP" {
		t.Errorf("number = %q", ref.DocumentNumber)
	}
}

func TestParseReference_ActionStrip(t *testing.T) {
	ref := ParseReference("tra cứu Luật 24/2018/QH14")
	if ref.DocTypeSlug != "luat" {
		t.Errorf("doc type = %q", ref.DocTypeSlug)
	}
	if ref.DocumentNumber != "24/2018/QH14" {
		t.Errorf("number = %q", ref.DocumentNumber)
	}
}

func TestParseReference_YearNotNumber(t *testing.T) {
	// A standalone year must not be consumed as the document number.
	ref := ParseReference("Luật năm 2018")
	if ref.Year != "2018" {
		t.Errorf("year = %q", ref.Year)
	}
	if ref.NumberRaw == "2018" {
		t.Error("year wrongly consumed as number")
	}
}

func TestParseReference_LongestTypeMatch(t *testing.T) {
	// "thông tư liên tịch" must win over plain "thông tư".
	ref := ParseReference("Thông tư liên tịch 05/2020/TTLT-BGDĐT-BTC")
	if ref.DocTypeSlug != "thong_tu_lien_tich" {
		t.Errorf("doc type = %q, want thong_tu_lien_tich", ref.DocTypeSlug)
	}
}

func TestNumberTokenPresent(t *testing.T) {
	if !NumberTokenPresent("53/2022/NĐ-CP", "53") {
		t.Error("53 should match in 53/2022/NĐ-CP")
	}
	if NumberTokenPresent("853/2022", "53") {
		t.Error("53 must not match inside 853")
	}
	if NumberTokenPresent("153 abc", "53") {
		t.Error("53 must not match inside 153")
	}
	if NumberTokenPresent("abc 2053", "53") {
		t.Error("53 must not match inside 2053")
	}
}

func TestExtractTitleKeywords(t *testing.T) {
	kw := ExtractTitleKeywords("an ninh mạng của quốc gia")
	// "của" is a stopword; an/ninh/mạng/quốc/gia survive.
	for _, k := range kw {
		if k == "của" {
			t.Error("stopword leaked into keywords")
		}
	}
	if len(kw) != 5 {
		t.Errorf("keywords = %v", kw)
	}
}
