package tools

import "testing"

func TestFormatFixConsent(t *testing.T) {
	yes := []string{"Đồng ý, áp dụng các sửa đó", "sửa luôn giúp tôi", "OK", "Áp dụng ngay", "được, tiến hành đi", "tự sửa giúp tôi luôn", ""}
	no := []string{"Sửa các lỗi thể thức đó giúp tôi", "Kiểm tra thể thức văn bản này", "không áp dụng", "chưa đồng ý, xem lại đã", "văn bản này sai gì?"}
	for _, q := range yes {
		if !formatFixConsent(q) {
			t.Errorf("expected consent for %q", q)
		}
	}
	for _, q := range no {
		if formatFixConsent(q) {
			t.Errorf("expected no consent for %q", q)
		}
	}
}
