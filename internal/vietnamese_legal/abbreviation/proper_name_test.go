package abbreviation

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestInspectKeepsProperNamesAsTyped(t *testing.T) {
	active := []*types.Abbreviation{
		{ID: "1", ShortForm: "PV01", FullForm: "PV01", IsActive: true, IsProperName: true},
		{ID: "2", ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
		{ID: "3", ShortForm: "PA05", FullForm: "PA05", IsActive: false, IsProperName: true},
	}
	q := "Gửi PV01 và UBND, PA05"
	det := types.AbbreviationDetection{Detector: "clef", Keys: []string{"pv01", "ubnd", "pa05"}}
	r := InspectDetected(q, active, det)
	if len(r.UnknownTerms) != 1 || r.UnknownTerms[0] != "PA05" {
		t.Fatalf("an inactive proper name is still asked about, the active one not: %v", r.UnknownTerms)
	}
	for _, term := range r.Terms {
		if term.ShortForm == "PV01" {
			t.Fatalf("PV01 became a term: %+v", term)
		}
	}
	active[2].IsActive = true
	r = InspectDetected(q, active, det)
	out, err := RenderResolvedQuery(r)
	if err != nil || r.Status != types.AbbreviationStatusReady || out != "Gửi PV01 và Ủy ban nhân dân (UBND), PA05" {
		t.Fatalf("status=%s out=%q err=%v", r.Status, out, err)
	}
}

func TestParseKeepReplies(t *testing.T) {
	for text, keep := range map[string]bool{
		"giữ nguyên":               true,
		"là tên riêng, giữ nguyên": true,
		"Đây là tên đơn vị":        true,
		"không phải viết tắt":      true,
		"PV01 là tên phòng":        true,
		"PV01 = mã đơn vị":         true,
		"PV01 = Phòng Tham mưu":    false, // a meaning
		"Phòng Tham mưu":           false,
		"giữ nguyên nhưng hãy tìm giúp tôi tất cả các công văn gửi phòng này trong năm 2026": false, // a new question
		"quy định về giữ nguyên hiện trạng đất là gì?":                                       false,
		"tên đơn vị nào ký công văn này?":                                                    false,
		"Giữ nguyên nhé": true,
		"giữ nguyên.":    true,
	} {
		got, err := ParseUserDefinitions(text, "m1", []string{"PV01"}, true)
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		kept := len(got.Typos) == 1 && got.Typos[0] == (types.AbbreviationTypo{Wrong: "PV01", Correct: "PV01"}) && len(got.Definitions) == 0
		if kept != keep {
			t.Errorf("%q: kept=%v (typos %v, definitions %v)", text, kept, got.Typos, got.Definitions)
		}
	}
}

// A meaning activated after someone kept the word as typed wins.
func TestProperNameYieldsToAMeaning(t *testing.T) {
	active := []*types.Abbreviation{
		{ID: "1", ShortForm: "BHXH", FullForm: "BHXH", IsActive: true, IsProperName: true},
		{ID: "2", ShortForm: "BHXH", FullForm: "Bảo hiểm xã hội", IsActive: true},
	}
	r := InspectDetected("Mức đóng BHXH", active, types.AbbreviationDetection{Detector: "clef", Keys: []string{"bhxh"}})
	out, err := RenderResolvedQuery(r)
	if err != nil || out != "Mức đóng Bảo hiểm xã hội (BHXH)" {
		t.Fatalf("out=%q err=%v status=%s", out, err, r.Status)
	}
}
