package answerdocx

import (
	"strings"
	"testing"
)

func TestCitationsInOrderOfFirstUse(t *testing.T) {
	md := `Theo Điều 6 <kb doc="Nghị định số 80-2021-NĐ-CP.html" chunk_id="c-2" kb_id="kb-1" /> và Điều 4 ` +
		`<kb doc="Luật 04-2017-QH14 &amp; sửa đổi.html" chunk_id="c-1" kb_id="kb-1" />; lại Điều 6 <kb doc="Nghị định số 80-2021-NĐ-CP.html" chunk_id="c-2" kb_id="kb-1" />.` +
		` Xem <web url="https://chinhphu.vn/a?x=1&amp;y=2" title="Cổng &quot;Chính phủ&quot;" /> <ref id="c9"/> </kb>`
	got := Citations(md)
	want := []Citation{
		{ChunkID: "c-2", KBID: "kb-1", Doc: "Nghị định số 80-2021-NĐ-CP.html"},
		{ChunkID: "c-1", KBID: "kb-1", Doc: "Luật 04-2017-QH14 & sửa đổi.html"},
		{URL: "https://chinhphu.vn/a?x=1&y=2", Title: `Cổng "Chính phủ"`},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("citation %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	inCode := "`<kb doc=\"a\" chunk_id=\"c-code\" />`\n\n```\n<web url=\"https://code.vn\" />\n```\n" +
		`<kb doc="b" chunk_id="c-real" url="https://kb-url.vn" title="t" />`
	if got := Citations(inCode); len(got) != 1 || got[0] != (Citation{ChunkID: "c-real", Doc: "b"}) {
		t.Errorf("citations with code = %+v", got)
	}
	if Citations("Không có trích dẫn.") != nil {
		t.Error("citations in plain text")
	}
}

func TestDisplayTitle(t *testing.T) {
	cut := "Nghị định số 45-2024-NĐ-CP Sửa đổi, bổ sung một số điều của Nghị định số 39-2019-NĐ-CP ngày 10 tháng 5 năm 2019 của Chính phủ về tổ chức và hoạt động của Quỹ Phát triển doanh nghiệ.html"
	cutAtSpace := "Thông tư số 04-2018-TT-BNV Hướng dẫn về thẩm quyền, thủ tục bổ nhiệm, bổ nhiệm lại, miễn nhiệm, thay thế và phụ cấp trách nhiệm công việc của kế toán trưởng, phụ trách kế toán của .html"
	cases := map[string]string{
		"Nghị định số 80-2021-NĐ-CP Quy định chi tiết.html": "Nghị định số 80-2021-NĐ-CP Quy định chi tiết",
		"Báo cáo quý 3.PDF": "Báo cáo quý 3",
		"Hướng dẫn nội bộ":  "Hướng dẫn nội bộ",
		"  bảng.xlsx ":      "bảng",
		"v1.2 ghi chú":      "v1.2 ghi chú",
		cut:                 "Nghị định số 45-2024-NĐ-CP Sửa đổi, bổ sung một số điều của Nghị định số 39-2019-NĐ-CP ngày 10 tháng 5 năm 2019 của Chính phủ về tổ chức và hoạt động của Quỹ Phát triển doanh…",
		cutAtSpace:          strings.TrimSuffix(cutAtSpace, " .html") + "…",
		strings.Repeat("dài ", 60) + "hết.html": strings.Repeat("dài ", 60) + "hết", // longer than the cut: whole
	}
	for in, want := range cases {
		if got := DisplayTitle(in); got != want {
			t.Errorf("DisplayTitle(%.50q) = %q, want %q", in, got, want)
		}
	}
}
