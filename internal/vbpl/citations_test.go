package vbpl

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

func TestCitationLinks(t *testing.T) {
	d := convert(t, header+`
<p><em>Căn cứ Luật Giáo dục số <a href="/van-ban/chi-tiet/136042" target="_blank">43/2019/QH14</a> được sửa đổi, bổ sung bởi Luật số <a href="/van-ban/chi-tiet/185973">123/2025/QH15</a>;</em></p>
<p>Căn cứ Nghị định số <a href="/van-ban/chi-tiet/1e48fa20-84a7-11f1-8ef2-67037efa0c35">279/2026/NĐ-CP</a> ngày 12 tháng 7 năm 2026;</p>
<p id="a1" class="prov-article">Điều 1. Sửa đổi</p>
<p id="k1" class="prov-clause">1. Bãi bỏ khoản 2 Điều 5 Nghị định 152/2025/NĐ-CP, nay quy định theo Nghị định số <a href="http://vbpl.biznext.vn/van-ban/chi-tiet/181234">152/2025/NĐ-CP</a>.</p>
<p id="k2" class="prov-clause">2. Liên kết rỗng <a href="/van-ban/chi-tiet/999"></a> và liên kết khác <a href="/tin-tuc/1">tin</a> bị bỏ qua.</p>
<p id="a2" class="prov-article">Điều 2. Hiệu lực</p>
<p class="prov-content">Thông tư <a href="/van-ban/chi-tiet/55555?go=abc">01/2020/TT-BCA</a> hết hiệu lực.</p>
`+signature)
	if len(d.Links) != 5 {
		t.Fatalf("links = %+v", d.Links)
	}
	chunks := Plan(d, 2500)
	cits := CitationLinks(d, chunks, "")
	type got struct{ chunk, id, text string }
	var all []got
	for i, cs := range cits {
		runes := []rune(chunks[i].Content)
		for _, c := range cs {
			if string(runes[c.Start:c.End]) != c.Text {
				t.Errorf("chunk %d [%d:%d] = %q, want %q", i, c.Start, c.End, string(runes[c.Start:c.End]), c.Text)
			}
			all = append(all, got{chunks[i].Kind + ":" + chunks[i].ArticleNo, c.VBPLID, c.Text})
		}
	}
	want := []got{
		{"preamble:", "136042", "43/2019/QH14"},
		{"preamble:", "185973", "123/2025/QH15"},
		{"preamble:", "1e48fa20-84a7-11f1-8ef2-67037efa0c35", "279/2026/NĐ-CP"},
		{"article:1", "181234", "152/2025/NĐ-CP"},
		{"article:2", "55555", "01/2020/TT-BCA"},
	}
	if len(all) != len(want) {
		t.Fatalf("citations = %+v", all)
	}
	for i := range want {
		if all[i] != want[i] {
			t.Errorf("citation %d = %+v, want %+v", i, all[i], want[i])
		}
	}
	// the linked 152/2025 is the second one written in Điều 1, not the plain first
	for i, cs := range cits {
		for _, c := range cs {
			if c.VBPLID == "181234" {
				runes := []rune(chunks[i].Content)
				if before := string(runes[c.Start-len([]rune("Nghị định số ")) : c.Start]); before != "Nghị định số " {
					t.Errorf("linked occurrence preceded by %q", before)
				}
			}
		}
	}
	// review #9: FindCitations reads the same rune spans in the chunk content
	for i, cs := range cits {
		found := map[[2]int]bool{}
		for _, fc := range vietnamese_legal.FindCitations(chunks[i].Content) {
			found[[2]int{fc.Start, fc.End}] = true
		}
		for _, c := range cs {
			if !found[[2]int{c.Start, c.End}] {
				t.Errorf("chunk %d: link %q at %d:%d not a FindCitations span", i, c.Text, c.Start, c.End)
			}
		}
	}
	// the chunk's legal metadata carries them
	for i, c := range chunks {
		legal := c.Legal()
		legal.Citations = cits[i]
		if c.Kind == "article" && c.ArticleNo == "2" && (len(legal.Citations) != 1 || legal.Citations[0].VBPLID != "55555") {
			t.Errorf("Điều 2 legal citations = %+v", legal.Citations)
		}
	}
}

// Review: the text-only fallback must not land inside a longer number;
// two links with the same text and context take both occurrences; links to
// the document itself are dropped.
func TestCitationLinksReview(t *testing.T) {
	whole := func(md string) []Chunk {
		return []Chunk{{Start: 0, End: len([]rune(md)), Content: md}}
	}
	md := "Xem 15/2020/NĐ-CP và Nghị định 5/2020/NĐ-CP."
	d := &Document{Markdown: md, Links: []Link{{VBPLID: "9", Text: "5/2020/NĐ-CP", before: "không khớp "}}}
	got := CitationLinks(d, whole(md), "")[0]
	if len(got) != 1 || string([]rune(md)[got[0].Start:got[0].End]) != "5/2020/NĐ-CP" || got[0].Start != len([]rune("Xem 15/2020/NĐ-CP và Nghị định ")) {
		t.Fatalf("fallback = %+v", got)
	}

	md = "theo Nghị định số 5/2020/NĐ-CP; theo Nghị định số 5/2020/NĐ-CP"
	d = &Document{Markdown: md, Links: []Link{
		{VBPLID: "9", Text: "5/2020/NĐ-CP", before: "theo Nghị định số "},
		{VBPLID: "9", Text: "5/2020/NĐ-CP", before: "theo Nghị định số "},
	}}
	got = CitationLinks(d, whole(md), "")[0]
	if len(got) != 2 || got[0].Start == got[1].Start {
		t.Fatalf("repeated link = %+v", got)
	}

	md = "Điều 3 của Nghị định này và Nghị định 5/2020/NĐ-CP"
	d = &Document{Markdown: md, Links: []Link{
		{VBPLID: "own", Text: "Điều 3"},
		{VBPLID: "9", Text: "5/2020/NĐ-CP"},
	}}
	got = CitationLinks(d, whole(md), "own")[0]
	if len(got) != 1 || got[0].VBPLID != "9" {
		t.Fatalf("self link kept: %+v", got)
	}
}
