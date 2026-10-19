package service

import (
	"testing"
	"time"
)

func TestApplyPhraseChanges(t *testing.T) {
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	nd141 := legalHistoryDoc{Number: "141/2026/NĐ-CP", Type: "Nghị định", EffFrom: "2026-01-01"}
	content := "#### Điều 3. Thuế giá trị gia tăng\n\n1. Hộ kinh doanh có mức doanh thu năm từ 500 triệu đồng trở xuống thuộc đối tượng không chịu thuế.\n\n2. Hộ kinh doanh có mức doanh thu năm trên 500 triệu đồng thuộc đối tượng chịu thuế."
	got := applyPhraseChanges(content, []legalPhraseChange{{From: "500 triệu đồng", To: "01 tỷ đồng", Amender: nd141}}, now)
	want := "#### Điều 3. Thuế giá trị gia tăng\n\n1. Hộ kinh doanh có mức doanh thu năm từ 01 tỷ đồng [thay cho “500 triệu đồng” theo Nghị định 141/2026/NĐ-CP, từ 01/01/2026] trở xuống thuộc đối tượng không chịu thuế.\n\n2. Hộ kinh doanh có mức doanh thu năm trên 01 tỷ đồng [thay cho “500 triệu đồng” theo Nghị định 141/2026/NĐ-CP, từ 01/01/2026] thuộc đối tượng chịu thuế."
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}

	// khoản-scoped, pending, removal
	future := legalHistoryDoc{Number: "9/2027/NĐ-CP", Type: "Nghị định", EffFrom: "2027-01-01"}
	got = applyPhraseChanges(content, []legalPhraseChange{
		{From: "500 triệu đồng", To: "02 tỷ đồng", Khoan: "2", Amender: future},
		{From: "Hộ kinh doanh có", Khoan: "1", Amender: nd141},
	}, now)
	want = "#### Điều 3. Thuế giá trị gia tăng\n\n1. [cụm từ “Hộ kinh doanh có” đã bị bãi bỏ theo Nghị định 141/2026/NĐ-CP, từ 01/01/2026] mức doanh thu năm từ 500 triệu đồng trở xuống thuộc đối tượng không chịu thuế.\n\n2. Hộ kinh doanh có mức doanh thu năm trên 500 triệu đồng [sẽ được thay bằng “02 tỷ đồng” theo Nghị định 9/2027/NĐ-CP, từ 01/01/2027] thuộc đối tượng chịu thuế."
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}
