package docformat

import (
	"context"
	"strings"
	"testing"
)

// Review fixes of the HD36 checker.

// partyReport is a report of a party type made of component lines (zone
// right for chức danh / người ký, left otherwise), for the judgment kinds.
func partyReport(used string, comps map[string][]string) *Report {
	r := &Report{OK: true, DocumentType: &DocumentTypeInfo{Used: used}, Components: map[string]*Component{}}
	for key, lines := range comps {
		zone := ZoneLeft
		if key == "chuc_danh" || key == "nguoi_ky" {
			zone = ZoneRight
		}
		cf := ComponentFormat{Key: key}
		for i, t := range lines {
			cf.Lines = append(cf.Lines, LineFormat{Para: i, Text: t, Zone: zone})
		}
		r.Format = append(r.Format, cf)
		r.Components[key] = &Component{Found: true}
	}
	return r
}

func judgmentRule(t *testing.T, used, id string) JudgmentRule {
	t.Helper()
	rs, err := LoadRuleSet(ruleSetName(used))
	if err != nil {
		t.Fatal(err)
	}
	for _, jr := range rs.Judgment {
		if jr.ID == id {
			return jr
		}
	}
	t.Fatalf("%s has no judgment rule %s", used, id)
	return JudgmentRule{}
}

// 1. The tiêu đề as one line of a paragraph read line by line: only that
// line leaves the other components.
func TestPartyTitleSubLine(t *testing.T) {
	seg := &Segmentation{Components: map[string]*Component{}, ParaLabels: map[int]string{}}
	seg.add("dia_danh_ngay_thang", 5, "Huế, ngày 03 tháng 02 năm 2026", ZoneRight, 3, 1)
	seg.add("header_right_other", 5, "ĐẢNG CỘNG SẢN VIỆT NAM", ZoneRight, 2, 0)
	seg.dropPara(5, 0, "ĐẢNG CỘNG SẢN VIỆT NAM")
	if c := seg.Comp("dia_danh_ngay_thang"); !c.Found || c.Text != "Huế, ngày 03 tháng 02 năm 2026" || fmtInts(c.Subs[5]) != "1" {
		t.Errorf("date line lost: %+v", c)
	}
	if seg.Comp("header_right_other").Found {
		t.Errorf("title line kept: %+v", seg.Comp("header_right_other"))
	}

	// end to end: "ĐẢNG CỘNG SẢN VIỆT NAM⏎Huế, ngày …" in the right cell,
	// the model labelling the title khac and the date line dia_danh
	right := `<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:rPr><w:b/><w:sz w:val="30"/></w:rPr><w:t>ĐẢNG CỘNG SẢN VIỆT NAM</w:t></w:r>` +
		`<w:r><w:br/></w:r><w:r><w:rPr><w:i/><w:sz w:val="28"/></w:rPr><w:t>Huế, ngày 03 tháng 02 năm 2026</w:t></w:r></w:p>`
	doc := strings.Replace(string(nghiQuyetTUBody()), "@RIGHT@", right, 1)
	content := partyDoc(t, doc, hd36Margins)
	l := InspectDocx(content)
	heur := Segment(l)
	if !heur.Comp("tieu_de_dang").Found || !heur.Comp("dia_danh_ngay_thang").Found {
		t.Fatalf("heuristic: title %+v date %+v", heur.Comp("tieu_de_dang"), heur.Comp("dia_danh_ngay_thang"))
	}
	task := PrepareFullTask(l, heur, nil)
	labels := map[string]string{}
	for _, u := range task.All {
		lab := u.Hint
		if lab == "tieu_de_dang" {
			lab = "khac"
		}
		if lab != "" {
			labels[u.ID] = lab
		}
	}
	r := Check(context.Background(), content, Options{Labels: &Reply{Labels: labels}})
	if !r.Party() || !r.Components["tieu_de_dang"].Found || !r.Components["dia_danh_ngay_thang"].Found {
		t.Fatalf("type %+v, components %v", r.DocumentType, r.Components)
	}
	if got := checkStatuses(r)["component.dia_danh_ngay_thang.present"]; got != StatusPass {
		t.Errorf("date present = %s", got)
	}
}

func fmtInts(xs []int) string {
	var s []string
	for _, x := range xs {
		s = append(s, string(rune('0'+x)))
	}
	return strings.Join(s, ",")
}

// nghiQuyetTUBody is the Nghị quyết of nghiQuyetTU with its right header
// cell left as "@RIGHT@".
func nghiQuyetTUBody() string {
	head := ttbl([2]string{
		pl("TỈNH UỶ THỪA THIÊN HUẾ", "center", 14, "b") + pl("*", "center", 14, "") + pl("Số 05-NQ/TU", "center", 14, ""),
		"@RIGHT@",
	})
	body := pl("NGHỊ QUYẾT", "center", 15, "b") + pl("về phát triển kinh tế - xã hội năm 2026", "center", 14, "b") +
		pl("-----", "center", 14, "") +
		pp("both", 14, pr("Điều 1. ", "b"), pr("Thông qua kế hoạch phát triển kinh tế - xã hội năm 2026.", ""))
	tail := ttbl([2]string{
		pl("Nơi nhận:", "", 14, "u") + pl("- Ban Bí thư Trung ương,", "", 12, "") + pl("- Lưu Văn phòng Tỉnh uỷ.", "", 12, ""),
		pl("T/M BAN THƯỜNG VỤ", "center", 14, "b") + pl("BÍ THƯ", "center", 14, "") + pl("Lê Văn Ánh", "center", 14, "b"),
	})
	return head + body + tail
}

// 2. A "kiêm" signer, or a deputy of a hội đồng / ban chỉ đạo / tiểu ban,
// signs without "T/M" (HD36 p.24).
func TestPartyCollectiveExemptions(t *testing.T) {
	jr := judgmentRule(t, "dang_huong_dan", "tham_quyen_ky")
	run := func(agency []string, sig ...string) ruleOut {
		r := partyReport("dang_huong_dan", map[string][]string{"co_quan_ban_hanh": agency, "chuc_danh": sig, "nguoi_ky": {"Nguyễn Văn A"}})
		return newJudgeData(r).dangThamQuyen(jr)
	}
	if o := run([]string{"TỈNH UỶ THỪA THIÊN HUẾ", "HỘI ĐỒNG LÝ LUẬN"}, "TRƯỞNG BAN TUYÊN GIÁO", "kiêm", "PHÓ CHỦ TỊCH HỘI ĐỒNG"); len(o.findings) != 0 {
		t.Errorf("kiêm signer: %+v", o.findings)
	}
	if o := run([]string{"TỈNH UỶ THỪA THIÊN HUẾ", "HỘI ĐỒNG LÝ LUẬN"}, "PHÓ CHỦ TỊCH"); len(o.findings) != 0 {
		t.Errorf("phó chủ tịch hội đồng: %+v", o.findings)
	}
	if o := run([]string{"TỈNH UỶ THỪA THIÊN HUẾ", "HỘI ĐỒNG LÝ LUẬN"}, "CHỦ TỊCH"); len(o.findings) != 1 {
		t.Errorf("chủ tịch hội đồng must sign T/M: %+v", o.findings)
	}
	if o := run([]string{"TỈNH UỶ THỪA THIÊN HUẾ"}, "PHÓ BÍ THƯ"); len(o.findings) != 1 {
		t.Errorf("tỉnh uỷ without T/M: %+v", o.findings)
	}
}

// 3. The generic party type loads the-thuc-dang once.
func TestPartyGenericSkillOnce(t *testing.T) {
	if s := SkillsFor(PartyGeneric); len(s) != 1 || s[0].Name != PartySkill {
		t.Errorf("SkillsFor(dang) = %+v", s)
	}
	r := partyReport(PartyGeneric, map[string][]string{})
	msgs := PartialEvaluationMessages(r, nil, []int{6}, EvalOptions{Render: func(*Report) string { return "" }})
	if n := strings.Count(msgs[0].Content, `<skill name="the-thuc-dang"`); n != 1 {
		t.Errorf("the-thuc-dang sent %d times", n)
	}
}

// 4. Nơi nhận: not required → pass; an undashed line continues its entry.
func TestPartyNoiNhanFixes(t *testing.T) {
	jr := judgmentRule(t, "dang_bien_ban", "noi_nhan")
	o := newJudgeData(partyReport("dang_bien_ban", map[string][]string{})).dangNoiNhan(jr)
	if o.unknown != "" || len(o.findings) != 0 || o.note == "" {
		t.Errorf("biên bản without nơi nhận: %+v", o)
	}
	jr = judgmentRule(t, "dang_bao_cao", "noi_nhan")
	o = newJudgeData(partyReport("dang_bao_cao", map[string][]string{})).dangNoiNhan(jr)
	if o.unknown == "" {
		t.Errorf("báo cáo without nơi nhận is unknown (item 1 fails it): %+v", o)
	}
	r := partyReport("dang_bao_cao", map[string][]string{"noi_nhan": {"Nơi nhận:", "- Các ban, đảng uỷ,", "hội đồng trực thuộc,",
		"- Báo, tạp chí trực thuộc,", "- Lưu Văn phòng Tỉnh uỷ."}})
	if o := newJudgeData(r).dangNoiNhan(jr); len(o.findings) != 0 {
		t.Errorf("wrapped entry: %+v", o.findings)
	}
}

// 5. A nghị quyết's draft mark or stray line in the left column does not
// open a công văn trích yếu.
func TestPartyLeftLinesKeepHeading(t *testing.T) {
	head := ttbl([2]string{
		pl("TỈNH UỶ THỪA THIÊN HUẾ", "center", 14, "b") + pl("*", "center", 14, "") + pl("Số 05-NQ/TU", "center", 14, "") +
			pl("(Dự thảo lần 2)", "center", 14, "i") + pl("Tài liệu phục vụ Hội nghị", "center", 14, "") +
			pl("kỳ họp thường lệ tháng 01", "center", 14, ""),
		partyRight(15, "Huế, ngày 03 tháng 02 năm 2026"),
	})
	doc := strings.Replace(nghiQuyetTUBody(), nghiQuyetTUBody()[:strings.Index(nghiQuyetTUBody(), "</w:tbl>")+len("</w:tbl>")], head, 1)
	r := Check(context.Background(), partyDoc(t, doc, hd36Margins), Options{Segmenter: SegmenterHeuristic})
	if r.DocumentType.Used != "dang_nghi_quyet" {
		t.Errorf("type = %+v", r.DocumentType)
	}
	if ty := r.Components["trich_yeu"]; ty == nil || !strings.HasPrefix(ty.Text, "NGHỊ QUYẾT") || ty.Zone != ZoneFull {
		t.Errorf("trích yếu = %+v", ty)
	}
	if cd := r.Components["chi_dan_luu_hanh"]; cd == nil || !strings.Contains(cd.Text, "(Dự thảo lần 2)") || !strings.Contains(cd.Text, "Tài liệu phục vụ") {
		t.Errorf("chỉ dẫn = %+v", cd)
	}
	if st := checkStatuses(r); st["chi_dan_du_thao.italic"] != StatusPass {
		t.Errorf("dự thảo italic = %s", st["chi_dan_du_thao.italic"])
	}
}

// 6. The separator under the trích yếu: missing is an error, an off count
// of hyphens only a warning.
func TestPartyDashes(t *testing.T) {
	run := func(sep string) map[string]string {
		doc := strings.Replace(nghiQuyetTUBody(), "@RIGHT@", partyRight(15, "Huế, ngày 03 tháng 02 năm 2026"), 1)
		doc = strings.Replace(doc, pl("-----", "center", 14, ""), sep, 1)
		return checkStatuses(Check(context.Background(), partyDoc(t, doc, hd36Margins), Options{Segmenter: SegmenterHeuristic}))
	}
	for sep, want := range map[string][2]string{
		pl("-----", "center", 14, ""):     {StatusPass, StatusPass},
		pl("- - - - -", "center", 14, ""): {StatusPass, StatusPass},
		pl("---", "center", 14, ""):       {StatusPass, StatusWarn},
		pl("———", "center", 14, ""):       {StatusPass, StatusWarn},
		"":                                {StatusFail, StatusSkip},
	} {
		st := run(sep)
		if st["trich_yeu.dashes"] != want[0] || st["trich_yeu.dashes.count"] != want[1] {
			t.Errorf("%q: dashes %s, count %s; want %v", sep, st["trich_yeu.dashes"], st["trich_yeu.dashes.count"], want)
		}
	}
}

// 7. A party report names its full scope as a party document's.
func TestPartyScopeLabel(t *testing.T) {
	r := Check(context.Background(), nghiQuyetTU(t), Options{Segmenter: SegmenterHeuristic})
	if got := r.ScopeLabel(); got != "đầy đủ (văn bản của Đảng)" {
		t.Errorf("party scope label = %q", got)
	}
	nd30 := &Report{DocumentType: &DocumentTypeInfo{Used: "cong_van"}, Scope: &ScopeInfo{Used: ScopeFull}}
	if got := nd30.ScopeLabel(); got != ScopeName(ScopeFull) {
		t.Errorf("NĐ30 scope label = %q", got)
	}
	tech := &Report{DocumentType: &DocumentTypeInfo{Used: "dang_bao_cao"}, Scope: &ScopeInfo{Used: ScopeTechnical, Reason: "x"}}
	if got := tech.ScopeLabel(); got != ScopeName(ScopeTechnical) {
		t.Errorf("party technical label = %q", got)
	}
	if out := BuildScopedEvaluation(tech, nil); !strings.Contains(out, partyScopeChecksNote) || strings.Contains(out, "NĐ30") {
		t.Errorf("scoped party evaluation:\n%s", out)
	}
}
