package main

import (
	"regexp"
	"testing"
)

// The SQL preselection must let through every lead the Go parser reads,
// "cụm từ:" before the quote included (Fable review: 14 chunks were never
// candidates).
func TestPhraseLeadSQL(t *testing.T) {
	re := regexp.MustCompile(`(?i)` + phraseLeadSQL)
	for _, s := range []string{
		`Thay thế cụm từ “A” bằng cụm từ “B” tại Điều 3`,
		`Thay thế cụm từ: “Phòng Giáo dục và Đào tạo” bằng cụm từ “Ủy ban nhân dân cấp xã”`,
		`Bãi bỏ các từ : "và"`,
	} {
		if !re.MatchString(s) {
			t.Errorf("not a candidate: %s", s)
		}
	}
}
