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
		"2. Thay thế": {{Document: nd, Article: "34", Khoan: "2", Diem: "a", Phrase: true, From: "cấp huyện", To: "cấp xã"}},
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

// Sentences from the dev KB (review 2026-10-19) and the shapes around them.
func TestPhraseAmendsClauses(t *testing.T) {
	none := func() string { return "" }
	cases := []struct {
		text string
		want []Amend
	}{
		// a ";" ends the provisions: khoản 14 Điều 3 is repealed, not rewritten
		{`Sửa đổi cụm từ "Thanh tra" thành "Kiểm tra" tại khoản 26 Điều 2; bãi bỏ khoản 14 Điều 3 của Nghị định số 28/2025/NĐ-CP.`,
			[]Amend{{Article: "2", Khoan: "26", Phrase: true, From: "Thanh tra", To: "Kiểm tra"}}},
		{`Thay thế cụm từ “A” bằng cụm từ “B” tại Điều 3; bãi bỏ cụm từ “C” tại Điều 4 Luật số 12/2020/QH14`,
			[]Amend{{Article: "3", Phrase: true, From: "A", To: "B"}, {Document: "Luật số 12/2020/QH14", Article: "4", Phrase: true, From: "C"}}},
		// each verb reads its own phrases
		{`Bãi bỏ cụm từ "Thanh tra,", sửa đổi cụm từ "thanh tra chuyên ngành" thành "kiểm tra chuyên ngành" tại khoản 38 Điều 2; bãi bỏ khoản 7 Điều 3`,
			[]Amend{{Article: "2", Khoan: "38", Phrase: true, From: "thanh tra chuyên ngành", To: "kiểm tra chuyên ngành"}}},
		// a clause going on with the same verb
		{`Thay thế từ “huyện” bằng từ “xã” tại khoản 2 Điều 6; cụm từ “pháp lệnh” bằng cụm từ “pháp luật” tại điểm a khoản 1 Điều 28;`,
			[]Amend{{Article: "6", Khoan: "2", Phrase: true, From: "huyện", To: "xã"}, {Article: "28", Khoan: "1", Diem: "a", Phrase: true, From: "pháp lệnh", To: "pháp luật"}}},
		// exceptions are not targets
		{`Thay thế cụm từ “A” bằng cụm từ “B” tại Điều 3, trừ trường hợp quy định tại khoản 2 Điều 9`, nil},
		{`Thay thế cụm từ “A” bằng cụm từ “B” tại khoản 2 Điều 9 trừ điểm a`, nil},
		// ";" inside a provision list (re-review, Nghị định 53/2024)
		{`Thay thế cụm từ “Bộ Tài nguyên và Môi trường” bằng cụm từ “Bộ Nông nghiệp và Môi trường” tại khoản 2 Điều 4; khoản 6 Điều 5; Điều 74; Điều 75.`,
			[]Amend{
				{Article: "4", Khoan: "2", Phrase: true, From: "Bộ Tài nguyên và Môi trường", To: "Bộ Nông nghiệp và Môi trường"},
				{Article: "5", Khoan: "6", Phrase: true, From: "Bộ Tài nguyên và Môi trường", To: "Bộ Nông nghiệp và Môi trường"},
				{Article: "74", Phrase: true, From: "Bộ Tài nguyên và Môi trường", To: "Bộ Nông nghiệp và Môi trường"},
				{Article: "75", Phrase: true, From: "Bộ Tài nguyên và Môi trường", To: "Bộ Nông nghiệp và Môi trường"},
			}},
		{`Bãi bỏ cụm từ “A” tại các điểm a, b và c khoản 4 Điều 124`,
			[]Amend{{Article: "124", Khoan: "4", Diem: "a", Phrase: true, From: "A"}, {Article: "124", Khoan: "4", Diem: "b", Phrase: true, From: "A"}, {Article: "124", Khoan: "4", Diem: "c", Phrase: true, From: "A"}}},
		{`Thay thế cụm từ “A” bằng cụm từ “B” tại Điều 3, 15 ngày`,
			[]Amend{{Article: "3", Phrase: true, From: "A", To: "B"}}},
		{`Thay thế cụm từ “A” bằng cụm từ “B” tại khoản 3, khoản 6 Điều 5 Luật Phòng, chống ma túy số 73/2021/QH14`,
			[]Amend{{Document: "Luật Phòng, chống ma túy số 73/2021/QH14", Article: "5", Khoan: "3", Phrase: true, From: "A", To: "B"}, {Document: "Luật Phòng, chống ma túy số 73/2021/QH14", Article: "5", Khoan: "6", Phrase: true, From: "A", To: "B"}}},
		{`Thay thế cụm từ “A” bằng cụm từ “B” trong toàn bộ Nghị định, trừ Điều 5`, nil},
		// điểm lists, a stray number after a comma
		{`Thay thế cụm từ “A” bằng cụm từ “B” tại điểm a và c khoản 2 Điều 14`,
			[]Amend{{Article: "14", Khoan: "2", Diem: "a", Phrase: true, From: "A", To: "B"}, {Article: "14", Khoan: "2", Diem: "c", Phrase: true, From: "A", To: "B"}}},
		{`Thay thế cụm từ “A” bằng cụm từ “B” tại khoản 2 Điều 3, Điều 4 kể từ ngày 01 tháng 7 năm 2026, 15 ngày`,
			[]Amend{{Article: "3", Khoan: "2", Phrase: true, From: "A", To: "B"}, {Article: "4", Phrase: true, From: "A", To: "B"}}},
		// the document after the provisions, with a title or none
		{`Thay thế cụm từ “A” bằng cụm từ “B” tại Điều 56 của Luật Xây dựng số 50/2014/QH13 đã được sửa đổi, bổ sung theo Luật số 03/2016/QH14`,
			[]Amend{{Document: "Luật Xây dựng số 50/2014/QH13", Article: "56", Phrase: true, From: "A", To: "B"}}},
		{`Thay thế cụm từ “A” bằng cụm từ “B” tại Điều 3 Luật Quản lý thuế ngày 13 tháng 6 năm 2019`,
			[]Amend{{Document: "Luật Quản lý thuế", Article: "3", Phrase: true, From: "A", To: "B"}}},
		// the amending document's own provisions
		{`Thay thế cụm từ “A” bằng cụm từ “B” tại Điều 5 Nghị định này`, nil},
	}
	for _, c := range cases {
		if got := parsePhraseAmends(c.text, none); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s\n got %+v\nwant %+v", c.text, got, c.want)
		}
	}
}
