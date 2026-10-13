package abbreviation

import "testing"

func TestStripMentions(t *testing.T) {
	cases := map[string]string{
		"Chèn vào cuối @thong_bao.docx một đoạn: 'Văn bản này có hiệu lực kể từ ngày ký.'": "Chèn vào cuối một đoạn: 'Văn bản này có hiệu lực kể từ ngày ký.'",
		"Đối chiếu @bao_cao_dai.docx với @ke_hoach_nguon.docx":                             "Đối chiếu với",
		"UBND là gì?":  "UBND là gì?",
		"email a@b.vn": "email a@b.vn",
	}
	for in, want := range cases {
		if got := StripMentions(in); got != want {
			t.Errorf("StripMentions(%q) = %q, want %q", in, got, want)
		}
	}
}
