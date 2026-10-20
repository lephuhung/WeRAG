package service

import (
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

func TestMentionsDoc(t *testing.T) {
	answer := "Theo Nghị định 102/2026/NĐ-CP sửa đổi Nghị định số 75/2019/NĐ-CP"
	for num, want := range map[string]bool{
		"75/2019/NĐ-CP": true, "102/2026/NĐ-CP": true, "5/2019/NĐ-CP": false, "175/2019/NĐ-CP": false,
	} {
		if got := mentionsDoc(answer, num); got != want {
			t.Errorf("mentionsDoc(%q) = %v, want %v", num, got, want)
		}
	}
}

func TestProvisionText(t *testing.T) {
	content := "#### Điều 22. Vi phạm quy định về cung cấp thông tin\n\n" +
		"1. Phạt cảnh cáo đối với hành vi chậm cung cấp.\n\n" +
		"2. Phạt tiền từ 10.000.000 đồng đến 20.000.000 đồng đối với bên bị điều tra.\n\n" +
		"3. Biện pháp khắc phục hậu quả."
	if got := provisionText(content, "2"); got != "Phạt tiền từ 10.000.000 đồng đến 20.000.000 đồng đối với bên bị điều tra." {
		t.Errorf("khoản 2 = %q", got)
	}
	if got := provisionText(content, "9"); got != "" {
		t.Errorf("missing khoản = %q", got)
	}
}

func TestQuotedProvision(t *testing.T) {
	content := "1. Sửa đổi khoản 2 Điều 22 như sau:\n\n> “2. Phạt tiền từ 20.000.000 đồng đến 30.000.000 đồng đối với bên bị điều tra.”\n\n" +
		"2. Bổ sung khoản 5 như sau:\n\n> “5. Khoản mới.”"
	if got := quotedProvision(content, "2"); got != "Phạt tiền từ 20.000.000 đồng đến 30.000.000 đồng đối với bên bị điều tra." {
		t.Errorf("quoted khoản 2 = %q", got)
	}
	if got := quotedProvision(content, "5"); got != "Khoản mới." {
		t.Errorf("quoted khoản 5 = %q", got)
	}
}

func TestLegalHistoryEffText(t *testing.T) {
	loc := time.FixedZone("ICT", 7*3600)
	h := &legalHistory{now: time.Date(2026, 10, 13, 9, 0, 0, 0, loc)}
	if got := h.effText("2026-05-20"); got != ", có hiệu lực từ 20/05/2026" {
		t.Errorf("past = %q", got)
	}
	if got := h.effText("2027-01-01"); got != ", sẽ có hiệu lực từ 01/01/2027" {
		t.Errorf("future = %q", got)
	}
	if got := h.effText(""); got != "" {
		t.Errorf("empty = %q", got)
	}
	if got := h.dateText(legalHistoryDoc{IssueDate: "2025-12-10"}); got != ", ban hành ngày 10/12/2025" {
		t.Errorf("issue-date fallback = %q", got)
	}
}

func TestAnswerUsesProvision(t *testing.T) {
	answer := "Theo khoản 2 Điều 22, bên bị điều tra bị phạt tiền từ 20.000.000 đồng đến 30.000.000 đồng."
	cases := []struct {
		article, khoan, text string
		want                 bool
	}{
		{"22", "2", "Phạt tiền từ 20.000.000 đồng đến 30.000.000 đồng", true},
		{"22", "3", "Phạt tiền từ 40.000.000 đồng đến 50.000.000 đồng", false},
		{"22", "5", "Phạt tiền từ 30.000.000 đồng đến 50.000.000 đồng", false}, // one shared bound
		{"22", "4", "Biện pháp khắc phục hậu quả", false},                      // no figure, khoản 4 not named
		{"2", "", "Đối tượng áp dụng", false},                                  // "Điều 22" is not Điều 2
		{"7", "4", "Phạt tiền từ 600.000 đồng", false},
		{"14", "1", "phổ cập cho trẻ em từ 03 tuổi", false},
		{"22", "3", "Phạt tiền từ 30.000.000 đồng đến 50.000.000 đồng đối với bên bị điều tra trong vụ việc cạnh tranh", false},
	}
	for _, c := range cases {
		if got := answerUsesProvision(answer, c.article, c.khoan, c.text); got != c.want {
			t.Errorf("article %s khoản %s: got %v want %v", c.article, c.khoan, got, c.want)
		}
	}
}

func TestAnswerUsesProvisionLeadingZero(t *testing.T) {
	if !answerUsesProvision("phổ cập giáo dục mầm non cho trẻ em từ 3 tuổi đến 6 tuổi", "14", "1", "từ 03 tuổi đến 06 tuổi") {
		t.Fatal("03 tuổi should match 3 tuổi")
	}
}

func TestAnswerUsesProvisionByWording(t *testing.T) {
	answer := "Theo luật sửa đổi, giáo dục tiểu học và giáo dục trung học cơ sở là giáo dục bắt buộc."
	if !answerUsesProvision(answer, "14", "1", "Giáo dục tiểu học và giáo dục trung học cơ sở là giáo dục bắt buộc. Nhà nước thực hiện phổ cập") {
		t.Fatal("shared wording should count")
	}
	if answerUsesProvision(answer, "14", "2", "Nhà nước chịu trách nhiệm thực hiện giáo dục bắt buộc trong cả nước") {
		t.Fatal("a few common words must not count")
	}
}

func TestLegalTitleBase(t *testing.T) {
	for in, want := range map[string]string{
		"Luật Giáo dục số 43/2019/QH14":          "luật giáo dục",
		"Luật GIÁO DỤC ĐẠI HỌC số 125/2025/QH15": "luật giáo dục đại học",
		"Nghị định  số 75/2019/NĐ-CP":            "nghị định",
		"Bộ luật Lao động":                       "bộ luật lao động",
	} {
		if got := LegalTitleBase(in, ""); got != want {
			t.Errorf("LegalTitleBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnswerUsesProvisionWordingDespiteAges(t *testing.T) {
	answer := "khoản 1 quy định: \"Giáo dục tiểu học và giáo dục trung học cơ sở là giáo dục bắt buộc.\""
	text := "Giáo dục tiểu học và giáo dục trung học cơ sở là giáo dục bắt buộc. Nhà nước thực hiện phổ cập giáo dục mầm non cho trẻ em từ 03 tuổi đến 06 tuổi."
	if !answerUsesProvision(answer, "14", "1", text) {
		t.Fatal("wording should count when only ages differ")
	}
}

func TestLegalHistoryNamesByCitation(t *testing.T) {
	h := &legalHistory{answer: `Theo Luật Giáo dục 2019 <kb doc="x" chunk_id="c-1" kb_id="k" />.`}
	if !h.names("43/2019/QH14", "c-1") {
		t.Fatal("a cited chunk names its document")
	}
	if h.names("43/2019/QH14", "c-2") || h.names("43/2019/QH14", "") {
		t.Fatal("an uncited chunk does not")
	}
}

func TestAnswerUsesPhrase(t *testing.T) {
	cases := []struct {
		answer, phrase string
		want           bool
	}{
		{"doanh thu năm từ 500 triệu đồng trở xuống", "500 triệu đồng", true},
		{"doanh thu năm từ 500  Triệu đồng trở xuống", "500 triệu đồng", true},
		{"doanh thu 1.500 triệu đồng", "500 triệu đồng", false},
		{"ngưỡng 1 tỷ đồng", "01 tỷ đồng", true},
		{"ngưỡng 11 tỷ đồng", "01 tỷ đồng", false},
		{"Ủy ban nhân dân cấp huyện", "cấp huyện", true},
		{"cấp huyện,", "cấp huyện,", true},
		{"", "500 triệu đồng", false},
		{"ab", "ab", false},
	}
	for _, c := range cases {
		if got := answerUsesPhrase(c.answer, c.phrase); got != c.want {
			t.Errorf("answerUsesPhrase(%q, %q) = %v", c.answer, c.phrase, got)
		}
	}
}

func TestPhraseLines(t *testing.T) {
	h := &legalHistory{phrases: map[string]*phraseChange{}, now: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)}
	old := legalHistoryDoc{Number: "68/2026/NĐ-CP", Type: "Nghị định"}
	amender := legalHistoryDoc{Number: "141/2026/NĐ-CP", Type: "Nghị định", EffFrom: "2026-01-01"}
	for _, art := range []string{"3", "4", "3", "8", "9", "10"} {
		h.addPhrase(old, amender, vietnamese_legal.AmendRef{Article: art, Phrase: true, From: "500 triệu đồng", To: "01 tỷ đồng"})
	}
	h.addPhrase(old, amender, vietnamese_legal.AmendRef{Article: "11", Khoan: "1", Phrase: true, From: "cấp huyện,"})
	lines := h.phraseLines()
	want := []string{
		"Điều 3, Điều 4, Điều 8, Điều 9, … Nghị định 68/2026/NĐ-CP: cụm từ “500 triệu đồng” được thay bằng “01 tỷ đồng” bởi Nghị định 141/2026/NĐ-CP, có hiệu lực từ 01/01/2026.",
		"khoản 1 Điều 11 Nghị định 68/2026/NĐ-CP: cụm từ “cấp huyện,” được bãi bỏ bởi Nghị định 141/2026/NĐ-CP, có hiệu lực từ 01/01/2026.",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %+v", lines)
	}
	for i := range want {
		if lines[i].text != want[i] {
			t.Errorf("line %d = %q\nwant %q", i, lines[i].text, want[i])
		}
	}
}
