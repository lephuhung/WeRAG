package vbpl

import (
	"reflect"
	"testing"
)

func TestPhraseAmends(t *testing.T) {
	d := convert(t, header+`
<p id="a1" class="prov-article">Điều 1. Sửa đổi, bổ sung một số điều của Nghị định số 68/2026/NĐ-CP ngày 05 tháng 3 năm 2026 của Chính phủ quy định về chính sách thuế</p>
<p id="k1" class="prov-clause">1. Sửa đổi cụm từ “500 triệu đồng” thành “01 tỷ đồng” tại Điều 3, Điều 4, khoản 1 Điều 8, khoản 1 và khoản 2 Điều 12 Nghị định số 68/2026/NĐ-CP.</p>
<p id="k2" class="prov-clause">2. Thay thế cụm từ “cấp huyện” bằng cụm từ “cấp xã” tại điểm a khoản 2 Điều 34;</p>
<p id="k3" class="prov-clause">3. Thay thế cụm từ “Bộ Lao động - Thương binh và Xã hội” tại khoản 12 Điều 73 bằng cụm từ “Bộ Nội vụ”;</p>
<p id="k4" class="prov-clause">4. Bãi bỏ cụm từ “cấp huyện,” tại khoản 1 Điều 11.</p>
<p id="k5" class="prov-clause">5. Thay thế cụm từ “Tổng Cục trưởng” bằng cụm từ “Cục trưởng” tại Điều 14 Thông tư số 09/2015/TT-BGTVT.</p>
<p id="k6" class="prov-clause">6. Bãi bỏ cụm từ “(kèm theo hợp đồng)” tại Phụ lục XIX.</p>
<p id="k7" class="prov-clause">7. Thay thế cụm từ “A” bằng cụm từ “B” tại các khoản 1, 2 và khoản 5 Điều 9, Điều 10. Thay cụm từ “C” bằng cụm từ “D” tại khoản 3, khoản 6 Điều 55.</p>
<p id="a2" class="prov-article">Điều 2. Hiệu lực thi hành</p>`+signature)
	nd := "Nghị định số 68/2026/NĐ-CP"
	want := map[string][]Amend{
		"1. Sửa đổi cụm từ": {
			{Document: nd, Article: "3", Phrase: true, From: "500 triệu đồng", To: "01 tỷ đồng"},
			{Document: nd, Article: "4", Phrase: true, From: "500 triệu đồng", To: "01 tỷ đồng"},
			{Document: nd, Article: "8", Khoan: "1", Phrase: true, From: "500 triệu đồng", To: "01 tỷ đồng"},
			{Document: nd, Article: "12", Khoan: "1", Phrase: true, From: "500 triệu đồng", To: "01 tỷ đồng"},
			{Document: nd, Article: "12", Khoan: "2", Phrase: true, From: "500 triệu đồng", To: "01 tỷ đồng"},
		},
		"2. Thay thế": {{Document: nd, Article: "34", Khoan: "2", Phrase: true, From: "cấp huyện", To: "cấp xã"}},
		"3. Thay thế": {{Document: nd, Article: "73", Khoan: "12", Phrase: true, From: "Bộ Lao động - Thương binh và Xã hội", To: "Bộ Nội vụ"}},
		"4. Bãi bỏ":   {{Document: nd, Article: "11", Khoan: "1", Phrase: true, From: "cấp huyện,"}},
		"5. Thay thế": {{Document: "Thông tư số 09/2015/TT-BGTVT", Article: "14", Phrase: true, From: "Tổng Cục trưởng", To: "Cục trưởng"}},
		"6. Bãi bỏ":   nil,
		"7. Thay thế": {
			{Document: nd, Article: "9", Khoan: "1", Phrase: true, From: "A", To: "B"},
			{Document: nd, Article: "9", Khoan: "2", Phrase: true, From: "A", To: "B"},
			{Document: nd, Article: "9", Khoan: "5", Phrase: true, From: "A", To: "B"},
			{Document: nd, Article: "10", Phrase: true, From: "A", To: "B"},
			{Document: nd, Article: "55", Khoan: "3", Phrase: true, From: "C", To: "D"},
			{Document: nd, Article: "55", Khoan: "6", Phrase: true, From: "C", To: "D"},
		},
	}
	for prefix, w := range want {
		b := findBlock(d, KindClause, prefix)
		if b == nil {
			t.Fatalf("%q: no block", prefix)
		}
		if !reflect.DeepEqual(b.Phrases, w) {
			t.Errorf("%q: phrases = %+v, want %+v", prefix, b.Phrases, w)
		}
	}

	chunks := Plan(d, 2500)
	for _, c := range chunks {
		if c.ArticleNo != "1" {
			continue
		}
		m := c.Legal()
		if len(m.Amends) != 15 || !m.Amends[0].Phrase || m.Amends[0].From != "500 triệu đồng" {
			t.Fatalf("Điều 1 amends: %+v", m.Amends)
		}
		return
	}
	t.Fatal("no Điều 1 chunk")
}

func TestPhraseAmendsIgnoreOtherText(t *testing.T) {
	d := convert(t, header+`
<p id="a1" class="prov-article">Điều 5. Trách nhiệm</p>
<p id="k1" class="prov-clause">1. Cụm từ “hộ kinh doanh” trong Nghị định này được hiểu như sau.</p>
<p id="k2" class="prov-clause">2. Bổ sung cụm từ “Chủ tịch” vào trước cụm từ “Ủy ban nhân dân cấp tỉnh” tại khoản 2 Điều 11.</p>`+signature)
	for _, prefix := range []string{"1. Cụm từ", "2. Bổ sung"} {
		if b := findBlock(d, KindClause, prefix); b == nil || len(b.Phrases) != 0 {
			t.Errorf("%q: phrases = %+v", prefix, b)
		}
	}
}

func TestPhraseAmendsInChunk(t *testing.T) {
	content := "#### Điều 1. Sửa đổi, bổ sung một số điều của Nghị định số 68/2026/NĐ-CP\n\n" +
		"1. Sửa đổi cụm từ “500 triệu đồng” thành “01 tỷ đồng” tại Điều 3, khoản 1 Điều 8.\n\n" +
		"2. Sửa đổi, bổ sung khoản 5 Điều 8 như sau:\n\n> \"5. Thay thế cụm từ “A” bằng cụm từ “B” tại Điều 9.\""
	path := []string{"Điều 1. Sửa đổi, bổ sung một số điều của Nghị định số 68/2026/NĐ-CP ngày 05 tháng 3 năm 2026 của Chính phủ…"}
	got := PhraseAmendsInChunk(content, path, "Nghị định số 141-2026-NĐ-CP sửa đổi Nghị định số 68-2026-NĐ-CP và Nghị định 320/2025/NĐ-CP")
	nd := "Nghị định số 68/2026/NĐ-CP"
	want := []Amend{
		{Document: nd, Article: "3", Phrase: true, From: "500 triệu đồng", To: "01 tỷ đồng"},
		{Document: nd, Article: "8", Khoan: "1", Phrase: true, From: "500 triệu đồng", To: "01 tỷ đồng"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}
