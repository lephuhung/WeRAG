package docformat

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// componentNames are the Vietnamese names of the components, for reports.
var componentNames = map[string]string{
	"quoc_hieu":           "Quốc hiệu",
	"tieu_ngu":            "Tiêu ngữ",
	"co_quan_chu_quan":    "Cơ quan chủ quản",
	"co_quan_ban_hanh":    "Cơ quan ban hành",
	"so_ky_hieu":          "Số, ký hiệu",
	"dia_danh_ngay_thang": "Địa danh, ngày tháng",
	"do_mat":              "Độ mật",
	"do_khan":             "Độ khẩn",
	"trich_yeu":           "Tên loại, trích yếu",
	"tham_quyen_ban_hanh": "Thẩm quyền ban hành",
	"can_cu":              "Căn cứ",
	"kinh_gui":            "Kính gửi",
	"noi_dung":            "Nội dung",
	"chuc_danh":           "Chức vụ người ký",
	"nguoi_ky":            "Họ tên người ký",
	"signature":           "Khối chữ ký",
	"noi_nhan":            "Nơi nhận",
	"chi_dan_luu_hanh":    "Chỉ dẫn phạm vi lưu hành",
	"ky_hieu_soan_thao":   "Ký hiệu người soạn thảo, số bản",
	"dia_chi_co_quan":     "Địa chỉ cơ quan",
	"phu_luc":             "Phụ lục",
}

// minorComponents (ô 11, 12, 13) are rarely present and never required:
// the judgment brief does not list them as missing.
var minorComponents = map[string]bool{"chi_dan_luu_hanh": true, "ky_hieu_soan_thao": true, "dia_chi_co_quan": true}

// ComponentName returns the Vietnamese name of a component key.
func ComponentName(key string) string {
	if n, ok := componentNames[key]; ok {
		return n
	}
	return key
}

func compactJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// RenderText renders a report as compact text for an agent to explain to
// the user: findings first (fail, then warn), each with the rule, the
// measured value and the offending lines; then what could not be measured
// and the components found.
func RenderText(r *Report) string {
	var b strings.Builder
	if !r.OK {
		fmt.Fprintf(&b, "Không kiểm tra được %s: %s\n", r.Source, r.Error)
		return b.String()
	}
	dt := r.DocumentType
	fmt.Fprintf(&b, "Văn bản: %s\nLoại văn bản: %s — bộ luật: %s\n", r.Source, dt.Detected, dt.RuleSet)
	if !r.FullScope() {
		fmt.Fprintf(&b, "Phạm vi kiểm tra: %s (%s) — %s\n", ScopeName(r.Scope.Used), r.Scope.Used, r.Scope.Reason)
	}
	if s := r.Segmentation; s != nil {
		fmt.Fprintf(&b, "Gán thành phần: %s", s.Method)
		if s.Model != "" {
			fmt.Fprintf(&b, " (%s)", s.Model)
		}
		if s.Error != "" {
			fmt.Fprintf(&b, " — %s", s.Error)
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "Kết quả: %d đạt, %d sai, %d cảnh báo, %d không đo được\n",
		r.Summary.Pass, r.Summary.Fail, r.Summary.Warn, r.Summary.Skip)

	for _, status := range []string{StatusFail, StatusWarn} {
		title := map[string]string{StatusFail: "\n## Lỗi (sai thể thức)\n", StatusWarn: "\n## Cảnh báo (nên sửa)\n"}[status]
		if hasStatus(r.Checks, status) {
			b.WriteString(title)
			writeFindings(&b, r.Checks, status)
		}
	}
	var skipped []string
	for _, c := range r.Checks {
		if c.Status == StatusSkip && c.Severity == "error" && c.Note != "" &&
			!strings.HasPrefix(c.Note, "thành phần không bắt buộc") {
			skipped = append(skipped, c.ID)
		}
	}
	if len(skipped) > 0 {
		fmt.Fprintf(&b, "\nKhông đo được (thiếu thành phần): %s\n", strings.Join(skipped, ", "))
	}
	b.WriteString("\n## Thành phần bóc tách được\n")
	for _, c := range Components {
		comp, ok := r.Components[c.Key]
		if !ok || !comp.Found || c.Key == "noi_dung" {
			continue
		}
		fmt.Fprintf(&b, "- %s: %s\n", ComponentName(c.Key), truncateRunes(strings.ReplaceAll(comp.Text, "\n", " / "), 120))
	}
	b.WriteString("\nLưu ý: kiểm tra tự động theo NĐ30/2020 Phụ lục I trên file .docx; số trang chỉ đo được trường PAGE trong header; con dấu, chữ ký cần rà soát thủ công.\n")
	return b.String()
}

func hasStatus(checks []CheckResult, status string) bool {
	for _, c := range checks {
		if c.Status == status {
			return true
		}
	}
	return false
}

// writeFindings writes the checks of one status, each with its measured
// value and the offending lines.
func writeFindings(b *strings.Builder, checks []CheckResult, status string) {
	for _, c := range checks {
		if c.Status != status {
			continue
		}
		fmt.Fprintf(b, "- [%s] %s", c.ID, c.Desc)
		if c.Actual != nil {
			fmt.Fprintf(b, " — thực tế: %s", compactJSON(c.Actual))
		}
		b.WriteByte('\n')
		for _, ev := range c.Evidence {
			if txt, ok := ev["text"]; ok {
				if para, ok := ev["para"]; ok {
					fmt.Fprintf(b, "    · đoạn %v %q: %s\n", para, txt, compactJSON(ev["actual"]))
				} else if a, ok := ev["actual"].(string); ok {
					fmt.Fprintf(b, "    · %q: %s\n", txt, a)
				} else {
					fmt.Fprintf(b, "    · %q: %s\n", txt, compactJSON(ev["actual"]))
				}
			} else {
				fmt.Fprintf(b, "    · %s\n", compactJSON(ev))
			}
		}
	}
}

func fmtLine(lf LineFormat) string {
	var attrs []string
	if lf.SizePt != nil {
		attrs = append(attrs, strconv.FormatFloat(*lf.SizePt, 'f', -1, 64)+"pt")
	}
	if lf.Font != nil {
		attrs = append(attrs, *lf.Font)
	}
	if lf.Bold {
		attrs = append(attrs, "đậm")
	}
	if lf.Italic {
		attrs = append(attrs, "nghiêng")
	}
	attrs = append(attrs, "căn "+lf.Align, "cột "+lf.Zone)
	// a line break inside one paragraph matters (two signature lines in one
	// paragraph): keep it visible
	return fmt.Sprintf("%q [%s]", strings.ReplaceAll(lf.Text, "\n", " ⏎ "), strings.Join(attrs, ", "))
}

// RenderData renders the measured findings and the format data of every
// component, line by line — what the evaluation skills judge.
func RenderData(r *Report) string {
	if !r.OK {
		return RenderText(r)
	}
	var b strings.Builder
	b.WriteString("# Kết quả đo thể thức\n\n")
	findings := RenderText(r)
	if i := strings.Index(findings, "\n## Thành phần bóc tách được"); i >= 0 {
		findings = findings[:i] + "\n"
	}
	b.WriteString(findings)

	var passed []string
	for _, c := range r.Checks {
		if c.Status == StatusPass && !strings.HasPrefix(c.ID, "component.") {
			passed = append(passed, "- "+c.Desc)
		}
	}
	if len(passed) > 0 {
		b.WriteString("\n## Mục đo đạc đã đạt (không chấm lại)\n")
		b.WriteString(strings.Join(passed, "\n") + "\n")
	}

	b.WriteString("\n# Dữ liệu thể thức (đo từ file)\n")
	for _, s := range r.Sections {
		fmt.Fprintf(&b, "Trang: %s × %s mm; lề trên %s, dưới %s, trái %s, phải %s mm\n",
			fmtMM(s.PageWidthMM), fmtMM(s.PageHeightMM), fmtMM(s.MarginTopMM),
			fmtMM(s.MarginBottomMM), fmtMM(s.MarginLeftMM), fmtMM(s.MarginRightMM))
	}
	for _, cf := range r.Format {
		fmt.Fprintf(&b, "\n## %s (%s)\n", cf.Name, cf.Key)
		for i, lf := range cf.Lines {
			if cf.Omitted > 0 && i == len(cf.Lines)-bodyTailLines {
				fmt.Fprintf(&b, "- … (lược %d đoạn nội dung ở giữa)\n", cf.Omitted)
			}
			b.WriteString("- " + fmtLine(lf) + "\n")
		}
	}
	var missing []string
	for _, c := range Components {
		if comp, ok := r.Components[c.Key]; (!ok || !comp.Found) && c.Key != "khac" {
			missing = append(missing, ComponentName(c.Key))
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(&b, "\nKhông tìm thấy: %s\n", strings.Join(missing, ", "))
	}
	return b.String()
}

// RenderSkills renders the evaluation skills of a document type.
func RenderSkills(docType string) string {
	var b strings.Builder
	for _, s := range SkillsFor(docType) {
		fmt.Fprintf(&b, "<skill name=%q>\n%s\n</skill>\n\n", s.Name, strings.TrimSpace(s.Body))
	}
	return strings.TrimSpace(b.String())
}

// RenderForAgent renders the judgment brief together with the skills so
// the agent can judge them itself — the fallback when EvaluateWithSkills
// is unavailable.
func RenderForAgent(r *Report) string {
	if !r.OK {
		return RenderText(r)
	}
	return RenderJudgment(r) + "\n# Hướng dẫn đánh giá\n" +
		"Áp dụng các skill dưới đây vào hồ sơ phán đoán ở trên; giữ nguyên các lỗi đo đạc đã nêu, " +
		"bổ sung các lỗi về thẩm quyền ký, nơi nhận, thành phần và cách ghi. Chính tả do công cụ khác kiểm tra.\n\n" +
		RenderSkills(r.DocumentType.Used) + "\n"
}

func fmtMM(v *float64) string {
	if v == nil {
		return "?"
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

// zoneNames are the column names of the judgment brief.
var zoneNames = map[string]string{ZoneLeft: "cột trái", ZoneRight: "cột phải", ZoneFull: "toàn trang", ZoneSplit: "chia hai cột"}

func zoneName(z string) string {
	if n, ok := zoneNames[z]; ok {
		return n
	}
	return z
}

// judgmentLine is one line of the judgment brief: the text, quoted, with a
// line break inside the paragraph kept visible as ⏎, and — when emphasis
// is asked for — [đậm]/[nghiêng]. No font, size or alignment: those are
// measured by the rules.
func judgmentLine(lf LineFormat, emphasis bool) string {
	s := fmt.Sprintf("%q", strings.ReplaceAll(lf.Text, "\n", " ⏎ "))
	if emphasis {
		var tags []string
		if lf.Bold {
			tags = append(tags, "đậm")
		}
		if lf.Italic {
			tags = append(tags, "nghiêng")
		}
		if len(tags) > 0 {
			s += " [" + strings.Join(tags, ", ") + "]"
		}
	}
	return s
}

// shortDesc cuts a rule description to its head ("Lề trên 20-25mm (NĐ30
// Phụ lục I)" → "Lề trên 20-25mm").
func shortDesc(d string) string {
	for _, sep := range []string{" (", " — "} {
		if i := strings.Index(d, sep); i > 0 {
			d = d[:i]
		}
	}
	if runeLen(d) > 60 {
		d = truncateRunes(d, 60) + "…"
	}
	return d
}

// propNames name the measured property of a check id's last part.
var propNames = map[string]string{
	"zone": "vị trí", "bold": "độ đậm", "italic": "kiểu nghiêng", "size": "cỡ chữ",
	"align": "căn lề", "font": "phông chữ", "pattern": "dạng ghi", "type_line": "dòng tên loại",
	"first_line_indent": "thụt đầu dòng", "line_spacing": "giãn dòng", "color": "màu chữ",
}

// passedSummary lists the passed measurements compactly, grouped by
// component: "Trang (khổ giấy, lề); Quốc hiệu (vị trí, đậm, cỡ chữ); …".
func passedSummary(checks []CheckResult) string {
	var groups []string
	props := map[string][]string{}
	add := func(group, prop string) {
		if _, ok := props[group]; !ok {
			groups = append(groups, group)
		}
		for _, p := range props[group] {
			if p == prop {
				return
			}
		}
		props[group] = append(props[group], prop)
	}
	for _, c := range checks {
		if c.Status != StatusPass || strings.HasPrefix(c.ID, "component.") {
			continue
		}
		key, prop, _ := strings.Cut(c.ID, ".")
		part := "" // "label" / "list" of noi_nhan.label.size, "dieu" of noi_dung.dieu.bold
		if i := strings.LastIndex(prop, "."); i >= 0 {
			part, prop = prop[:i], prop[i+1:]
		}
		switch {
		case key == "page":
			switch {
			case strings.HasPrefix(part+prop, "size"):
				add("Trang", "khổ giấy")
			case prop == "number":
				add("Trang", "đánh số trang")
			default:
				add("Trang", "lề")
			}
		case key == "dia_danh" && propNames[prop] != "":
			add(ComponentName("dia_danh_ngay_thang"), propNames[prop])
		case componentNames[key] != "":
			// the property, never the rule's description: a passed rule's
			// example ("'NNN/CV-CQ'") was read as a requirement
			name := propNames[prop]
			if name == "" {
				name = "khác"
			}
			switch part {
			case "label":
				name += " dòng nhãn"
			case "list":
				name += " danh sách"
			case "dieu":
				name += " dòng Điều"
			}
			add(ComponentName(key), name)
		default:
			add(shortDesc(c.Desc), "")
		}
	}
	parts := make([]string, 0, len(groups))
	for _, g := range groups {
		var ps []string
		for _, p := range props[g] {
			if p != "" {
				ps = append(ps, p)
			}
		}
		if len(ps) > 0 {
			g += " (" + strings.Join(ps, ", ") + ")"
		}
		parts = append(parts, g)
	}
	return strings.Join(parts, "; ")
}

// judgmentRenderVersion changes whenever RenderJudgment's layout changes,
// so cached evaluations made on the old layout are dropped (SkillsDigest).
const judgmentRenderVersion = "judgment-v3"

// RenderJudgment renders the judgment brief ("hồ sơ phán đoán") the skill
// evaluation reads: the measured findings (final, not to be re-judged),
// then each component as text and position only — no font, size or
// alignment, which the rules measure — keeping [đậm]/[nghiêng] on the
// lines whose emphasis the skills still judge (cơ quan chủ quản, căn cứ,
// khối ký, nơi nhận). Quốc hiệu, tiêu ngữ and the body text are left out;
// the body is shown as its anchor lines.
func RenderJudgment(r *Report) string {
	if r == nil || !r.OK {
		if r == nil {
			return ""
		}
		return RenderText(r)
	}
	var b strings.Builder
	dt := r.DocumentType
	b.WriteString("# Hồ sơ phán đoán\n")
	fmt.Fprintf(&b, "Văn bản: %s\n", r.Source)
	fmt.Fprintf(&b, "Loại: %s (nhận diện: %s; bộ quy tắc: %s)\n", dt.Used, dt.Detected, dt.RuleSet)
	if !r.FullScope() {
		fmt.Fprintf(&b, "Phạm vi kiểm tra: %s — %s\n", ScopeName(r.Scope.Used), r.Scope.Reason)
	}
	if s := r.Segmentation; s != nil && s.Error != "" {
		// the components were assigned by position only: say so
		fmt.Fprintf(&b, "Gán thành phần: %s — %s\n", s.Method, s.Error)
	}

	for _, st := range []struct{ status, title string }{
		{StatusFail, "Lỗi đo được"}, {StatusWarn, "Cảnh báo đo được"},
	} {
		if !hasStatus(r.Checks, st.status) {
			fmt.Fprintf(&b, "%s: không có\n", st.title)
			continue
		}
		fmt.Fprintf(&b, "%s:\n", st.title)
		writeFindings(&b, r.Checks, st.status)
	}
	if passed := passedSummary(r.Checks); passed != "" {
		fmt.Fprintf(&b, "Đã đo đạt (không chấm lại): %s\n", passed)
	}

	format := map[string]ComponentFormat{}
	for _, cf := range r.Format {
		format[cf.Key] = cf
	}
	joined := func(key string, emphasis bool) string {
		var parts []string
		for _, lf := range format[key].Lines {
			parts = append(parts, judgmentLine(lf, emphasis))
		}
		return strings.Join(parts, " / ")
	}
	list := func(title, key string, emphasis bool) {
		cf, ok := format[key]
		if !ok {
			return
		}
		fmt.Fprintf(&b, "%s:\n", title)
		for _, lf := range cf.Lines {
			b.WriteString("  - " + judgmentLine(lf, emphasis) + "\n")
		}
	}

	b.WriteString("\n## Thành phần ([số] là ô theo NĐ30 Phụ lục I)\n")
	if s := joined("co_quan_chu_quan", true); s != "" {
		fmt.Fprintf(&b, "[2] Cơ quan chủ quản: %s\n", s)
	}
	if s := joined("co_quan_ban_hanh", false); s != "" {
		fmt.Fprintf(&b, "[2] Cơ quan ban hành: %s\n", s)
	}
	if s := joined("so_ky_hieu", false); s != "" {
		fmt.Fprintf(&b, "[3] Số, ký hiệu: %s\n", s)
	}
	if s := joined("dia_danh_ngay_thang", false); s != "" {
		fmt.Fprintf(&b, "[4] Địa danh, ngày tháng: %s\n", s)
	}
	if s := joined("do_mat", false); s != "" {
		fmt.Fprintf(&b, "[10a] Độ mật: %s\n", s)
	}
	if s := joined("do_khan", false); s != "" {
		fmt.Fprintf(&b, "[10b] Độ khẩn: %s\n", s)
	}
	if s := joined("trich_yeu", false); s != "" {
		// công văn has no tên loại line (ô 5b); other types print it (ô 5a)
		line := "[5a] Tên loại, trích yếu: " + s
		if dt.Used == "cong_van" {
			line = "[5b] Trích yếu công văn (công văn không có dòng tên loại): " + s
		}
		if tq := joined("tham_quyen_ban_hanh", false); tq != "" {
			line += "; Thẩm quyền ban hành: " + tq
		}
		b.WriteString(line + "\n")
	} else if tq := joined("tham_quyen_ban_hanh", false); tq != "" {
		fmt.Fprintf(&b, "[5] Thẩm quyền ban hành: %s\n", tq)
	}
	list("[6] Căn cứ", "can_cu", true)
	list("[9a] Kính gửi", "kinh_gui", false)

	if r.BodyParas == 0 {
		b.WriteString("[6] Thân văn bản: không có\n")
	} else {
		fmt.Fprintf(&b, "[6] Thân văn bản: %d đoạn; dòng mốc (chỉ các dòng này được trích):\n", r.BodyParas)
		if len(r.Anchors) == 0 {
			b.WriteString("  (không có dòng mốc nào khớp)\n")
		}
		for _, a := range r.Anchors {
			fmt.Fprintf(&b, "  - đ%d %q (%s)\n", a.Para, a.Text, a.Desc)
		}
	}

	// signature blocks: per column, the chức vụ lines then the họ tên
	var zones []string
	byZone := map[string][]LineFormat{}
	for _, key := range []string{"chuc_danh", "nguoi_ky"} {
		for _, lf := range format[key].Lines {
			if _, ok := byZone[lf.Zone]; !ok {
				zones = append(zones, lf.Zone)
			}
			byZone[lf.Zone] = append(byZone[lf.Zone], lf)
		}
	}
	for _, z := range zones {
		fmt.Fprintf(&b, "[7] Khối ký (%s):\n", zoneName(z))
		for _, lf := range byZone[z] {
			b.WriteString("  - " + judgmentLine(lf, true) + "\n")
		}
	}
	if cf, ok := format["noi_nhan"]; ok && len(cf.Lines) > 0 {
		fmt.Fprintf(&b, "[9b] Nơi nhận (%s; dòng đầu là nhãn, các dòng sau là danh sách):\n", zoneName(cf.Lines[0].Zone))
		for _, lf := range cf.Lines {
			b.WriteString("  - " + judgmentLine(lf, true) + "\n")
		}
	}
	if s := joined("chi_dan_luu_hanh", false); s != "" {
		fmt.Fprintf(&b, "[11] Chỉ dẫn phạm vi lưu hành: %s\n", s)
	}
	if s := joined("ky_hieu_soan_thao", false); s != "" {
		fmt.Fprintf(&b, "[12] Ký hiệu người soạn thảo, số bản: %s\n", s)
	}
	if s := joined("dia_chi_co_quan", false); s != "" {
		fmt.Fprintf(&b, "[13] Địa chỉ cơ quan: %s\n", s)
	}
	if comp, ok := r.Components["phu_luc"]; ok && comp.Found {
		b.WriteString("Phụ lục: có\n")
	}

	// missing parts, split by whether the rule set requires them
	required := map[string]bool{}
	if rs, err := RuleSetForReport(r); err == nil {
		for _, k := range rs.RequiredComponents {
			required[k] = true
		}
	}
	found := func(k string) bool {
		c, ok := r.Components[k]
		return ok && c.Found
	}
	var missReq, missOpt []string
	if required["signature"] && !found("chuc_danh") && !found("nguoi_ky") {
		missReq = append(missReq, ComponentName("signature"))
	}
	for _, c := range Components {
		if c.Key == "khac" || found(c.Key) {
			continue
		}
		if required[c.Key] {
			missReq = append(missReq, ComponentName(c.Key))
		} else if !minorComponents[c.Key] {
			missOpt = append(missOpt, ComponentName(c.Key))
		}
	}
	if len(missReq) > 0 {
		fmt.Fprintf(&b, "Không có (bắt buộc với loại này): %s\n", strings.Join(missReq, ", "))
	}
	if len(missOpt) > 0 {
		fmt.Fprintf(&b, "Không có (không bắt buộc): %s\n", strings.Join(missOpt, ", "))
	}
	return b.String()
}
