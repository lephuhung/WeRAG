package vbpl

import (
	"strings"
	"testing"
)

const header = `<table><tr><td><p><strong>CHÍNH PHỦ<br>-------</strong></p></td>
<td><p><strong>CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM<br>Độc lập - Tự do - Hạnh phúc</strong></p></td></tr>
<tr><td><p>Số: 48/2018/NĐ-CP</p></td><td><p><em>Hà Nội, ngày 21 tháng 3 năm 2018</em></p></td></tr></table>
<p>NGHỊ ĐỊNH</p><p>Căn cứ Luật Tổ chức Chính phủ ngày 19 tháng 6 năm 2015;</p>`

const signature = `<table><tr><td><p><strong>Nơi nhận:</strong><br>- Lưu: VT.</p></td>
<td><p>TM. CHÍNH PHỦ<br>THỦ TƯỚNG<br>Nguyễn Văn A</p></td></tr></table>`

func convert(t *testing.T, body string) *Document {
	t.Helper()
	d, err := ConvertBytes([]byte("<html><body>" + body + "</body></html>"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func kinds(d *Document) []BlockKind {
	var out []BlockKind
	for _, b := range d.Blocks {
		out = append(out, b.Kind)
	}
	return out
}

func findBlock(d *Document, kind BlockKind, prefix string) *Block {
	for i := range d.Blocks {
		if d.Blocks[i].Kind == kind && strings.HasPrefix(d.Blocks[i].Text, prefix) {
			return &d.Blocks[i]
		}
	}
	return nil
}

func checkOffsets(t *testing.T, d *Document) {
	t.Helper()
	r := []rune(d.Markdown)
	for i, b := range d.Blocks {
		if b.Start < 0 || b.End > len(r) || b.Start >= b.End {
			t.Fatalf("block %d bad range %d..%d", i, b.Start, b.End)
		}
	}
}

func TestConvertStructureFromClasses(t *testing.T) {
	d := convert(t, header+`
<p id="c1" class="prov-chapter">Chương II</p>
<p class="prov-chapter">TIẾP NHẬN NGƯỜI ĐANG CHẤP HÀNH ÁN</p>
<p class="prov-chapter">Ở NƯỚC NGOÀI VỀ VIỆT NAM</p>
<p id="a14" class="prov-article">Điều 14. Thẩm quyền</p>
<p class="prov-content">Tòa án nhân dân khu vực quyết định.</p>
<p id="a15" class="prov-article">Ðiều 15. Điều kiện</p>
<p id="k1" class="prov-clause">1. Người được tiếp nhận phải:</p>
<p id="d1" class="prov-item">a) Là công dân Việt Nam;</p>
<p id="k2" class="prov-clause">2. Chính phủ quy định chi tiết.</p>
<p>Đoạn không có class vẫn thuộc khoản 2.</p>`+signature)
	checkOffsets(t, d)
	if !d.Structured {
		t.Fatal("expected structured")
	}
	ch := findBlock(d, KindChapter, "Chương II")
	if ch == nil || ch.Text != "Chương II. TIẾP NHẬN NGƯỜI ĐANG CHẤP HÀNH ÁN Ở NƯỚC NGOÀI VỀ VIỆT NAM" || ch.ID != "c1" {
		t.Fatalf("chapter title not merged: %+v", ch)
	}
	a15 := findBlock(d, KindArticle, "Ðiều 15")
	if a15 == nil || a15.Number != "15" || a15.ID != "a15" {
		t.Fatalf("eth article not parsed: %+v", a15)
	}
	item := findBlock(d, KindItem, "a)")
	if item == nil || item.Number != "a" || d.Blocks[item.Parent].ID != "k1" {
		t.Fatalf("item parent wrong: %+v", item)
	}
	tail := findBlock(d, KindContent, "Đoạn không có class")
	if tail == nil || d.Blocks[tail.Parent].ID != "k2" {
		t.Fatal("unclassed paragraph not attached to the open clause")
	}
	if findBlock(d, KindClosing, "Nơi nhận") == nil {
		t.Fatal("signature table not closing")
	}
	if p := findBlock(d, KindPreamble, "Số: 48/2018/NĐ-CP"); p == nil {
		t.Fatal("số hiệu missing from preamble")
	}
	if !strings.Contains(d.Markdown, "## Chương II. TIẾP NHẬN") || !strings.Contains(d.Markdown, "#### Điều 14. Thẩm quyền") {
		t.Fatalf("markdown headings missing:\n%s", d.Markdown)
	}
}

func TestQuotedAmendmentIsNotOwnStructure(t *testing.T) {
	d := convert(t, header+`
<p id="a1" class="prov-article">Điều 1. Sửa đổi, bổ sung một số điều của Nghị định số 108/2024/NĐ-CP</p>
<p id="k1" class="prov-clause">1. Sửa đổi Điều 18 như sau:</p>
<p class="prov-content">“Điều 18. Hình thức xử lý tài sản</p>
<p class="prov-clause">1. Thu hồi.</p>
<p class="prov-item">a) Bán.”.</p>
<p id="k2" class="prov-clause">2. Trong Nghị định này, “tài sản” là tài sản công.</p>
<p id="a2" class="prov-article">Điều 2. Hiệu lực thi hành</p>`+signature)
	checkOffsets(t, d)
	if n := d.Stats.Articles; n != 2 {
		t.Fatalf("articles = %d, want 2 (quoted Điều 18 must not count)", n)
	}
	q := findBlock(d, KindQuote, "“Điều 18")
	if q == nil || d.Blocks[q.Parent].ID != "k1" {
		t.Fatalf("quoted article not a quote under khoản 1: %+v", q)
	}
	if findBlock(d, KindQuote, "1. Thu hồi") == nil || findBlock(d, KindQuote, "a) Bán") == nil {
		t.Fatal("quoted clause/item escaped the quote span")
	}
	// a quoted term inside a clause does not open a span
	if findBlock(d, KindClause, "2. Trong Nghị định này") == nil {
		t.Fatal("clause with a quoted term was swallowed")
	}
	if !strings.Contains(d.Markdown, "> “Điều 18. Hình thức xử lý tài sản") {
		t.Fatal("quote not rendered as blockquote")
	}
	if d.Stats.QuoteSpans != 1 {
		t.Fatalf("quote spans = %d", d.Stats.QuoteSpans)
	}
}

func TestUnclosedQuoteClosesAtRealArticle(t *testing.T) {
	d := convert(t, `<p id="a1" class="prov-article">Điều 1. Sửa đổi</p>
<p class="prov-content">“Điều 5. Nội dung bị bỏ quên dấu đóng</p>
<p id="a2" class="prov-article">Điều 2. Hiệu lực</p>`)
	if d.Stats.Articles != 2 || d.Stats.ForcedCloses != 1 {
		t.Fatalf("stats = %+v", d.Stats)
	}
}

func TestAttachedDocumentAfterSignature(t *testing.T) {
	d := convert(t, header+`
<p id="a1" class="prov-article">Điều 1. Ban hành kèm theo Quyết định này Quy chế công tác văn thư.</p>
<p id="a2" class="prov-article">Điều 2. Quyết định này có hiệu lực kể từ ngày ký.</p>`+signature+`
<table><tr><td><p>BỘ TÀI CHÍNH</p></td><td><p>CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM</p></td></tr></table>
<p>QUY CHẾ</p><p>Công tác văn thư của Bộ Tài chính</p><p>(Kèm theo Quyết định số 1313/QĐ-BTC)</p>
<p>Chương I</p><p>QUY ĐỊNH CHUNG</p>
<p>Điều 1. Phạm vi điều chỉnh</p>
<p>1. Quy chế này quy định về công tác văn thư.</p>
<p>2. Áp dụng thống nhất.</p>
<p>Chương II</p><p>CÔNG TÁC VĂN THƯ</p>
<p>I. SOẠN THẢO, BAN HÀNH VĂN BẢN</p>
<p>Điều 3. Hình thức văn bản</p><p>Nội dung điều 3.</p>
<p>II. QUẢN LÝ VĂN BẢN</p>
<p>Điều 7. Nguyên tắc chung</p><p>Nội dung điều 7.</p>`)
	checkOffsets(t, d)
	app := findBlock(d, KindAppendix, "QUY CHẾ")
	if app == nil || !strings.Contains(app.Text, "Công tác văn thư") || !strings.Contains(app.Text, "1313/QĐ-BTC") {
		t.Fatalf("attachment title: %+v", app)
	}
	art := findBlock(d, KindArticle, "Điều 7.")
	if art == nil || !art.Inferred || art.Number != "7" {
		t.Fatalf("inferred article: %+v", art)
	}
	chunks := Plan(d, 2500)
	var path string
	for _, c := range chunks {
		if c.ArticleNo == "7" && strings.HasPrefix(c.Path[0], "QUY CHẾ") {
			path = c.ContextHeader()
		}
	}
	want := "Chương II. CÔNG TÁC VĂN THƯ > II. QUẢN LÝ VĂN BẢN > Điều 7. Nguyên tắc chung"
	if !strings.HasSuffix(path, want) {
		t.Fatalf("path = %q, want suffix %q", path, want)
	}
	c1 := findBlock(d, KindClause, "1. Quy chế này")
	if c1 == nil || !c1.Inferred {
		t.Fatal("inferred clause missing")
	}
}

func TestAppendixRegionAndTableSpans(t *testing.T) {
	d := convert(t, `<p id="a1" class="prov-article">Điều 1. Phạm vi</p>
<p class="prov-content">Nội dung.</p>
<div class="doc-region-appendix"><p>PHỤ LỤC I</p><p>DANH MỤC HÀNG HÓA</p>
<table><tr><td>STT</td><td>Tên</td><td>Mức</td></tr>
<tr><td rowspan="2">1</td><td>A | B</td><td>10</td></tr>
<tr><td>C</td><td>20</td></tr></table></div>`)
	checkOffsets(t, d)
	app := findBlock(d, KindAppendix, "PHỤ LỤC I")
	if app == nil || app.Text != "PHỤ LỤC I DANH MỤC HÀNG HÓA" {
		t.Fatalf("appendix: %+v", app)
	}
	tb := findBlock(d, KindTable, "STT")
	if tb == nil || len(tb.RowStarts) != 2 {
		t.Fatalf("table: %+v", tb)
	}
	if !strings.Contains(d.Markdown, "| 1 | C | 20 |") {
		t.Fatalf("rowspan not repeated:\n%s", d.Markdown)
	}
	if !strings.Contains(d.Markdown, `| 1 | A \| B | 10 |`) {
		t.Fatal("pipe not escaped")
	}
}

func TestNoMarkupFallsBackToInference(t *testing.T) {
	d := convert(t, `<p>QUYẾT ĐỊNH</p><p>Điều 1. Phạm vi</p><p>1. Khoản một.</p><p>Điều 2. Hiệu lực</p>`)
	if !d.Structured || d.Stats.Articles != 2 || len(d.Warnings) == 0 {
		t.Fatalf("doc = %+v", d)
	}
	empty := convert(t, ``)
	if empty.Structured || len(empty.Blocks) != 0 {
		t.Fatal("empty page must be unstructured")
	}
}

func TestPlanOneArticlePerChunk(t *testing.T) {
	d := convert(t, header+`
<p class="prov-chapter">Chương I</p><p class="prov-chapter">QUY ĐỊNH CHUNG</p>
<p id="a1" class="prov-article">Điều 1. Phạm vi</p><p class="prov-content">Ngắn.</p>
<p id="a2" class="prov-article">Điều 2. (Được bãi bỏ)</p>
<p id="a3" class="prov-article">Điều 3. Dài</p>
<p id="k1" class="prov-clause">1. `+strings.Repeat("Nội dung khoản một. ", 30)+`</p>
<p id="k2" class="prov-clause">2. Sửa đổi như sau:</p>
<p class="prov-content">“`+strings.Repeat("Nội dung trích dẫn dài. ", 40)+`”.</p>
<p id="k3" class="prov-clause">3. `+strings.Repeat("Khoản ba. ", 20)+`</p>`+signature)
	chunks := Plan(d, 800)
	runes := []rune(d.Markdown)
	var arts []string
	for _, c := range chunks {
		if c.Content != string(runes[c.Start:c.End]) {
			t.Fatal("chunk is not a markdown slice")
		}
		if c.End-c.Start > 800 {
			t.Fatalf("chunk over limit: %d", c.End-c.Start)
		}
		if strings.Contains(c.Content, "Nơi nhận") && c.Kind != "closing" {
			t.Fatal("closing mixed into a content chunk")
		}
		arts = append(arts, c.ArticleNo)
	}
	// every block is covered by some chunk
	for _, b := range d.Blocks {
		covered := false
		for _, c := range chunks {
			if c.Start <= b.Start && b.End <= c.End {
				covered = true
				break
			}
		}
		if !covered && !(b.Kind == KindTable || b.Kind == KindQuote || b.Kind == KindContent) {
			t.Fatalf("block %q not covered by any chunk", b.Text)
		}
	}
	if !strings.HasPrefix(chunks[1].Content, "## Chương I. QUY ĐỊNH CHUNG") {
		t.Fatalf("bare chapter heading not prepended: %q", chunks[1].Content)
	}
	if chunks[0].Kind != "preamble" || !strings.Contains(chunks[0].Content, "48/2018/NĐ-CP") {
		t.Fatal("preamble chunk missing")
	}
	// Điều 1 and Điều 2 stay separate even though both are tiny
	if arts[1] != "1" || arts[2] != "2" || chunks[2].ArticleID != "a2" {
		t.Fatalf("articles = %v", arts)
	}
	if got := chunks[1].ContextHeader(); got != "Chương I. QUY ĐỊNH CHUNG > Điều 1. Phạm vi" {
		t.Fatalf("path = %q", got)
	}
	// Điều 3 is split; the short lead-in "2. Sửa đổi như sau:" travels with its quote
	var k2 *Chunk
	for i := range chunks {
		if chunks[i].ArticleNo == "3" && strings.Contains(chunks[i].Content, "2. Sửa đổi như sau:") {
			k2 = &chunks[i]
		}
	}
	if k2 == nil || !strings.Contains(k2.Content, "Nội dung trích dẫn") || !k2.Quoted {
		t.Fatalf("lead-in separated from its quote: %+v", k2)
	}
	if k2.Part == 0 {
		t.Fatal("split article chunk must carry its part number")
	}
}

func TestChunkLegalKeys(t *testing.T) {
	d := convert(t, `<p id="a5" class="prov-article">Điều 5. Phạt tiền</p>
<p id="k1" class="prov-clause">1. Phạt từ 1 triệu:</p>
<p id="d1a" class="prov-item">a) `+strings.Repeat("Hành vi một. ", 60)+`</p>
<p id="d1b" class="prov-item">b) `+strings.Repeat("Hành vi hai. ", 60)+`</p>
<p id="k2" class="prov-clause">2. Phạt từ 2 triệu.</p>`)
	chunks := Plan(d, 900)
	if len(chunks) < 2 {
		t.Fatalf("expected split, got %d", len(chunks))
	}
	last := chunks[len(chunks)-1]
	if last.IDs["dieu"] != "a5" || last.IDs["khoan:2"] != "k2" {
		t.Fatalf("ids = %v", last.IDs)
	}
	// the piece holding điểm b is still khoản 1
	var b *Chunk
	for i := range chunks {
		if strings.Contains(chunks[i].Content, "b) Hành vi hai") {
			b = &chunks[i]
		}
	}
	if b == nil || b.IDs["khoan:1/diem:b"] != "d1b" || b.IDs["khoan:1"] != "k1" {
		t.Fatalf("diem chunk = %+v", b)
	}
	m := b.Legal()
	if m.SchemaVersion != 2 || m.Source != "vbpl" || m.ArticleNos[0] != "5" || !contains(m.SubdivisionRefs, "khoan:1/diem:b") {
		t.Fatalf("legal = %+v", m)
	}
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func TestPlanHeadingWithoutRoomStandsAlone(t *testing.T) {
	d := convert(t, header+`
<p class="prov-chapter">Chương II</p><p class="prov-chapter">QUY ĐỊNH CỤ THỂ</p>
<p id="a1" class="prov-article">Điều 1. Dài</p>
<p class="prov-content">`+strings.Repeat("x", 780)+`</p>`+signature)
	chunks := Plan(d, 800)
	for _, c := range chunks {
		if c.End-c.Start > 800 {
			t.Fatalf("chunk over limit: %d %q", c.End-c.Start, c.Content[:40])
		}
	}
	if !strings.HasPrefix(chunks[1].Content, "## Chương II") || chunks[1].ArticleNo != "" {
		t.Fatalf("heading chunk: %+v", chunks[1])
	}
	if chunks[2].ArticleNo != "1" {
		t.Fatalf("article chunk: %+v", chunks[2])
	}
}

func TestAmendTargets(t *testing.T) {
	d := convert(t, header+`
<p id="a1" class="prov-article">Điều 1. Sửa đổi, bổ sung một số điều của Luật Phòng, chống thiên tai</p>
<p id="k1" class="prov-clause">1. Sửa đổi, bổ sung một số khoản của Điều 3 như sau:</p>
<p id="i1" class="prov-item">a) Sửa đổi, bổ sung khoản 1 như sau:</p>
<p class="prov-content">“1. Thiên tai là hiện tượng tự nhiên.”;</p>
<p id="k2" class="prov-clause">2. Bổ sung khoản 6 vào sau khoản 5 Điều 5 như sau:</p>
<p class="prov-content">“6. Ưu tiên bố trí nguồn lực.”.</p>
<p id="k3" class="prov-clause">3. Sửa đổi, bổ sung Điều 6 như sau:</p>
<p class="prov-content">“Điều 6. Nguồn nhân lực</p>
<p class="prov-content">1. Lực lượng xung kích.”.</p>
<p id="a2" class="prov-article">Điều 2. Sửa đổi điểm b khoản 2 Điều 7 của Nghị định số 100/2016/NĐ-CP ngày 01 tháng 7 năm 2016 như sau:</p>
<p class="prov-content">“b) Nội dung mới.”.</p>
<p id="a3" class="prov-article">Điều 3. Hiệu lực thi hành</p>`+signature)
	want := map[string]Amend{
		"“1. Thiên tai":     {Document: "Luật Phòng, chống thiên tai", Article: "3", Khoan: "1"},
		"“6. Ưu tiên":       {Document: "Luật Phòng, chống thiên tai", Article: "5", Khoan: "6"},
		"“Điều 6. Nguồn":    {Document: "Luật Phòng, chống thiên tai", Article: "6"},
		"1. Lực lượng":      {Document: "Luật Phòng, chống thiên tai", Article: "6"},
		"“b) Nội dung mới.": {Document: "Nghị định số 100/2016/NĐ-CP", Article: "7", Khoan: "2", Diem: "b"},
	}
	for prefix, w := range want {
		q := findBlock(d, KindQuote, prefix)
		if q == nil || q.Amend == nil || *q.Amend != w {
			t.Errorf("%q: amend = %+v, want %+v", prefix, q, w)
		}
	}

	chunks := Plan(d, 2500)
	var art1 *Chunk
	for i := range chunks {
		if chunks[i].ArticleNo == "1" {
			art1 = &chunks[i]
		}
	}
	if art1 == nil || len(art1.Amends) != 3 {
		t.Fatalf("Điều 1 chunk amends: %+v", art1)
	}
	if cite := art1.Legal().Citation(); cite != "Điều 1 (khoản 1, 2, 3) (sửa đổi khoản 1 Điều 3, khoản 6 Điều 5, Điều 6 Luật Phòng, chống thiên tai)" {
		t.Fatalf("citation = %q", cite)
	}
}

func TestSentenceCaseKeepsNumber(t *testing.T) {
	if got := sentenceCase("NGHỊ ĐỊNH SỐ 100/2016/NĐ-CP"); got != "Nghị định số 100/2016/NĐ-CP" {
		t.Fatal(got)
	}
}
