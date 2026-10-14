package docformat

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// bodyLayout is a layout whose paragraphs are all body (noi_dung); a text
// starting with "#data " is a data-table cell.
func bodyLayout(texts ...string) (*Layout, *Segmentation) {
	l := &Layout{}
	comp := &Component{Found: true}
	for i, t := range texts {
		p := &Para{Index: i, Text: t, Zone: ZoneFull}
		if rest, ok := strings.CutPrefix(t, "#data "); ok {
			p.Text, p.InTable, p.TableLayout = rest, true, "data"
		}
		l.Paragraphs = append(l.Paragraphs, p)
		comp.Paras = append(comp.Paras, i)
		comp.Zones = append(comp.Zones, ZoneFull)
	}
	return l, &Segmentation{Components: map[string]*Component{"noi_dung": comp}}
}

func anchorKinds(lines []AnchorLine) string {
	var parts []string
	for _, a := range lines {
		parts = append(parts, fmt.Sprintf("%d:%s", a.Para, a.Kind))
	}
	return strings.Join(parts, " ")
}

func mustRuleSet(t *testing.T, typ string) *RuleSet {
	t.Helper()
	rs, err := LoadRuleSet(typ)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func TestAnchorsQuyetDinh(t *testing.T) {
	l, seg := bodyLayout(
		"QUYẾT ĐỊNH:",
		"Điều 1. Ban hành kèm theo Quyết định này Kế hoạch triển khai.",
		"Các đơn vị tổ chức thực hiện theo kế hoạch.",
		"Điều 2. Quyết định này có hiệu lực kể từ ngày ký.",
		"Điều 3. Chánh Văn phòng, Thủ trưởng các đơn vị chịu trách nhiệm thi hành Quyết định này./.",
	)
	got, n := ExtractAnchors(l, seg, mustRuleSet(t, "quyet_dinh").Anchors)
	if n != 5 {
		t.Fatalf("body paras = %d", n)
	}
	// the closing regex matches Điều 3 first; it stays one line, with the
	// Điều description added
	if k := anchorKinds(got); k != "0:resolution_line 1:dieu 3:dieu 4:closing" {
		t.Fatalf("anchors = %s", k)
	}
	last := got[len(got)-1]
	if !strings.Contains(last.Desc, "câu kết") || !strings.Contains(last.Desc, "Điều") {
		t.Errorf("last line desc = %q", last.Desc)
	}
	for _, a := range got {
		if a.Para == 2 {
			t.Error("a plain middle paragraph must not be an anchor")
		}
	}
}

func TestAnchorsKeepLastWhenCapped(t *testing.T) {
	var texts []string
	for i := 1; i <= 30; i++ {
		texts = append(texts, fmt.Sprintf("Điều %d. Nội dung điều %d.", i, i))
	}
	l, seg := bodyLayout(texts...)
	got, _ := ExtractAnchors(l, seg, []Anchor{{ID: "dieu", Regex: `^DIEU\s+\d+\.?`, Max: 20, Desc: "Điều"}})
	if len(got) != 20 || got[18].Para != 18 || got[19].Para != 29 {
		t.Fatalf("anchors = %s", anchorKinds(got))
	}
}

func TestAnchorsBaoCao(t *testing.T) {
	long := "Trong năm 2025, " + strings.Repeat("đơn vị đã triển khai đồng bộ các nhiệm vụ, ", 8)
	l, seg := bodyLayout(
		"I. ĐẶC ĐIỂM, TÌNH HÌNH CHUNG",
		long,
		"II. KẾT QUẢ THỰC HIỆN",
		"1. Công tác tuyên truyền",
		"Đã tổ chức 12 hội nghị, trong đó có 05 hội nghị trực tuyến; kết quả cụ thể như sau.",
		"1. Thực hiện đồng bộ các giải pháp đã đề ra; bảo đảm tiến độ.",
		"III. TỒN TẠI, HẠN CHẾ",
		"Trên đây là báo cáo kết quả thực hiện năm 2025 của đơn vị.",
		"#data 12",
	)
	got, n := ExtractAnchors(l, seg, mustRuleSet(t, "bao_cao").Anchors)
	if n != 9 {
		t.Fatalf("body paras = %d", n)
	}
	want := "0:heading 1:opening 2:heading 3:heading 6:heading 7:closing"
	if k := anchorKinds(got); k != want {
		t.Fatalf("anchors = %s, want %s", k, want)
	}
	if txt := got[1].Text; runeLen(txt) != anchorTextRunes+1 || !strings.HasSuffix(txt, "…") {
		t.Errorf("long line not truncated: %d runes", runeLen(txt))
	}
}

func TestAnchorsNoBody(t *testing.T) {
	got, n := ExtractAnchors(&Layout{}, &Segmentation{Components: map[string]*Component{}}, mustRuleSet(t, "base").Anchors)
	if got != nil || n != 0 {
		t.Fatalf("got %v, %d", got, n)
	}
}

func TestRuleSetAnchorsMerge(t *testing.T) {
	ids := func(as []Anchor) string {
		var out []string
		for _, a := range as {
			out = append(out, a.ID)
		}
		return strings.Join(out, ",")
	}
	if got := ids(mustRuleSet(t, "base").Anchors); got != "opening,heading,closing,tail" {
		t.Errorf("base anchors = %s", got)
	}
	if got := ids(mustRuleSet(t, "cong_van").Anchors); got != "opening,heading,closing,tail" {
		t.Errorf("cong_van anchors = %s", got)
	}
	if got := ids(mustRuleSet(t, "quyet_dinh").Anchors); got != "opening,heading,closing,tail,resolution_line,dieu" {
		t.Errorf("quyet_dinh anchors = %s", got)
	}
	merged := mergeAnchors(
		[]Anchor{{ID: "a", Max: 1}, {ID: "b", Max: 1}},
		[]Anchor{{ID: "c", Max: 3}, {ID: "a", Max: 9}})
	if ids(merged) != "a,b,c" || merged[0].Max != 9 {
		t.Errorf("merge = %+v", merged)
	}
	// every anchor of every rule set is usable
	for _, typ := range append(AvailableTypes(), "base") {
		for _, a := range mustRuleSet(t, typ).Anchors {
			switch a.Kind {
			case AnchorHead, AnchorTail:
				if a.Max <= 0 {
					t.Errorf("%s/%s: head/tail needs max", typ, a.ID)
				}
			case "", AnchorRegex:
				if anchorRegexp(a.Regex) == nil || a.Regex == "" {
					t.Errorf("%s/%s: bad regex %q", typ, a.ID, a.Regex)
				}
			default:
				t.Errorf("%s/%s: unknown kind %q", typ, a.ID, a.Kind)
			}
			if a.Desc == "" {
				t.Errorf("%s/%s: no desc", typ, a.ID)
			}
		}
	}
}

func TestRenderJudgment(t *testing.T) {
	content, _ := fixture(t, "fx_qd_so_y_te")
	r := Check(context.Background(), content, Options{Segmenter: SegmenterHeuristic, SourceName: "qd.docx"})
	if r.Skeleton == "" || r.Skeleton != RenderJudgment(r) {
		t.Fatal("Check must set the skeleton")
	}
	if r.BodyParas == 0 || len(r.Anchors) == 0 {
		t.Fatalf("body paras %d, anchors %v", r.BodyParas, r.Anchors)
	}
	s := r.Skeleton
	for _, want := range []string{
		"# Hồ sơ phán đoán", "Văn bản: qd.docx", "Loại: quyet_dinh",
		"Lỗi đo được: không có", "Đã đo đạt (không chấm lại): Trang (khổ giấy, lề)",
		`[3] Số, ký hiệu: "Số: 1234/QĐ-SYT"`, "Thẩm quyền ban hành: \"GIÁM ĐỐC SỞ Y TẾ\"",
		"[6] Căn cứ:", `[nghiêng]`, "dòng mốc", `"QUYẾT ĐỊNH:"`, `"Điều 2. Quyết định này có hiệu lực`,
		"[7] Khối ký (cột phải):", `"KT. GIÁM ĐỐC" [đậm]`, "[9b] Nơi nhận (cột trái; dòng đầu là nhãn", `"- Lưu: VT, TCCB."`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("skeleton lacks %q", want)
		}
	}
	for _, omit := range []string{"Times New Roman", "pt,", "căn center", "căn both",
		"CỘNG HÒA XÃ HỘI", "Độc lập - Tự do", "Trang: ", "<skill"} {
		if strings.Contains(s, omit) {
			t.Errorf("skeleton must not carry %q", omit)
		}
	}
	// the [7] block is chức vụ then họ tên
	if i, j := strings.Index(s, "PHÓ GIÁM ĐỐC"), strings.Index(s, "Trần Văn Bình"); i < 0 || j < i {
		t.Error("signature lines out of order")
	}
	if !strings.Contains(RenderForAgent(r), "# Hồ sơ phán đoán") {
		t.Error("the agent fallback renders the judgment brief")
	}
}

func TestRenderJudgmentMissingRequired(t *testing.T) {
	content, _ := fixture(t, "fx_bao_cao_two_tables")
	r := Check(context.Background(), content, Options{Segmenter: SegmenterHeuristic})
	if !strings.Contains(r.Skeleton, "Không có (bắt buộc với loại này): Kính gửi") {
		t.Errorf("skeleton:\n%s", r.Skeleton)
	}
	if strings.Contains(r.Skeleton, `"12"`) {
		t.Error("a data-table cell is no body anchor")
	}
}

func TestSkillsDigest(t *testing.T) {
	d := SkillsDigest()
	if len(d) != 16 || d != SkillsDigest() {
		t.Fatalf("digest %q not stable", d)
	}
	if _, err := hex.DecodeString(d); err != nil {
		t.Fatalf("digest %q not hex", d)
	}
}

func TestEvaluateWithSkillsRender(t *testing.T) {
	content, _ := fixture(t, "fx_ky_thay_missing_kt")
	r := Check(context.Background(), content, Options{Segmenter: SegmenterHeuristic})
	spy := &evalSpy{reply: "ok"}
	if _, err := EvaluateWithSkillsOpts(context.Background(), spy, r, EvalOptions{ForceLLM: true}); err != nil {
		t.Fatal(err)
	}
	if user := spy.got[1].Content; !strings.HasPrefix(user, "# Hồ sơ phán đoán") || strings.Contains(user, "# Dữ liệu thể thức") {
		t.Errorf("default render must be the judgment brief:\n%s", user)
	}
	if _, err := EvaluateWithSkillsOpts(context.Background(), spy, r, EvalOptions{Render: RenderData, ForceLLM: true}); err != nil {
		t.Fatal(err)
	}
	if user := spy.got[1].Content; !strings.HasPrefix(user, "# Kết quả đo thể thức") {
		t.Errorf("Render option ignored:\n%s", user)
	}
	sys := spy.got[0].Content
	if strings.Contains(sys, "7. Chính tả") || !strings.Contains(sys, "không nêu lỗi chính tả") {
		t.Error("the general skill must leave spelling to the spellcheck tool")
	}
}
