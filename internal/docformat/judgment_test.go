package docformat

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// itemStatuses is the status of each Bảng kiểm item.
func itemStatuses(items []JudgmentItem) map[int]string {
	out := map[int]string{}
	for _, it := range items {
		out[it.Item] = it.Status
	}
	return out
}

// TestJudgmentHarnessDocs is the acceptance test of the judgment rules:
// on every document of the live harness, code alone marks exactly the
// expected items "Lỗi" and leaves nothing to the model.
func TestJudgmentHarnessDocs(t *testing.T) {
	docs := loadLiveDocs(t)
	if len(docs) != 15 {
		t.Fatalf("harness documents = %d, want 15", len(docs))
	}
	var table strings.Builder
	table.WriteString("| document | expected Lỗi | 1 | 2 | 3 | 4 | 5 | 6 |\n|---|---|---|---|---|---|---|---|\n")
	for _, d := range docs {
		items := Judge(d.Report)
		st := itemStatuses(items)
		fmt.Fprintf(&table, "| %s | %v |", d.Name, d.Errors)
		for n := 1; n <= 6; n++ {
			fmt.Fprintf(&table, " %s |", st[n])
			if st[n] == JudgeUndetermined {
				t.Errorf("%s: item %d left to the model: %s", d.Name, n, items[n-1].Reason)
			}
			if contains(d.Any, n) {
				continue
			}
			if got, want := st[n] == JudgeFail, contains(d.Errors, n); got != want {
				t.Errorf("%s: item %d = %s, want Lỗi=%v (%s)", d.Name, n, ChecklistLine(items[n-1]), want, d.Note)
			}
		}
		table.WriteString("\n")
	}
	t.Log("\n" + table.String())
}

// The other ký-thay parity fixtures.
func TestJudgmentKyThayFixtures(t *testing.T) {
	for _, tc := range []struct {
		name string
		want map[int]string
	}{
		{"fx_ky_thay_deputy_listed", map[int]string{2: JudgePass, 3: JudgeFail}},
		{"fx_ky_thay_tm_chain", map[int]string{2: JudgePass, 3: JudgePass}},
		{"fx_ky_thay_unmarked", map[int]string{2: JudgePass, 3: JudgeWarn}},
	} {
		content, err := os.ReadFile("testdata/parity/" + tc.name + ".docx")
		if err != nil {
			t.Fatal(err)
		}
		r := Check(context.Background(), content, Options{Segmenter: SegmenterHeuristic, SourceName: tc.name})
		items := Judge(r)
		st := itemStatuses(items)
		for n, want := range tc.want {
			if st[n] != want {
				t.Errorf("%s item %d = %q, want %s", tc.name, n, ChecklistLine(items[n-1]), want)
			}
		}
	}
}

// synth builds a report from component lines (a line may hold ⏎ as \n).
func synth(docType string, comps map[string][]string, anchors ...AnchorLine) *Report {
	r := &Report{OK: true, Source: "synth.docx",
		DocumentType: &DocumentTypeInfo{Requested: "auto", Detected: docType, Used: docType, RuleSet: docType},
		Components:   map[string]*Component{}, Anchors: anchors, BodyParas: len(anchors)}
	zones := map[string]string{"chuc_danh": ZoneRight, "nguoi_ky": ZoneRight, "noi_nhan": ZoneLeft}
	para := 0
	for key, lines := range comps {
		cf := ComponentFormat{Key: key, Name: ComponentName(key)}
		comp := &Component{Found: true}
		for _, l := range lines {
			z := zones[key]
			if z == "" {
				z = ZoneFull
			}
			cf.Lines = append(cf.Lines, LineFormat{Para: para, Text: l, Zone: z})
			comp.Paras = append(comp.Paras, para)
			comp.Texts = append(comp.Texts, l)
			para++
		}
		r.Format = append(r.Format, cf)
		r.Components[key] = comp
	}
	if _, ok := comps["chuc_danh"]; ok {
		r.Components["signature"] = &Component{Found: true}
	}
	return r
}

func item(t *testing.T, r *Report, n int) JudgmentItem {
	t.Helper()
	for _, it := range Judge(r) {
		if it.Item == n {
			return it
		}
	}
	t.Fatalf("no item %d", n)
	return JudgmentItem{}
}

func expectItem(t *testing.T, name string, r *Report, n int, want string) JudgmentItem {
	t.Helper()
	it := item(t, r, n)
	if it.Status != want {
		t.Errorf("%s: item %d = %q (%s), want %s", name, n, ChecklistLine(it), it.Status, want)
	}
	return it
}

func TestJudgeKyThay(t *testing.T) {
	cv := func(chuc ...string) *Report {
		return synth("cong_van", map[string][]string{"co_quan_ban_hanh": {"SỞ NỘI VỤ"}, "chuc_danh": chuc})
	}
	cases := []struct {
		name string
		r    *Report
		want string
	}{
		{"head signs", cv("GIÁM ĐỐC"), JudgePass},
		{"KT ok", cv("KT. GIÁM ĐỐC", "PHÓ GIÁM ĐỐC"), JudgePass},
		{"KT ok in one paragraph", cv("KT. TRƯỞNG PHÒNG\nPHÓ TRƯỞNG PHÒNG"), JudgePass},
		{"TM KT chain", synth("cong_van", map[string][]string{"co_quan_ban_hanh": {"ỦY BAN NHÂN DÂN", "TỈNH HÀ TĨNH"},
			"chuc_danh": {"TM. ỦY BAN NHÂN DÂN", "KT. CHỦ TỊCH", "PHÓ CHỦ TỊCH"}}), JudgePass},
		{"title prefix", cv("KT. CHỦ TỊCH", "PHÓ CHỦ TỊCH THƯỜNG TRỰC"), JudgePass},
		{"PHO without KT", cv("GIÁM ĐỐC", "PHÓ GIÁM ĐỐC"), JudgeFail},
		{"PHO alone", cv("PHÓ GIÁM ĐỐC"), JudgeFail},
		{"KT title mismatch", cv("KT. GIÁM ĐỐC", "PHÓ TRƯỞNG PHÒNG"), JudgeFail},
		{"K/T", cv("K/T GIÁM ĐỐC", "PHÓ GIÁM ĐỐC"), JudgeFail},
		{"KT without dot", cv("KT GIÁM ĐỐC", "PHÓ GIÁM ĐỐC"), JudgeFail},
		{"Kt lower case", cv("Kt. GIÁM ĐỐC", "PHÓ GIÁM ĐỐC"), JudgeFail},
		{"collective without TM", synth("cong_van", map[string][]string{"co_quan_ban_hanh": {"ỦY BAN NHÂN DÂN", "TỈNH HÀ TĨNH"},
			"chuc_danh": {"CHỦ TỊCH"}}), JudgeFail},
		{"collective TL", synth("cong_van", map[string][]string{"co_quan_ban_hanh": {"ỦY BAN NHÂN DÂN", "TỈNH HÀ TĨNH"},
			"chuc_danh": {"TL. CHỦ TỊCH", "CHÁNH VĂN PHÒNG"}}), JudgePass},
		{"two titles on one line", cv("KT. GIÁM ĐỐC PHÓ GIÁM ĐỐC"), JudgeUndetermined},
		{"only the signer's name", synth("cong_van", map[string][]string{"nguoi_ky": {"Nguyễn Văn A"}}), JudgeUndetermined},
		{"no signature", synth("cong_van", map[string][]string{"so_ky_hieu": {"Số: 12/UBND-VP"}}), JudgeUnknown},
	}
	for _, tc := range cases {
		expectItem(t, tc.name, tc.r, 2, tc.want)
	}
	// biên bản: no TM. for the thư ký / chủ tọa blocks
	bb := synth("bien_ban", map[string][]string{"co_quan_ban_hanh": {"ỦY BAN NHÂN DÂN"}, "chuc_danh": {"CHỦ TỌA"}})
	expectItem(t, "bien_ban", bb, 2, JudgePass)
}

func TestJudgeHeadInNoiNhan(t *testing.T) {
	r := func(chuc []string, nn ...string) *Report {
		return synth("cong_van", map[string][]string{"chuc_danh": chuc, "noi_nhan": append([]string{"Nơi nhận:"}, nn...)})
	}
	kt := []string{"KT. GIÁM ĐỐC", "PHÓ GIÁM ĐỐC"}
	cases := []struct {
		name string
		r    *Report
		want string
	}{
		{"no deputy", r([]string{"GIÁM ĐỐC"}, "- Như trên;", "- Lưu: VT, VP."), JudgePass},
		{"head b/c", r(kt, "- Như trên;", "- Giám đốc Sở (để b/c);", "- Lưu: VT, VP."), JudgePass},
		{"Đ/c head báo cáo", r(kt, "- Đ/c Giám đốc (để báo cáo);", "- Lưu: VT."), JudgePass},
		{"CT alias", r([]string{"TM. ỦY BAN NHÂN DÂN", "KT. CHỦ TỊCH", "PHÓ CHỦ TỊCH"}, "- CT UBND tỉnh (để b/c);", "- Lưu: VT."), JudgePass},
		{"head without b/c", r(kt, "- Giám đốc Sở;", "- Lưu: VT."), JudgeWarn},
		{"only deputy", r(kt, "- Phó Giám đốc Sở (để b/c);", "- Lưu: VT."), JudgeFail},
		{"no head", r(kt, "- Như trên;", "- Lưu: VT, VP."), JudgeFail},
		{"missing KT still checked", r([]string{"GIÁM ĐỐC", "PHÓ GIÁM ĐỐC"}, "- Như trên;", "- Lưu: VT."), JudgeFail},
		{"agency b/c is no head", r(kt, "- UBND tỉnh (để báo cáo);", "- Lưu: VT."), JudgeFail},
		{"unknown person b/c", r(kt, "- Đ/c Nguyễn Văn A (để b/c);", "- Lưu: VT."), JudgeUndetermined},
	}
	for _, tc := range cases {
		expectItem(t, tc.name, tc.r, 3, tc.want)
	}
}

func TestJudgeNoiNhanFormat(t *testing.T) {
	r := func(nn ...string) *Report {
		return synth("cong_van", map[string][]string{"noi_nhan": nn})
	}
	cases := []struct {
		name string
		r    *Report
		want string
	}{
		{"ok", r("Nơi nhận:", "- Như trên;", "- Lưu: VT, VP."), JudgePass},
		{"wrapped entry", r("Nơi nhận:", "- Ủy ban nhân dân tỉnh, Sở Nội vụ,", "Sở Tài chính;", "- Lưu: VT, VP."), JudgePass},
		{"Lưu without VT", r("Nơi nhận:", "- Như trên;", "- Lưu: VP."), JudgeFail},
		{"no Lưu", r("Nơi nhận:", "- Như trên;"), JudgeFail},
		{"no dash", r("Nơi nhận:", "Như trên;", "- Lưu: VT."), JudgeFail},
		{"dash without space", r("Nơi nhận:", "-Như trên;", "- Lưu: VT."), JudgeFail},
		{"no end mark", r("Nơi nhận:", "- Như trên", "- Lưu: VT."), JudgeFail},
		{"label without colon", r("Nơi nhận", "- Như trên;", "- Lưu: VT."), JudgeFail},
		{"no Nơi nhận", synth("cong_van", map[string][]string{"so_ky_hieu": {"Số: 12/UBND-VP"}}), JudgeUnknown},
	}
	for _, tc := range cases {
		expectItem(t, tc.name, tc.r, 4, tc.want)
	}
}

func TestJudgeSoKyHieuDate(t *testing.T) {
	r := func(docType, so, date string) *Report {
		return synth(docType, map[string][]string{"so_ky_hieu": {so}, "dia_danh_ngay_thang": {date}})
	}
	const date = "Huế, ngày 05 tháng 10 năm 2026"
	cases := []struct {
		name string
		r    *Report
		want string
	}{
		{"qd ok", r("quyet_dinh", "Số: 125/QĐ-SYT", date), JudgePass},
		{"cv ok", r("cong_van", "Số: 45/SNV-VP", date), JudgePass},
		{"cv unit code", r("cong_van", "Số: 12/PA05-Đ4", date), JudgePass},
		{"nghi dinh year", r("nghi_dinh", "Số: 12/2025/NĐ-CP", "Hà Nội, ngày 05 tháng 01 năm 2025"), JudgePass},
		{"draft without number", r("bao_cao", "Số:      /BC-SYT", "Huế, ngày      tháng     năm 2026"), JudgePass},
		{"tháng 7", r("cong_van", "Số: 45/SNV-VP", "Huế, ngày 07 tháng 7 năm 2026"), JudgePass},
		{"tháng 07", r("cong_van", "Số: 45/SNV-VP", "Huế, ngày 07 tháng 07 năm 2026"), JudgeWarn},
		{"no leading zero number", r("quyet_dinh", "Số: 5/QĐ-SYT", date), JudgeFail},
		{"no colon", r("quyet_dinh", "Số 125/QĐ-SYT", date), JudgeFail},
		{"type code mismatch", r("quyet_dinh", "Số: 125/BC-SYT", date), JudgeFail},
		{"typed doc without type code", r("quyet_dinh", "Số: 125/UBND-VP", date), JudgeFail},
		{"cong van with type code", r("cong_van", "Số: 45/CV-SNV", date), JudgeFail},
		{"cong van with QĐ", r("cong_van", "Số: 45/QĐ-SNV", date), JudgeFail},
		{"day without zero", r("cong_van", "Số: 45/SNV-VP", "Huế, ngày 5 tháng 10 năm 2026"), JudgeFail},
		{"month 1 without zero", r("cong_van", "Số: 45/SNV-VP", "Huế, ngày 15 tháng 1 năm 2026"), JudgeFail},
		{"invalid date", r("cong_van", "Số: 45/SNV-VP", "Huế, ngày 31 tháng 4 năm 2026"), JudgeFail},
		{"no place", r("cong_van", "Số: 45/SNV-VP", "ngày 05 tháng 10 năm 2026"), JudgeFail},
		{"unreadable date", r("cong_van", "Số: 45/SNV-VP", "Huế, ngày năm tháng mười năm 2026"), JudgeUndetermined},
	}
	for _, tc := range cases {
		expectItem(t, tc.name, tc.r, 5, tc.want)
	}
}

func dieu(para int, text string, ids ...string) AnchorLine {
	return AnchorLine{Kind: ids[0], IDs: ids, Para: para, Text: text}
}

func TestJudgeTypeRequirements(t *testing.T) {
	qd := func(tq bool, anchors ...AnchorLine) *Report {
		c := map[string][]string{"trich_yeu": {"QUYẾT ĐỊNH", "Về việc …"}}
		if tq {
			c["tham_quyen_ban_hanh"] = []string{"GIÁM ĐỐC SỞ Y TẾ"}
		}
		return synth("quyet_dinh", c, anchors...)
	}
	res := dieu(10, "QUYẾT ĐỊNH:", "resolution_line")
	cut := "Điều 3. Chánh Văn phòng Sở, Trưởng các phòng chức năng và Thủ trưởng các đơn vị trực thuộc Sở và các tổ chức, cá nhân có liên quan căn cứ Quyết định này triển khai…"
	cases := []struct {
		name string
		r    *Report
		want string
	}{
		{"qd ok", qd(true, res, dieu(11, "Điều 1. Ban hành kèm theo Quyết định này Kế hoạch.", "dieu"), dieu(12, "Điều 2. Quyết định này có hiệu lực kể từ ngày ký./.", "closing", "dieu", "tail")), JudgePass},
		{"qd no QUYẾT ĐỊNH:", qd(true, dieu(11, "Điều 1. Ban hành kèm theo Quyết định này Kế hoạch.", "dieu"), dieu(12, "Điều 2. … thi hành Quyết định này./.", "dieu", "tail")), JudgeFail},
		{"qd no effect", qd(true, res, dieu(11, "Điều 1. Ban hành kèm theo Quyết định này Kế hoạch.", "dieu"), dieu(12, "Điều 2. Giao Văn phòng Sở hướng dẫn, đôn đốc triển khai Kế hoạch.", "dieu", "tail")), JudgeFail},
		{"qd no thẩm quyền", qd(false, res, dieu(12, "Điều 2. Quyết định này có hiệu lực kể từ ngày ký./.", "dieu")), JudgeFail},
		{"qd last Điều cut", qd(true, res, dieu(11, "Điều 1. Ban hành kèm theo Quyết định này Kế hoạch.", "dieu"), dieu(12, cut, "dieu", "tail")), JudgeUndetermined},
		{"qd no Điều", qd(true, res), JudgeUnknown},
	}
	// the untruncated text decides a cut line
	full := qd(true, res, dieu(11, "Điều 1. Ban hành kèm theo Quyết định này Kế hoạch.", "dieu"), dieu(12, cut, "dieu", "tail"))
	full.Anchors[2].Full = strings.TrimSuffix(cut, "…") + " thực hiện; chịu trách nhiệm thi hành Quyết định này./."
	cases = append(cases, struct {
		name string
		r    *Report
		want string
	}{"qd cut line, full text known", full, JudgePass})

	cv := func(trich ...string) *Report {
		return synth("cong_van", map[string][]string{"trich_yeu": trich}, dieu(9, "Đề nghị … thực hiện./.", "closing"))
	}
	cases = append(cases, []struct {
		name string
		r    *Report
		want string
	}{
		{"cv ok", cv("V/v rà soát vị trí việc làm"), JudgePass},
		{"cv CÔNG VĂN line", cv("CÔNG VĂN", "Về việc rà soát"), JudgeFail},
		{"cv Về việc", cv("Về việc rà soát vị trí việc làm"), JudgeWarn},
		{"cv no closing", synth("cong_van", map[string][]string{"trich_yeu": {"V/v x"}}, dieu(9, "Phòng PA05 trao đổi.", "opening")), JudgeWarn},
		{"nq no TM", synth("nghi_quyet", map[string][]string{"chuc_danh": {"CHỦ TỊCH"}},
			dieu(10, "QUYẾT NGHỊ:", "resolution_line"), dieu(11, "Điều 1. Nghị quyết này có hiệu lực từ ngày thông qua./.", "dieu")), JudgeFail},
		{"nq ok", synth("nghi_quyet", map[string][]string{"chuc_danh": {"TM. HỘI ĐỒNG NHÂN DÂN", "CHỦ TỊCH"}},
			dieu(10, "QUYẾT NGHỊ:", "resolution_line"), dieu(11, "Điều 1. Nghị quyết này có hiệu lực từ ngày thông qua./.", "dieu")), JudgePass},
		{"bc short, no headings", synth("bao_cao", nil, dieu(9, "Sở báo cáo …", "opening")), JudgeUnknown},
		{"bc missing tồn tại", synth("bao_cao", nil, dieu(9, "I. TÌNH HÌNH CHUNG", "heading"), dieu(10, "II. KẾT QUẢ", "heading"),
			dieu(11, "III. PHƯƠNG HƯỚNG NHIỆM VỤ", "heading")), JudgeWarn},
		{"bien ban ok", synth("bien_ban", nil, dieu(9, "Vào hồi 8 giờ …", "time_place"), dieu(10, "Thành phần: …", "participants"),
			dieu(11, "Biên bản kết thúc vào hồi 10 giờ …", "end")), JudgePass},
		{"bien ban no end", synth("bien_ban", nil, dieu(9, "Vào hồi 8 giờ …", "time_place"), dieu(10, "Thành phần: …", "participants")), JudgeWarn},
		{"thong bao with Kính gửi", synth("thong_bao", map[string][]string{"kinh_gui": {"Kính gửi: Các phòng."}}), JudgeWarn},
		{"type without requirements", synth("huong_dan", nil), JudgePass},
	}...)
	for _, tc := range cases {
		expectItem(t, tc.name, tc.r, 6, tc.want)
	}
}

func TestJudgmentRulesMerge(t *testing.T) {
	cv, err := LoadRuleSet("cong_van")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]JudgmentRule{}
	for _, jr := range cv.Judgment {
		byID[jr.ID] = jr
	}
	so := byID["so_ky_hieu_ngay_thang"]
	if so.SymbolMode != "no_type_code" || so.Kind != KindSoKyHieuDate || so.Item != 5 {
		t.Errorf("cong_van so_ky_hieu rule = %+v: the type's fields merge over the base entry", so)
	}
	if len(byID["yeu_cau_rieng"].Requirements) == 0 || len(cv.Judgment) != 6 {
		t.Errorf("cong_van judgment = %+v", cv.Judgment)
	}
	bb, _ := LoadRuleSet("bien_ban")
	for _, jr := range bb.Judgment {
		if jr.ID == "ky_thay" && (jr.CollectiveRegex != "" || jr.BadAbbrevRegex == "") {
			t.Errorf("bien_ban ky_thay = %+v", jr)
		}
	}
	for _, typ := range append(AvailableTypes(), "base") {
		rs, err := LoadRuleSet(typ)
		if err != nil {
			t.Fatal(err)
		}
		items := map[int]bool{}
		for _, jr := range rs.Judgment {
			items[jr.Item] = true
			for _, p := range []string{jr.CollectiveRegex, jr.TMExemptRegex, jr.BadAbbrevRegex, jr.ReportRegex,
				jr.LabelRegex, jr.ItemRegex, jr.EndRegex, jr.LuuRegex, jr.LuuVTRegex} {
				if p != "" && jre(p) == nil {
					t.Errorf("%s %s: invalid regex %q", typ, jr.ID, p)
				}
			}
			for _, req := range jr.Requirements {
				for _, p := range []string{req.Regex, req.WarnRegex} {
					if p != "" && jre(p) == nil {
						t.Errorf("%s %s: invalid regex %q", typ, jr.ID, p)
					}
				}
			}
		}
		if len(items) != 6 {
			t.Errorf("%s: judgment covers items %v, want 1-6", typ, items)
		}
	}
}

// fakeCompleter records the messages and answers with a fixed text.
type fakeCompleter struct {
	answer string
	err    error
	calls  [][]Message
}

func (f *fakeCompleter) Complete(_ context.Context, msgs []Message) (string, error) {
	f.calls = append(f.calls, msgs)
	return f.answer, f.err
}

func TestEvaluateRulesOnly(t *testing.T) {
	docs := loadLiveDocs(t)
	for _, d := range docs {
		rep := *d.Report
		model := &fakeCompleter{err: fmt.Errorf("must not be called")}
		text, err := EvaluateWithSkills(context.Background(), model, &rep)
		if err != nil {
			t.Fatalf("%s: %v", d.Name, err)
		}
		if len(model.calls) != 0 || rep.Judgment == nil || rep.Judgment.Method != "rules" {
			t.Fatalf("%s: model calls %d, judgment %+v", d.Name, len(model.calls), rep.Judgment)
		}
		got := ParseChecklist(text)
		if len(got) != 6 {
			t.Fatalf("%s: checklist %v in\n%s", d.Name, got, text)
		}
		for n := 1; n <= 6; n++ {
			if contains(d.Any, n) {
				continue
			}
			if (got[n].Status == JudgeFail) != contains(d.Errors, n) {
				t.Errorf("%s: item %d = %+v", d.Name, n, got[n])
			}
		}
		for _, want := range []string{"## Kết luận", "## Lỗi", "## Cảnh báo", "chính tả"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: evaluation lacks %q", d.Name, want)
			}
		}
		if len(d.Errors) == 0 && !strings.Contains(text, "**Đạt thể thức**") && !hasStatus(d.Report.Checks, StatusFail) {
			t.Errorf("%s: correct document not concluded Đạt:\n%s", d.Name, text)
		}
		if d.Name == "cv_kt_missing" {
			t.Logf("\n%s", text)
			if !strings.Contains(text, `"PHÓ GIÁM ĐỐC" không có dòng "KT. GIÁM ĐỐC" phía trên`) {
				t.Errorf("cv_kt_missing evaluation:\n%s", text)
			}
		}
	}
}

func TestEvaluatePartialLLM(t *testing.T) {
	r := synth("cong_van", map[string][]string{
		"so_ky_hieu":          {"Số: 45/SNV-VP"},
		"dia_danh_ngay_thang": {"Huế, ngày 05 tháng 10 năm 2026"},
		"trich_yeu":           {"V/v rà soát"},
		"chuc_danh":           {"KT. GIÁM ĐỐC", "PHÓ GIÁM ĐỐC"},
		"noi_nhan":            {"Nơi nhận:", "- Đ/c Nguyễn Văn A (để b/c);", "- Lưu: VT, VP."},
	}, dieu(9, "Đề nghị … thực hiện./.", "closing"))
	model := &fakeCompleter{answer: "<think>x</think>## Bảng kiểm\n3. Báo cáo người đứng đầu — Lỗi: \"Đ/c Nguyễn Văn A\" không phải người đứng đầu\n" +
		"## Lỗi\n- Cấp phó ký thay phải báo cáo người đứng đầu → dòng vi phạm: \"- Đ/c Nguyễn Văn A (để b/c);\" → cách sửa: thêm \"- Giám đốc (để b/c);\"\n## Cảnh báo\nKhông có."}
	text, err := EvaluateWithSkills(context.Background(), model, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(model.calls) != 1 {
		t.Fatalf("model calls = %d", len(model.calls))
	}
	sys, user := model.calls[0][0].Content, model.calls[0][1].Content
	if !strings.Contains(user, "Chỉ chấm mục 3.") || !strings.Contains(user, "2. Ký thay — Đạt") ||
		!strings.Contains(user, "# Hồ sơ phán đoán") {
		t.Errorf("user turn:\n%s", user)
	}
	if !strings.Contains(sys, "## 2. Nơi nhận") || strings.Contains(sys, "## 1. Thẩm quyền ký") ||
		strings.Contains(sys, "the-thuc-cong-van") || strings.Contains(sys, "## Cách trả lời") {
		t.Errorf("system turn carries the wrong skill parts:\n%s", sys)
	}
	if j := r.Judgment; j == nil || j.Method != "rules+llm" || len(j.Undetermined) != 1 || j.Undetermined[0] != 3 {
		t.Fatalf("judgment = %+v", r.Judgment)
	}
	got := ParseChecklist(text)
	if got[3].Status != JudgeFail || got[2].Status != JudgePass || !strings.Contains(text, "Nguyễn Văn A (để b/c)") ||
		!strings.Contains(text, "**Chưa đạt thể thức**: 1 lỗi") {
		t.Errorf("merged evaluation:\n%s", text)
	}

	// a failed model call keeps the code report, the item unknown
	r.Judgment = nil
	text, err = EvaluateWithSkills(context.Background(), &fakeCompleter{err: fmt.Errorf("down")}, r)
	if err != nil || r.Judgment.Error == "" || ParseChecklist(text)[3].Status != JudgeUnknown {
		t.Errorf("model down: err %v, judgment %+v\n%s", err, r.Judgment, text)
	}

	// ForceLLM: the whole judgment by the model
	model = &fakeCompleter{answer: "## Kết luận\nĐạt."}
	text, err = EvaluateWithSkillsOpts(context.Background(), model, r, EvalOptions{ForceLLM: true})
	if err != nil || text != "## Kết luận\nĐạt." || r.Judgment.Method != "llm" || len(model.calls) != 1 {
		t.Errorf("ForceLLM: %q %v %+v", text, err, r.Judgment)
	}
}
