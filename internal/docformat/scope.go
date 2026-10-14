package docformat

import (
	"fmt"
	"regexp"
	"strings"

	vl "github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// Check scope: how much of NĐ30 a document is checked against.
//
//   - full: one of the 29 văn bản hành chính of Điều 7 NĐ30/2020 — every
//     rule, the required components, their order and the Bảng kiểm.
//   - technical: anything else — an unknown type, a văn bản quy phạm pháp
//     luật already promulgated (luật, nghị định, thông tư …), a form, a
//     table, loose material, or a hành chính heading with none of quốc
//     hiệu, số ký hiệu and khối ký. Only the technical layout is checked
//     (page, body font/size/colour/alignment/indent/spacing, page number,
//     stray characters); no component requirement, no model.
//   - attached: a văn bản ban hành kèm theo (quy chế, quy định, kế hoạch,
//     đề án …) whose heading carries "(Ban hành kèm theo Quyết định số …)"
//     and which has no số ký hiệu or khối ký of its own: the technical
//     checks, the tên loại / trích yếu checks and the "kèm theo" line.
//
// Which rules run in a scope is data: a rule check or judgment rule lists
// its scopes in "scope" (rules/_base.json); without one it runs in the full
// scope only.

// Scopes.
const (
	ScopeAuto      = "auto"
	ScopeFull      = "full"
	ScopeTechnical = "technical"
	ScopeAttached  = "attached"
)

// ScopeInfo tells which check scope was applied and why.
type ScopeInfo struct {
	Used     string `json:"used"`
	Detected string `json:"detected"`
	Reason   string `json:"reason"`
}

// kemTheoRe matches the "ban hành kèm theo" line of an attached document
// on fold()ed text: "(Ban hành kèm theo Quyết định số …)", "(Kèm theo
// Kế hoạch số …)".
var kemTheoRe = regexp.MustCompile(`\bBAN HANH KEM THEO\b|^\(?\s*KEM THEO\s+(QUYET DINH|NGHI QUYET|NGHI DINH|THONG TU|CHI THI|KE HOACH|CONG VAN|VAN BAN|TO TRINH)\b`)

// kemTheoScanLines bounds how far into the body ScopeOf looks for the
// "kèm theo" line when the segmenter left it out of the trích yếu.
const kemTheoScanLines = 3

// KemTheoLine returns the "ban hành kèm theo" line of the heading block
// (trích yếu, then the first body lines), "" when there is none.
func KemTheoLine(seg *Segmentation) string {
	if seg == nil {
		return ""
	}
	var lines []string
	lines = append(lines, strings.Split(seg.Comp("trich_yeu").Text, "\n")...)
	lines = append(lines, strings.Split(seg.Comp("tham_quyen_ban_hanh").Text, "\n")...)
	for _, ln := range lines {
		if ln = strings.TrimSpace(ln); ln != "" && kemTheoRe.MatchString(fold(ln)) {
			return ln
		}
	}
	// in the body only the parenthesised heading line counts, never
	// "Điều 1. Ban hành kèm theo Quyết định này …"
	body := strings.Split(seg.Comp("noi_dung").Text, "\n")
	if len(body) > kemTheoScanLines {
		body = body[:kemTheoScanLines]
	}
	for _, ln := range body {
		if ln = strings.TrimSpace(ln); strings.HasPrefix(ln, "(") && kemTheoRe.MatchString(fold(ln)) {
			return ln
		}
	}
	return ""
}

// IsHanhChinhType reports whether docType is one of the 29 văn bản hành
// chính of Điều 7 NĐ30/2020.
func IsHanhChinhType(docType string) bool {
	d := vl.DocTypeBySlug(docType)
	return d != nil && d.Group == vl.DocTypeGroupHanhChinh
}

// ScopeOf classifies a segmented document: attached when its heading
// block has the "kèm theo" line and it lacks a số ký hiệu or a khối ký of
// its own; full when the detected type is a văn bản hành chính and at
// least one of quốc hiệu, số ký hiệu, khối ký was found; technical
// otherwise. It returns the scope and the reason, in Vietnamese.
func ScopeOf(seg *Segmentation, detectedType string) (string, string) {
	if seg == nil {
		return ScopeTechnical, "không bóc tách được thành phần"
	}
	found := func(k string) bool { return seg.Comp(k).Found }
	if line := KemTheoLine(seg); line != "" && (!found("so_ky_hieu") || !found("signature")) {
		return ScopeAttached, fmt.Sprintf("văn bản ban hành kèm theo (%q), không có số ký hiệu hoặc khối ký riêng",
			truncateRunes(line, 100))
	}
	quocHieu := found("quoc_hieu") || hasLine(seg, quocHieuRe)
	if !IsHanhChinhType(detectedType) {
		if d := vl.DocTypeBySlug(detectedType); d != nil {
			return ScopeTechnical, fmt.Sprintf("loại %q là văn bản quy phạm pháp luật, không thuộc 29 loại văn bản hành chính", d.Name)
		}
		if quocHieu && (found("so_ky_hieu") || found("noi_nhan") || hasLine(seg, scopeSoLineRe) || hasLine(seg, noiNhanLineRe)) {
			// the type heading was not recognised, but quốc hiệu with số or
			// nơi nhận is a văn bản hành chính: the shared NĐ30 rules
			return ScopeFull, "chưa nhận diện được loại nhưng có quốc hiệu và số ký hiệu/nơi nhận: kiểm tra theo thể thức chung"
		}
		return ScopeTechnical, "không nhận diện được loại văn bản hành chính"
	}
	if !quocHieu && !found("so_ky_hieu") && !found("signature") {
		return ScopeTechnical, "không có quốc hiệu, số ký hiệu và khối ký (phần thân, biểu mẫu hoặc bảng)"
	}
	return ScopeFull, fmt.Sprintf("văn bản hành chính loại %q", detectedType)
}

var (
	scopeSoLineRe = regexp.MustCompile(`^SO\s*:?\s*\d*\s*/\s*\S`)
	noiNhanLineRe = regexp.MustCompile(`^NOI NHAN\s*:`)
)

// hasLine: a line of any component matches re on fold()ed text — a quốc
// hiệu, số or nơi nhận the segmenter put in another component (a flat,
// all-centred header) still marks a văn bản hành chính.
func hasLine(seg *Segmentation, re *regexp.Regexp) bool {
	for _, c := range seg.Components {
		if !c.Found {
			continue
		}
		for _, ln := range strings.Split(c.Text, "\n") {
			if re.MatchString(fold(strings.TrimSpace(ln))) {
				return true
			}
		}
	}
	return false
}

// resolveScope applies Options.Scope and an explicitly requested
// document type over the detected scope.
func resolveScope(optScope, requestedType string, seg *Segmentation) (*ScopeInfo, error) {
	detected, reason := ScopeOf(seg, seg.DetectedType)
	info := &ScopeInfo{Used: detected, Detected: detected, Reason: reason}
	switch s := strings.ToLower(strings.TrimSpace(optScope)); s {
	case "", ScopeAuto:
		if requestedType != "auto" && requestedType != "detect" && detected != ScopeFull {
			// a type the user named is checked in full
			info.Used, info.Reason = ScopeFull, fmt.Sprintf("loại văn bản %q do người dùng chọn", requestedType)
		}
	case ScopeFull, ScopeTechnical, ScopeAttached:
		if s != detected {
			info.Used, info.Reason = s, "phạm vi kiểm tra do người dùng chọn"
		}
	default:
		return nil, fmt.Errorf("scope must be one of auto, full, technical, attached")
	}
	return info, nil
}

func inScope(scopes []string, scope string) bool {
	if len(scopes) == 0 {
		return scope == ScopeFull
	}
	for _, s := range scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// ForScope returns the rule set cut down to a scope ("" is full): the
// checks and judgment rules that list it; outside the full scope no
// component is required, no order is checked and no anchor is picked.
func (rs *RuleSet) ForScope(scope string) *RuleSet {
	if rs == nil {
		return rs
	}
	if scope == "" {
		scope = ScopeFull
	}
	out := *rs
	if scope != ScopeFull {
		out.RequiredComponents, out.OptionalComponents, out.Order, out.Anchors = nil, nil, nil, nil
	}
	out.Checks, out.Judgment = nil, nil
	for _, c := range rs.Checks {
		if inScope(c.Scope, scope) {
			out.Checks = append(out.Checks, c)
		}
	}
	for _, j := range rs.Judgment {
		if inScope(j.Scope, scope) {
			out.Judgment = append(out.Judgment, j)
		}
	}
	return &out
}

// scopedSegmentation is the segmentation the attached scope evaluates: the
// "kèm theo" line, italic by custom, is judged on its own and leaves the
// trích yếu whose bold and size the rules measure.
func scopedSegmentation(l *Layout, seg *Segmentation, scope string) *Segmentation {
	if scope != ScopeAttached {
		return seg
	}
	ty, ok := seg.Components["trich_yeu"]
	if !ok || !ty.Found {
		return seg
	}
	keep := &Component{Found: ty.Found, Zone: ty.Zone, Pos: ty.Pos}
	var texts []string
	for i, idx := range ty.Paras {
		if idx >= 0 && idx < len(l.Paragraphs) && kemTheoRe.MatchString(fold(l.Paragraphs[idx].Text)) {
			continue
		}
		keep.Paras = append(keep.Paras, idx)
		if i < len(ty.Zones) {
			keep.Zones = append(keep.Zones, ty.Zones[i])
		}
		if i < len(ty.Texts) {
			keep.Texts = append(keep.Texts, ty.Texts[i])
		}
		if idx >= 0 && idx < len(l.Paragraphs) {
			texts = append(texts, l.Paragraphs[idx].Text)
		}
	}
	if len(keep.Paras) == len(ty.Paras) {
		return seg
	}
	keep.Text = strings.Join(texts, "\n")
	keep.Found = len(keep.Paras) > 0
	cp := *seg
	cp.Components = make(map[string]*Component, len(seg.Components))
	for k, v := range seg.Components {
		cp.Components[k] = v
	}
	cp.Components["trich_yeu"] = keep
	return &cp
}

// scopeChecksNote is the line of the scoped evaluation saying why the
// components were not checked.
const scopeChecksNote = "Không kiểm tra thành phần thể thức vì văn bản không thuộc 29 loại văn bản hành chính (Điều 7 NĐ30); chọn loại văn bản để kiểm tra đầy đủ."

// scopeNames are the scopes as the reports name them.
var scopeNames = map[string]string{
	ScopeFull:      "đầy đủ (văn bản hành chính)",
	ScopeTechnical: "kỹ thuật trình bày",
	ScopeAttached:  "văn bản ban hành kèm theo",
}

// ScopeName names a scope for the user.
func ScopeName(scope string) string {
	if n, ok := scopeNames[scope]; ok {
		return n
	}
	return scope
}

// BuildScopedEvaluation writes the evaluation of a technical or attached
// scope in code: the scope and why, the measured findings, the judged
// "kèm theo" line (attached), and why the components were not checked.
func BuildScopedEvaluation(r *Report, items []JudgmentItem) string {
	var b strings.Builder
	sc := r.Scope
	errs := measuredFindings(r, StatusFail)
	warns := append(measuredFindings(r, StatusWarn), judgedFindings(items, "warn")...)
	errs = append(errs, judgedFindings(items, "error")...)
	b.WriteString("## Kết luận\n")
	fmt.Fprintf(&b, "Phạm vi kiểm tra: **%s** — %s.\n", ScopeName(sc.Used), sc.Reason)
	if len(errs) > 0 {
		fmt.Fprintf(&b, "**Chưa đạt kỹ thuật trình bày**: %d lỗi cần sửa", len(errs))
	} else {
		b.WriteString("**Đạt kỹ thuật trình bày**: không có lỗi")
	}
	if len(warns) > 0 {
		fmt.Fprintf(&b, ", %d cảnh báo nên sửa", len(warns))
	}
	b.WriteString(".\n\n")
	fmt.Fprintf(&b, "Đã kiểm tra: %s.\n", scopeCheckedList(r))
	for _, it := range items {
		line := strings.TrimPrefix(ChecklistLine(it), fmt.Sprintf("%d. ", it.Item))
		fmt.Fprintf(&b, "- %s\n", line)
	}
	b.WriteString(scopeChecksNote + "\n")

	b.WriteString("\n## Lỗi (bắt buộc sửa)\n")
	if len(errs) == 0 {
		b.WriteString("Không có.\n")
	}
	writeGrouped(&b, errs)
	b.WriteString("\n## Cảnh báo (nên sửa)\n")
	if len(warns) == 0 {
		b.WriteString("Không có.\n")
	}
	writeGrouped(&b, warns)
	b.WriteString("\n---\nLưu ý: chính tả do công cụ kiểm tra chính tả rà riêng; con dấu, chữ ký, số trang cần rà soát thủ công trên bản in.\n")
	return b.String()
}

// scopeCheckedList names what a scoped check measured, from its checks.
func scopeCheckedList(r *Report) string {
	var parts []string
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			parts = append(parts, s)
		}
	}
	for _, c := range r.Checks {
		switch {
		case strings.HasPrefix(c.ID, "page.size"):
			add("khổ giấy")
		case strings.HasPrefix(c.ID, "page.margin"):
			add("lề trang")
		case c.ID == "page.number":
			add("số trang")
		case strings.HasPrefix(c.ID, "noi_dung."):
			add("phông, cỡ chữ, màu, căn lề, thụt đầu dòng, giãn dòng của nội dung")
		case strings.HasPrefix(c.ID, "trich_yeu."):
			add("tên loại, trích yếu")
		case c.ID == "text.stray_chars":
			add("ký tự lạ")
		}
	}
	if r.Scope != nil && r.Scope.Used == ScopeAttached {
		add("dòng \"ban hành kèm theo\"")
	}
	if len(parts) == 0 {
		return "không có mục nào"
	}
	return strings.Join(parts, "; ")
}

// attachedHeading (judgment kind attached_heading): the heading of an
// attached document must carry "(Ban hành kèm theo <văn bản> số … ngày …
// của …)"; the line missing, or its số, ngày or cơ quan missing, is a
// finding of the rule's severity (warn by default).
func (jd *judgeData) attachedHeading(jr JudgmentRule) ruleOut {
	var o ruleOut
	sev := jr.Severity
	if sev == "" {
		sev = "warn"
	}
	line := KemTheoLine(&Segmentation{Components: jd.r.Components})
	if re := jre(jr.HeadingRegex); line != "" && re != nil && !re.MatchString(fold(line)) {
		line = ""
	}
	if line == "" {
		o.add(sev, "trich_yeu", "Văn bản ban hành kèm theo phải ghi dưới tên loại, trích yếu dòng "+
			"\"(Ban hành kèm theo <Quyết định> số … ngày … của …)\"", "",
			"Bổ sung dòng \"(Ban hành kèm theo Quyết định số …/QĐ-… ngày … tháng … năm … của …)\".",
			"thiếu dòng \"ban hành kèm theo\"")
		return o
	}
	f := fold(line)
	var missing []string
	if re := jre(jr.NumberRegex); re != nil && !re.MatchString(f) {
		missing = append(missing, "số")
	}
	if re := jre(jr.DateRegex); re != nil && !re.MatchString(f) {
		missing = append(missing, "ngày tháng năm")
	}
	if re := jre(jr.IssuerRegex); re != nil && !re.MatchString(f) {
		missing = append(missing, "cơ quan, người ban hành (\"của …\")")
	}
	if len(missing) > 0 {
		what := strings.Join(missing, ", ")
		o.add(sev, "trich_yeu", "Dòng \"ban hành kèm theo\" ghi đủ số, ngày tháng năm và cơ quan ban hành văn bản gốc",
			line, "Ghi đủ "+what+" của văn bản ban hành.", "dòng \"kèm theo\" thiếu "+what)
		return o
	}
	o.note = "có dòng \"ban hành kèm theo\" đủ số, ngày"
	return o
}

// judgeScoped decides the judgment rules of a technical or attached scope
// (the scoped rule set lists only theirs): one item per rule, no model.
func judgeScoped(r *Report) []JudgmentItem {
	rs, err := RuleSetForReport(r)
	if err != nil {
		return nil
	}
	jd := newJudgeData(r)
	var items []JudgmentItem
	for _, jr := range rs.Judgment {
		var o ruleOut
		var title string
		switch jr.Kind {
		case KindAttachedHeading:
			o, title = jd.attachedHeading(jr), "Dòng \"ban hành kèm theo\""
		default:
			continue // the Bảng kiểm kinds need the full scope
		}
		it := JudgmentItem{Item: jr.Item, Title: title, By: "rules", Findings: o.findings, Note: o.note}
		it.Status = JudgePass
		for _, f := range o.findings {
			if f.Severity == "error" {
				it.Status = JudgeFail
				break
			}
			it.Status = JudgeWarn
		}
		items = append(items, it)
	}
	return items
}
