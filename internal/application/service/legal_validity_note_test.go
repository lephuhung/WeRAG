package service

import "testing"

func TestRemarksExpiry(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"Theo Điều 4 Nghị định 27/2015/NĐ-CP, Bộ Ngoại giao có thẩm quyền.", false},
		{"Nghị định 166/2013/NĐ-CP đã hết hiệu lực toàn bộ, bị thay thế bởi Nghị định 296/2025/NĐ-CP.", true},
		{"Thông tư 85/2026/TT-BQP (có hiệu lực từ 01/7/2026, thay thế Thông tư 181/2019/TT-BQP).", true},
	}
	numbers := []string{"27/2015/NĐ-CP", "166/2013/NĐ-CP", "181/2019/TT-BQP"}
	for i, c := range cases {
		if got := remarksExpiry(c.text, numbers[i]); got != c.want {
			t.Errorf("%q: got %v, want %v", c.text, got, c.want)
		}
	}
}
