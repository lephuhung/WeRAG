package docformat

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

// Văn bản của Đảng (Hướng dẫn 36-HD/VPTW): synthetic documents built like
// nd30_rules_test.go's, with the HD36 page.

// prun is one run of a party test paragraph: flags b (bold), i (italic),
// u (underline).
type prun struct{ text, flags string }

func pr(text, flags string) prun { return prun{text, flags} }

// pp is one paragraph of runs, the text taken verbatim ("*", "-----").
func pp(align string, size float64, runs ...prun) string {
	var b strings.Builder
	b.WriteString("<w:p>")
	if align != "" {
		fmt.Fprintf(&b, `<w:pPr><w:jc w:val="%s"/></w:pPr>`, align)
	}
	for _, r := range runs {
		rpr := ""
		if strings.Contains(r.flags, "b") {
			rpr += "<w:b/>"
		}
		if strings.Contains(r.flags, "i") {
			rpr += "<w:i/>"
		}
		if strings.Contains(r.flags, "u") {
			rpr += `<w:u w:val="single"/>`
		}
		rpr += fmt.Sprintf(`<w:sz w:val="%d"/>`, int(size*2))
		fmt.Fprintf(&b, `<w:r><w:rPr>%s</w:rPr><w:t xml:space="preserve">%s</w:t></w:r>`, rpr, r.text)
	}
	b.WriteString("</w:p>")
	return b.String()
}

// pl is a one-run paragraph.
func pl(text, align string, size float64, flags string) string {
	return pp(align, size, pr(text, flags))
}

// hd36Margins: top 20, right 15, bottom 20, left 30 mm, in twips.
const hd36Margins = `<w:pgMar w:top="1134" w:right="850" w:bottom="1134" w:left="1701" w:header="567" w:footer="567" w:gutter="0"/>`

// withPgMar replaces the test document's margins.
func withPgMar(t *testing.T, content []byte, pgMar string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		if f.Name == "word/document.xml" {
			s := string(data)
			i, j := strings.Index(s, "<w:pgMar"), strings.Index(s, `w:gutter="0"/>`)
			data = []byte(s[:i] + pgMar + s[j+len(`w:gutter="0"/>`):])
		}
		w, _ := zw.Create(f.Name)
		w.Write(data)
	}
	zw.Close()
	return buf.Bytes()
}

func partyDoc(t *testing.T, body string, pgMar string) []byte {
	return withPgMar(t, testDocx(t, body, "", "", false), pgMar)
}

// partyRight is the right header column: tiêu đề, its rule, địa danh.
func partyRight(titleSize float64, date string) string {
	return pl("ĐẢNG CỘNG SẢN VIỆT NAM", "center", titleSize, "b") +
		pl("______________________________", "center", titleSize, "") +
		pl(date, "center", 14, "i")
}

// nghiQuyetTU is a correct Nghị quyết of a tỉnh uỷ's ban thường vụ.
func nghiQuyetTU(t *testing.T) []byte {
	head := ttbl([2]string{
		pl("TỈNH UỶ THỪA THIÊN HUẾ", "center", 14, "b") + pl("*", "center", 14, "") + pl("Số 05-NQ/TU", "center", 14, ""),
		partyRight(15, "Huế, ngày 03 tháng 02 năm 2026"),
	})
	body := pl("NGHỊ QUYẾT", "center", 15, "b") +
		pl("về phát triển kinh tế - xã hội năm 2026", "center", 14, "b") +
		pl("-----", "center", 14, "") +
		pl("- Căn cứ Điều lệ Đảng;", "both", 14, "") +
		pl("- Căn cứ Quy chế làm việc của Tỉnh uỷ khoá XVI,", "both", 14, "") +
		pl("BAN THƯỜNG VỤ TỈNH UỶ QUYẾT NGHỊ:", "center", 14, "b") +
		pp("both", 14, pr("Điều 1. ", "b"), pr("Thông qua kế hoạch phát triển kinh tế - xã hội năm 2026 của tỉnh với các chỉ tiêu chủ yếu đã được hội nghị thảo luận.", "")) +
		pp("both", 14, pr("Điều 2. ", "b"), pr("Nghị quyết có hiệu lực từ ngày ký; các cấp uỷ, tổ chức đảng trực thuộc chịu trách nhiệm thi hành.", ""))
	tail := ttbl([2]string{
		pl("Nơi nhận:", "", 14, "u") + pl("- Ban Bí thư Trung ương,", "", 12, "") +
			pl("- Các huyện uỷ, thị uỷ, đảng uỷ trực thuộc,", "", 12, "") + pl("- Lưu Văn phòng Tỉnh uỷ.", "", 12, ""),
		pl("T/M BAN THƯỜNG VỤ", "center", 14, "b") + pl("BÍ THƯ", "center", 14, "") + pl("", "center", 14, "") + pl("Lê Văn Ánh", "center", 14, "b"),
	})
	return partyDoc(t, head+body+tail, hd36Margins)
}

// congVanBTC is a correct Công văn of a ban tổ chức tỉnh uỷ.
func congVanBTC(t *testing.T) []byte {
	head := ttbl([2]string{
		pl("TỈNH UỶ THỪA THIÊN HUẾ", "center", 14, "") + pl("BAN TỔ CHỨC", "center", 14, "b") + pl("*", "center", 14, "") +
			pl("Số 357-CV/BTCTU", "center", 14, "") +
			pl("Chuẩn bị hội nghị trực tuyến", "center", 12, "i") + pl("quán triệt Nghị quyết Trung ương", "center", 12, "i"),
		partyRight(15, "Huế, ngày 15 tháng 9 năm 2026"),
	})
	body := pp("center", 14, pr("Kính gửi: ", "i"), pr("Các huyện uỷ, thị uỷ, đảng uỷ trực thuộc Tỉnh uỷ.", "")) +
		pl("Thực hiện chỉ đạo của Ban Thường vụ Tỉnh uỷ, Ban Tổ chức Tỉnh uỷ đề nghị các cấp uỷ chuẩn bị điểm cầu hội nghị trực tuyến quán triệt Nghị quyết.", "both", 14, "") +
		pl("Đề nghị các cấp uỷ quan tâm, phối hợp thực hiện.", "both", 14, "")
	tail := ttbl([2]string{
		pl("Nơi nhận:", "", 14, "u") + pl("- Như trên,", "", 12, "") + pl("- Thường trực Tỉnh uỷ (để báo cáo),", "", 12, "") +
			pl("- Lưu Ban Tổ chức.", "", 12, ""),
		pl("K/T TRƯỞNG BAN", "center", 14, "b") + pl("PHÓ TRƯỞNG BAN", "center", 14, "") + pl("", "center", 14, "") +
			pl("Nguyễn Thị Bình", "center", 14, "b"),
	})
	return partyDoc(t, head+body+tail, hd36Margins)
}

// baoCaoBad is a Báo cáo of a tỉnh uỷ with NĐ30 habits and HD36 errors:
// "Số:" with a colon, "TM.", nơi nhận lines ending ";", no "*", the
// tiêu đề in 13 pt, the right margin 20 mm.
func baoCaoBad(t *testing.T) []byte {
	head := ttbl([2]string{
		pl("TỈNH UỶ THỪA THIÊN HUẾ", "center", 14, "b") + pl("Số: 12-BC/TU", "center", 14, ""),
		partyRight(13, "Huế, ngày 20 tháng 9 năm 2026"),
	})
	body := pl("BÁO CÁO", "center", 15, "b") +
		pl("kết quả thực hiện nhiệm vụ chính trị 9 tháng năm 2026", "center", 14, "b") +
		pl("-----", "center", 14, "") +
		pl("Trong 9 tháng năm 2026, Ban Thường vụ Tỉnh uỷ đã lãnh đạo, chỉ đạo thực hiện đạt kết quả các nhiệm vụ chính trị.", "both", 14, "") +
		pl("Trên đây là báo cáo kết quả thực hiện nhiệm vụ chính trị 9 tháng năm 2026.", "both", 14, "")
	tail := ttbl([2]string{
		pl("Nơi nhận:", "", 14, "u") + pl("- Ban Bí thư Trung ương;", "", 12, "") + pl("- Lưu Văn phòng Tỉnh uỷ.", "", 12, ""),
		pl("TM. BAN THƯỜNG VỤ", "center", 14, "b") + pl("BÍ THƯ", "center", 14, "") + pl("", "center", 14, "") + pl("Lê Văn Ánh", "center", 14, "b"),
	})
	margins := strings.Replace(hd36Margins, `w:right="850"`, `w:right="1134"`, 1)
	return partyDoc(t, head+body+tail, margins)
}

func checkStatuses(r *Report) map[string]string {
	out := map[string]string{}
	for _, c := range r.Checks {
		out[c.ID] = c.Status
	}
	return out
}

func judged(r *Report) map[int]JudgmentItem {
	out := map[int]JudgmentItem{}
	for _, it := range Judge(r) {
		out[it.Item] = it
	}
	return out
}

func assertParty(t *testing.T, r *Report, typ, ruleSet string) {
	t.Helper()
	if !r.OK {
		t.Fatalf("check failed: %s", r.Error)
	}
	if r.DocumentType.Detected != typ || r.DocumentType.Used != typ || !r.Party() {
		t.Errorf("type = %+v, want %s", r.DocumentType, typ)
	}
	if !strings.Contains(r.DocumentType.RuleSet, ruleSet) || !strings.Contains(r.DocumentType.RuleSet, PartyGuide) {
		t.Errorf("rule set = %q", r.DocumentType.RuleSet)
	}
	if r.Scope.Used != ScopeFull || !strings.Contains(r.Scope.Reason, PartyGuide) {
		t.Errorf("scope = %+v", r.Scope)
	}
	if !r.Components["tieu_de_dang"].Found || r.Components["quoc_hieu"] != nil {
		t.Errorf("tiêu đề = %+v, quốc hiệu = %+v", r.Components["tieu_de_dang"], r.Components["quoc_hieu"])
	}
	for _, id := range []string{"quoc_hieu.zone", "tieu_ngu.zone", "component.quoc_hieu.present"} {
		if _, ok := checkStatuses(r)[id]; ok {
			t.Errorf("NĐ30 check %s ran on a party document", id)
		}
	}
	if len(r.Skills) != 1 || r.Skills[0] != PartySkill {
		t.Errorf("skills = %v", r.Skills)
	}
	if strings.Contains(r.Skeleton, "NĐ30") || strings.Contains(RenderText(r), "NĐ30") {
		t.Errorf("a party report names NĐ30:\n%s", r.Skeleton)
	}
}

func TestPartyNghiQuyet(t *testing.T) {
	r := Check(context.Background(), nghiQuyetTU(t), Options{Segmenter: SegmenterHeuristic})
	assertParty(t, r, "dang_nghi_quyet", "Nghị quyết của Đảng")
	if got := r.Components["co_quan_ban_hanh"].Text; got != "TỈNH UỶ THỪA THIÊN HUẾ" {
		t.Errorf("cơ quan ban hành = %q (the \"*\" line is no part of it)", got)
	}
	if got := r.Components["can_cu"].Text; !strings.HasPrefix(got, "- Căn cứ Điều lệ Đảng;") || !strings.HasSuffix(got, "khoá XVI,") {
		t.Errorf("căn cứ = %q", got)
	}
	st := checkStatuses(r)
	for _, id := range []string{"co_quan_ban_hanh.star", "trich_yeu.dashes", "tieu_de_dang.rule", "tieu_de_dang.size",
		"tieu_de_dang.zone", "co_quan_ban_hanh.size", "so_ky_hieu.size", "dia_danh.italic", "trich_yeu.type.size",
		"page.margin.top", "page.margin.right", "page.margin.left", "page.margin.bottom",
		"noi_nhan.label.underline", "noi_nhan.label.bold", "noi_nhan.list.size", "chuc_danh.quyen_han.bold",
		"chuc_danh.chuc_vu.bold", "nguoi_ky.bold", "noi_dung.dieu.bold", "component.order", "component.tieu_de_dang.present"} {
		if st[id] != StatusPass {
			t.Errorf("%s = %s, want pass", id, st[id])
		}
	}
	for _, c := range r.Checks {
		if c.Status == StatusFail {
			t.Errorf("failed: %s %v %v", c.ID, c.Actual, c.Evidence)
		}
	}
	for n, it := range judged(r) {
		if it.Status != JudgePass {
			t.Errorf("item %d %s = %s %+v %s", n, it.Title, it.Status, it.Findings, it.Reason)
		}
	}
	text, err := EvaluateWithSkills(context.Background(), nil, r)
	if err != nil || r.Judgment.Method != "rules" || !strings.Contains(text, "**Đạt thể thức**") ||
		!strings.Contains(text, "2. Quyền hạn ký (T/M, K/T, T/L) — Đạt") || strings.Contains(text, "NĐ30") {
		t.Errorf("evaluation (%v, %+v):\n%s", err, r.Judgment, text)
	}
}

func TestPartyCongVan(t *testing.T) {
	r := Check(context.Background(), congVanBTC(t), Options{Segmenter: SegmenterHeuristic})
	assertParty(t, r, "dang_cong_van", "Công văn của Đảng")
	if c := r.Components["trich_yeu"]; c.Zone != ZoneLeft || c.Text != "Chuẩn bị hội nghị trực tuyến\nquán triệt Nghị quyết Trung ương" {
		t.Errorf("trích yếu = %+v", c)
	}
	if r.Components["co_quan_chu_quan"].Text != "TỈNH UỶ THỪA THIÊN HUẾ" || r.Components["co_quan_ban_hanh"].Text != "BAN TỔ CHỨC" {
		t.Errorf("cơ quan = %q / %q", r.Components["co_quan_chu_quan"].Text, r.Components["co_quan_ban_hanh"].Text)
	}
	if !r.Components["kinh_gui"].Found {
		t.Error("no Kính gửi")
	}
	st := checkStatuses(r)
	for _, id := range []string{"trich_yeu.zone", "trich_yeu.size", "trich_yeu.italic", "trich_yeu.bold", "kinh_gui.label.italic",
		"co_quan_ban_hanh.star", "co_quan_chu_quan.bold", "co_quan_ban_hanh.bold", "component.kinh_gui.present"} {
		if st[id] != StatusPass {
			t.Errorf("%s = %s, want pass", id, st[id])
		}
	}
	for _, id := range []string{"trich_yeu.align", "trich_yeu.dashes", "trich_yeu.type.size"} {
		if _, ok := st[id]; ok {
			t.Errorf("%s is disabled for a công văn", id)
		}
	}
	for _, c := range r.Checks {
		if c.Status == StatusFail {
			t.Errorf("failed: %s %v %v", c.ID, c.Actual, c.Evidence)
		}
	}
	for n, it := range judged(r) {
		if it.Status != JudgePass {
			t.Errorf("item %d %s = %s %+v %s", n, it.Title, it.Status, it.Findings, it.Reason)
		}
	}
}

func TestPartyBaoCaoErrors(t *testing.T) {
	r := Check(context.Background(), baoCaoBad(t), Options{Segmenter: SegmenterHeuristic})
	assertParty(t, r, "dang_bao_cao", "Báo cáo của Đảng")
	st := checkStatuses(r)
	for id, want := range map[string]string{
		"co_quan_ban_hanh.star": StatusFail, "tieu_de_dang.size": StatusWarn, "page.margin.right": StatusFail,
		"page.margin.left": StatusPass, "trich_yeu.dashes": StatusPass, "tieu_de_dang.rule": StatusPass,
	} {
		if st[id] != want {
			t.Errorf("%s = %s, want %s", id, st[id], want)
		}
	}
	items := judged(r)
	findings := func(n int) string {
		var s []string
		for _, f := range items[n].Findings {
			s = append(s, f.Summary)
		}
		return strings.Join(s, " | ")
	}
	if it := items[2]; it.Status != JudgeFail || !strings.Contains(findings(2), `"T/M BAN THƯỜNG VỤ"`) {
		t.Errorf("item 2 = %s: %s", it.Status, findings(2))
	}
	if it := items[4]; it.Status != JudgeFail || !strings.Contains(findings(4), "Ban Bí thư Trung ương;") {
		t.Errorf("item 4 = %s: %s", it.Status, findings(4))
	}
	if it := items[5]; it.Status != JudgeFail || !strings.Contains(findings(5), "dấu hai chấm") {
		t.Errorf("item 5 = %s: %s", it.Status, findings(5))
	}
	if items[1].Status != JudgePass || items[3].Status != JudgePass {
		t.Errorf("items 1, 3 = %s, %s", items[1].Status, items[3].Status)
	}
	text, err := EvaluateWithSkills(context.Background(), nil, r)
	if err != nil || !strings.Contains(text, "**Chưa đạt thể thức**") || !strings.Contains(text, "Theo "+PartyGuide+": Dưới tên cơ quan ban hành có dấu sao") ||
		strings.Contains(text, "NĐ30") {
		t.Errorf("evaluation (%v):\n%s", err, text)
	}
}

// The model labels the tiêu đề as quốc hiệu: still a party document.
func TestPartyFromLabels(t *testing.T) {
	content := nghiQuyetTU(t)
	l := InspectDocx(content)
	task := PrepareFullTask(l, Segment(l), nil)
	labels := map[string]string{}
	for _, u := range task.All {
		lab := u.Hint
		if lab == "tieu_de_dang" {
			lab = "quoc_hieu"
		}
		if lab != "" {
			labels[u.ID] = lab
		}
	}
	r := Check(context.Background(), content, Options{Labels: &Reply{DocumentType: "nghi_quyet", Labels: labels, NotND30: true}})
	assertParty(t, r, "dang_nghi_quyet", "Nghị quyết của Đảng")
	if r.Segmentation.Method != SegmenterLabels {
		t.Errorf("method = %s", r.Segmentation.Method)
	}
	// an NĐ30 type the user names maps to its party counterpart
	r = Check(context.Background(), content, Options{DocumentType: "nghi_quyet", Segmenter: SegmenterHeuristic})
	if r.DocumentType.Used != "dang_nghi_quyet" || r.DocumentType.Requested != "nghi_quyet" {
		t.Errorf("requested nghi_quyet on a party document: %+v", r.DocumentType)
	}
}

// An NĐ30 document quoting "Đảng Cộng sản Việt Nam" stays an NĐ30 one.
func TestNotPartyWhenQuoted(t *testing.T) {
	head := ttbl(
		[2]string{tp("*SỞ NỘI VỤ", "center", 13), tp("*CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", "center", 13) + tp("*Độc lập - Tự do - Hạnh phúc", "center", 14)},
		[2]string{tp("Số: 45/SNV-VP", "center", 13) + tp("V/v tuyên truyền ngày thành lập Đảng", "center", 12), tp("_Huế, ngày 05 tháng 01 năm 2026", "center", 14)},
	)
	body := tp("Kính gửi: Các phòng, ban thuộc Sở.", "center", 14) +
		tp("Nhân kỷ niệm ngày thành lập Đảng Cộng sản Việt Nam, Sở Nội vụ đề nghị các phòng, ban tổ chức tuyên truyền.", "both", 14) +
		tp("ĐẢNG CỘNG SẢN VIỆT NAM", "center", 14) +
		tp("Đề nghị các phòng, ban thực hiện./.", "both", 14)
	tail := ttbl([2]string{tp("*_Nơi nhận:", "", 12) + tp("- Như trên;", "", 11) + tp("- Lưu: VT, VP.", "", 11),
		tp("*GIÁM ĐỐC", "center", 14) + tp("", "center", 14) + tp("*Lê Thị Dung", "center", 14)})
	r := Check(context.Background(), testDocx(t, head+body+tail, "", "", false), Options{Segmenter: SegmenterHeuristic})
	if r.Party() || r.DocumentType.Used != "cong_van" || r.Components["tieu_de_dang"] != nil {
		t.Fatalf("NĐ30 công văn read as a party document: %+v", r.DocumentType)
	}
	if r.Scope.Used != ScopeFull || strings.Contains(r.DocumentType.RuleSet, PartyGuide) || r.Skills[0] != GeneralSkill {
		t.Errorf("scope %+v, rule set %q, skills %v", r.Scope, r.DocumentType.RuleSet, r.Skills)
	}
}

func TestPartyTypeDetection(t *testing.T) {
	for so, want := range map[string]string{
		"Số 15-HD/TU": "dang_huong_dan", "Số 05-QĐi/TU": "dang_quy_dinh", "Số 246-QĐ/BKTTW": "dang_quyet_dinh",
		"Số 01-CTr/TU": "dang_chuong_trinh", "Số 02-CT/TU": "dang_chi_thi", "Số 03-TTr/BTCTU": "dang_to_trinh",
		"Số 04-TT/TU": "dang_thong_tri", "Số 05-QC/BTCTU-BDVTU": "dang_quy_che", "Số: 12/BC-TU": "",
	} {
		if got := partyTypeFromSymbol(so); got != want {
			t.Errorf("%s → %q, want %q", so, got, want)
		}
	}
	for line, want := range map[string]string{"KẾT LUẬN": "dang_ket_luan", "QUY ĐỊNH": "dang_quy_dinh",
		"QUYẾT ĐỊNH": "dang_quyet_dinh", "THÔNG TRI": "dang_thong_tri", "về công tác cán bộ": ""} {
		if got := partyTypeFromLine(line); got != want {
			t.Errorf("%s → %q, want %q", line, got, want)
		}
	}
	// every party type loads, over _dang_base, and is listed
	types := strings.Join(AvailableTypes(), ",")
	for _, typ := range append(PartyTypes(), PartyGeneric) {
		rs, err := LoadRuleSet(ruleSetName(typ))
		if err != nil || rs.CheckByID("co_quan_ban_hanh.star") == nil || rs.CheckByID("quoc_hieu.zone") != nil {
			t.Errorf("%s: %v", typ, err)
		}
		if typ != PartyGeneric && !strings.Contains(types, typ) {
			t.Errorf("%s not in AvailableTypes", typ)
		}
	}
}
