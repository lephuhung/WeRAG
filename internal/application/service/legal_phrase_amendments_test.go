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

// Review 2026-10-19: whole words only, newest amender, chains, điểm scope,
// unknown dates — and no note is rewritten again.
func TestApplyPhraseChangesReviewCases(t *testing.T) {
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	doc := func(n, eff string) legalHistoryDoc {
		return legalHistoryDoc{Number: n, Type: "Nghị định", EffFrom: eff}
	}
	cases := []struct {
		name, content string
		changes       []legalPhraseChange
		want          string
	}{
		{"whole number", "từ 1.500 triệu đồng và 500 triệu đồng; 125 ngày",
			[]legalPhraseChange{{From: "500 triệu đồng", To: "01 tỷ đồng", Amender: doc("1/2026/NĐ-CP", "2026-01-01")}, {From: "25 ngày", To: "30 ngày", Amender: doc("1/2026/NĐ-CP", "2026-01-01")}},
			"từ 1.500 triệu đồng và 01 tỷ đồng [thay cho “500 triệu đồng” theo Nghị định 1/2026/NĐ-CP, từ 01/01/2026]; 125 ngày"},
		{"newest of two", "trên 500 triệu đồng",
			[]legalPhraseChange{{From: "500 triệu đồng", To: "01 tỷ đồng", Amender: doc("1/2020/NĐ-CP", "2020-01-01")}, {From: "500 triệu đồng", To: "02 tỷ đồng", Amender: doc("2/2024/NĐ-CP", "2024-01-01")}},
			"trên 02 tỷ đồng [thay cho “500 triệu đồng” theo Nghị định 2/2024/NĐ-CP, từ 01/01/2024]"},
		{"chain", "do Tổng cục Thuế quyết định",
			[]legalPhraseChange{{From: "Tổng cục Thuế", To: "Cục Thuế", Amender: doc("1/2025/NĐ-CP", "2025-03-01")}, {From: "Cục Thuế", To: "Bộ Tài chính", Amender: doc("2/2026/NĐ-CP", "2026-02-01")}},
			"do Bộ Tài chính [thay cho “Tổng cục Thuế” theo Nghị định 1/2025/NĐ-CP, từ 01/03/2025; Nghị định 2/2026/NĐ-CP, từ 01/02/2026] quyết định"},
		{"điểm scope", "1. Khoản một:\na) cấp huyện xét;\nb) cấp huyện duyệt.",
			[]legalPhraseChange{{From: "cấp huyện", To: "cấp xã", Khoan: "1", Diem: "b", Amender: doc("1/2025/NĐ-CP", "2025-07-01")}},
			"1. Khoản một:\na) cấp huyện xét;\nb) cấp xã [thay cho “cấp huyện” theo Nghị định 1/2025/NĐ-CP, từ 01/07/2025] duyệt."},
		{"no date", "trên 500 triệu đồng",
			[]legalPhraseChange{{From: "500 triệu đồng", To: "01 tỷ đồng", Amender: legalHistoryDoc{Number: "9/2026/NĐ-CP", Type: "Nghị định"}}},
			"trên 500 triệu đồng [được thay bằng “01 tỷ đồng” theo Nghị định 9/2026/NĐ-CP, chưa rõ ngày hiệu lực]"},
		{"case and spacing", "Trên 500  Triệu đồng",
			[]legalPhraseChange{{From: "500 triệu đồng", To: "01 tỷ đồng", Amender: doc("1/2026/NĐ-CP", "2026-01-01")}},
			"Trên 01 tỷ đồng [thay cho “500  Triệu đồng” theo Nghị định 1/2026/NĐ-CP, từ 01/01/2026]"},
	}
	for _, c := range cases {
		if got := applyPhraseChanges(c.content, c.changes, now); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}
