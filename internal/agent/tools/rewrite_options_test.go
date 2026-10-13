package tools

import "testing"

func TestRequestedVersionCount(t *testing.T) {
	cases := map[string]int{
		"Đề xuất 2 phương án viết lại đoạn này": 2,
		"cho tôi ba cách viết khác":             3,
		"gợi ý vài phương án":                   2,
		"viết lại 5 phương án":                  maxRewriteVariants,
		"Viết lại đoạn này cho trang trọng hơn": 0,
		"tóm tắt văn bản này thành một bản":     0,
		"sửa 2 lỗi trong văn bản":               0,
	}
	for q, want := range cases {
		if got := requestedVersionCount(q); got != want {
			t.Errorf("requestedVersionCount(%q) = %d, want %d", q, got, want)
		}
	}
}
