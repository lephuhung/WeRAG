package docformat

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// segOf builds a segmentation from component texts ("" components absent).
func segOf(detected string, comps map[string]string) *Segmentation {
	s := &Segmentation{Components: map[string]*Component{}, DetectedType: detected}
	for k, text := range comps {
		s.Components[k] = &Component{Found: true, Text: text}
	}
	return s
}

func TestScopeOfFormCollection(t *testing.T) {
	s := segOf("quyet_dinh", map[string]string{
		"quoc_hieu": "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", "so_ky_hieu": "Số: 12/QĐ-CAT",
		"signature": "GIÁM ĐỐC", "noi_nhan": "Nơi nhận:\n- Như trên;",
	})
	if got, _ := ScopeOf(s, "quyet_dinh"); got != ScopeFull {
		t.Fatalf("one header: %s", got)
	}
	s.QuocHieuLines = 3
	if got, reason := ScopeOf(s, "quyet_dinh"); got != ScopeTechnical || !strings.Contains(reason, "biểu mẫu") {
		t.Fatalf("three headers: %s (%s)", got, reason)
	}
}

func TestScopeOf(t *testing.T) {
	for _, tc := range []struct {
		name, detected string
		comps          map[string]string
		want           string
	}{
		{"luat", "luat", map[string]string{
			"trich_yeu": "LUẬT\nTỔ CHỨC CHÍNH QUYỀN ĐỊA PHƯƠNG",
			"noi_dung":  "Điều 1. Phạm vi điều chỉnh\nLuật này quy định …",
		}, ScopeTechnical},
		{"quy che kem theo", "quy_che", map[string]string{
			"co_quan_ban_hanh": "SỞ NỘI VỤ", "quoc_hieu": "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM",
			"trich_yeu": "QUY CHẾ\nLàm việc của Sở Nội vụ\n(Ban hành kèm theo Quyết định số 125/QĐ-SNV ngày 05/10/2026 của Giám đốc Sở Nội vụ)",
			"noi_dung":  "Chương I\nĐiều 1. Phạm vi điều chỉnh",
		}, ScopeAttached},
		{"ke hoach kem theo, line in body", "ke_hoach", map[string]string{
			"trich_yeu": "KẾ HOẠCH\nChuyển đổi số năm 2026",
			"noi_dung":  "(Kèm theo Quyết định số 12/QĐ-UBND ngày 01 tháng 3 năm 2026 của UBND tỉnh)\nI. MỤC ĐÍCH",
		}, ScopeAttached},
		{"cong van", "cong_van", map[string]string{
			"quoc_hieu": "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", "so_ky_hieu": "Số: 45/SNV-VP",
			"signature": "GIÁM ĐỐC\nLê Thị Dung", "noi_dung": "Thực hiện …",
		}, ScopeFull},
		{"quyet dinh ban hanh kem theo (Điều 1)", "quyet_dinh", map[string]string{
			"quoc_hieu": "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", "so_ky_hieu": "Số: 125/QĐ-SYT",
			"signature": "GIÁM ĐỐC", "trich_yeu": "QUYẾT ĐỊNH\nVề việc ban hành Kế hoạch",
			"noi_dung": "Điều 1. Ban hành kèm theo Quyết định này Kế hoạch …",
		}, ScopeFull},
		{"bare table document", "bao_cao", map[string]string{
			"noi_dung": "STT\nĐơn vị\nSố hồ sơ\n1\nSở Nội vụ\n12",
		}, ScopeTechnical},
		{"unknown type with quốc hiệu", "unknown", map[string]string{
			"quoc_hieu": "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", "noi_dung": "Mẫu số 01 …",
		}, ScopeTechnical},
		{"unknown type, flat header with quốc hiệu and số", "unknown", map[string]string{
			"noi_dung": "SỞ NỘI VỤ\nCỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM\nSố: 12/SNV-VP\nSở Nội vụ đề nghị …",
		}, ScopeFull},
		{"QPPL quyết định (year in the số)", "quyet_dinh", map[string]string{
			"quoc_hieu": "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", "so_ky_hieu": "Số: 26/2024/QĐ-TTg",
			"signature": "KT. THỦ TƯỚNG\nPHÓ THỦ TƯỚNG\nBùi Thanh Sơn", "trich_yeu": "QUYẾT ĐỊNH\nBan hành Quy chế …",
		}, ScopeTechnical},
		{"Quốc hội nghị quyết", "nghi_quyet", map[string]string{
			"quoc_hieu": "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", "so_ky_hieu": "Nghị quyết số: 162/2024/QH15",
			"signature": "CHỦ TỊCH QUỐC HỘI\nTrần Thanh Mẫn",
		}, ScopeTechnical},
		{"unknown type, quốc hiệu and nơi nhận only (a form)", "unknown", map[string]string{
			"quoc_hieu": "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", "noi_nhan": "Nơi nhận:\n- Như trên;",
		}, ScopeTechnical},
		{"flat header: quốc hiệu inside the trích yếu", "thong_bao", map[string]string{
			"trich_yeu": "SỞ Y TẾ\nCỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM\nTHÔNG BÁO", "noi_dung": "Sở Y tế thông báo …",
		}, ScopeFull},
	} {
		got, reason := ScopeOf(segOf(tc.detected, tc.comps), tc.detected)
		if got != tc.want || reason == "" {
			t.Errorf("%s: scope %s (%s), want %s", tc.name, got, reason, tc.want)
		}
	}
}

// technicalIDs are the checks of the technical scope (rules/_base.json).
var technicalIDs = map[string]bool{
	"page.size.width": true, "page.size.height": true, "page.margin.top": true, "page.margin.bottom": true,
	"page.margin.left": true, "page.margin.right": true, "page.number": true, "noi_dung.font": true,
	"noi_dung.size": true, "noi_dung.color": true, "noi_dung.align": true, "noi_dung.first_line_indent": true,
	"noi_dung.line_spacing": true, "text.stray_chars": true,
}

func luatDocx(t *testing.T) []byte {
	t.Helper()
	para := func(text string, center, bold bool) string {
		ppr := `<w:pPr><w:ind w:firstLine="567"/><w:jc w:val="both"/></w:pPr>`
		if center {
			ppr = `<w:pPr><w:jc w:val="center"/></w:pPr>`
		}
		rpr := `<w:sz w:val="28"/>`
		if bold {
			rpr = `<w:b/>` + rpr
		}
		return fmt.Sprintf(`<w:p>%s<w:r><w:rPr>%s</w:rPr><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, ppr, rpr, text)
	}
	body := para("LUẬT", true, true) + para("TỔ CHỨC CHÍNH QUYỀN ĐỊA PHƯƠNG", true, true) +
		para("Căn cứ Hiến pháp nước Cộng hòa xã hội chủ nghĩa Việt Nam;", false, false) +
		para("Quốc hội ban hành Luật Tổ chức chính quyền địa phương.", false, false) +
		para("Điều 1. Phạm vi điều chỉnh", false, true) +
		para("Luật này quy định về đơn vị hành chính và tổ chức, hoạt động của chính quyền địa phương.", false, false) +
		para("Điều 2. Hiệu lực thi hành", false, true) +
		para("Luật này có hiệu lực thi hành từ ngày 01 tháng 7 năm 2025.", false, false)
	header := `<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:fldSimple w:instr=" PAGE "><w:r><w:t>2</w:t></w:r></w:fldSimple></w:p>`
	return testDocx(t, body, header, "", true)
}

func TestCheckTechnicalScope(t *testing.T) {
	r := Check(context.Background(), luatDocx(t), Options{Segmenter: SegmenterHeuristic, SourceName: "luat.docx"})
	if !r.OK {
		t.Fatal(r.Error)
	}
	if r.Scope == nil || r.Scope.Used != ScopeTechnical || r.Scope.Detected != ScopeTechnical {
		t.Fatalf("scope = %+v (type %+v)", r.Scope, r.DocumentType)
	}
	if len(r.Checks) == 0 {
		t.Fatal("no checks")
	}
	for _, c := range r.Checks {
		if !technicalIDs[c.ID] {
			t.Errorf("check %s outside the technical scope", c.ID)
		}
	}
	if len(r.Anchors) != 0 {
		t.Errorf("anchors %+v in the technical scope", r.Anchors)
	}
	model := &fakeCompleter{err: fmt.Errorf("must not be called")}
	text, err := EvaluateWithSkills(context.Background(), model, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(model.calls) != 0 || r.Judgment == nil || r.Judgment.Method != "none" || len(r.Judgment.Items) != 0 {
		t.Fatalf("model calls %d, judgment %+v", len(model.calls), r.Judgment)
	}
	for _, want := range []string{"kỹ thuật trình bày", r.Scope.Reason, scopeChecksNote, "## Lỗi"} {
		if !strings.Contains(text, want) {
			t.Errorf("evaluation lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Bảng kiểm") {
		t.Errorf("technical evaluation has a Bảng kiểm:\n%s", text)
	}

	// a hành chính type named by the user forces the full check
	full := Check(context.Background(), luatDocx(t), Options{Segmenter: SegmenterHeuristic, DocumentType: "cong_van"})
	if full.Scope.Used != ScopeFull || full.Scope.Detected != ScopeTechnical {
		t.Errorf("forced type: scope %+v", full.Scope)
	}
	if c := checkByID(t, full.Checks, "component.quoc_hieu.present"); c.Status != StatusFail {
		t.Errorf("forced full scope: quoc_hieu presence %s", c.Status)
	}
	if forced := Check(context.Background(), luatDocx(t), Options{Segmenter: SegmenterHeuristic, Scope: ScopeFull}); forced.Scope.Used != ScopeFull {
		t.Errorf("Options.Scope full: %+v", forced.Scope)
	}
	if bad := Check(context.Background(), luatDocx(t), Options{Scope: "everything"}); bad.OK {
		t.Error("an unknown scope was accepted")
	}
}

func TestAttachedHeading(t *testing.T) {
	rs, err := LoadRuleSet("quy_che")
	if err != nil {
		t.Fatal(err)
	}
	rs = rs.ForScope(ScopeAttached)
	if len(rs.RequiredComponents) != 0 || len(rs.Order) != 0 || len(rs.Judgment) != 1 || rs.Judgment[0].Kind != KindAttachedHeading {
		t.Fatalf("attached rule set: required %v order %v judgment %+v", rs.RequiredComponents, rs.Order, rs.Judgment)
	}
	for _, c := range rs.Checks {
		if !technicalIDs[c.ID] && !strings.HasPrefix(c.ID, "trich_yeu.") {
			t.Errorf("check %s in the attached scope", c.ID)
		}
	}
	for _, tc := range []struct {
		line, want string
	}{
		{"(Ban hành kèm theo Quyết định số 125/QĐ-SNV ngày 05 tháng 10 năm 2026 của Giám đốc Sở Nội vụ)", JudgePass},
		{"(Ban hành kèm theo Quyết định số     /QĐ-SNV ngày    tháng 10 năm 2026 của Giám đốc Sở Nội vụ)", JudgeWarn},
		{"(Ban hành kèm theo Quyết định của Giám đốc Sở Nội vụ)", JudgeWarn},
	} {
		r := &Report{OK: true, DocumentType: &DocumentTypeInfo{Used: "quy_che"},
			Scope:      &ScopeInfo{Used: ScopeAttached, Detected: ScopeAttached, Reason: "test"},
			Components: map[string]*Component{"trich_yeu": {Found: true, Text: "QUY CHẾ\nLàm việc\n" + tc.line}}}
		items := judgeScoped(r)
		if len(items) != 1 || items[0].Status != tc.want {
			t.Errorf("%q: items %+v, want %s", tc.line, items, tc.want)
		}
	}
}

func TestFormMarker(t *testing.T) {
	el := func(src string, texts ...string) []element {
		var out []element
		for i, s := range texts {
			out = append(out, element{&Para{Index: i, Text: s, Source: src}, ZoneFull, s, -1})
		}
		return out
	}
	for _, tc := range []struct {
		name  string
		els   []element
		found bool
	}{
		{"split over text-box lines", append(el("body", "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM"),
			el("textbox", "Mẫu số: 140", "BH theo TT số 128/2025/TT-BCA", "ngày 19/12/2025")...), true},
		{"one line, ban hành kèm theo", el("body", "Mẫu số 01/BB (Ban hành kèm theo Thông tư số 61/2017/TT-BCA)",
			"CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM"), true},
		{"mẫu biên bản số, nghị định", el("body", "Mẫu biên bản số 01 theo Nghị định số 118/2021/NĐ-CP"), true},
		{"NĐ30 appendix template", el("body", "Mẫu số 1.1 theo Nghị định số 30/2020/NĐ-CP"), false},
		{"no act reference", el("body", "Mẫu số 02", "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM"), false},
		{"công văn", el("body", "UBND TỈNH THỪA THIÊN HUẾ", "Số: 45/SNV-VP", "V/v mẫu số liệu theo Thông tư 01/2024/TT-BNV"), false},
	} {
		if got := formMarker(tc.els); (got != "") != tc.found {
			t.Errorf("%s: formMarker = %q, want found=%v", tc.name, got, tc.found)
		}
	}
	s := segOf("bien_ban", map[string]string{
		"quoc_hieu": "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", "trich_yeu": "BIÊN BẢN GHI LỜI KHAI",
		"signature": "CÁN BỘ GHI LỜI KHAI",
	})
	s.FormMarker = "Mẫu số: 140BH theo TT số 128/2025/TT-BCA"
	if got, reason := ScopeOf(s, "bien_ban"); got != ScopeTechnical || !strings.Contains(reason, "128/2025/TT-BCA") {
		t.Fatalf("form: %s (%s)", got, reason)
	}
}
