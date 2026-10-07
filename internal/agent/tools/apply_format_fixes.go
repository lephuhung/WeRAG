package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

var applyFormatFixesTool = BaseTool{
	name: ToolApplyFormatFixes,
	description: `Fix the layout ("thể thức") of the Word document open in this conversation's editor to Nghị định 30/2020/NĐ-CP. It re-runs the NĐ30 rule checks and corrects what can be corrected mechanically, as Word tracked formatting changes the user accepts or rejects in the editor:

- font (Times New Roman), font size, bold/italic and paragraph alignment of each component (quốc hiệu, tiêu ngữ, trích yếu, nội dung, chữ ký, nơi nhận…) — every paragraph of the component that breaks the rule, not only the samples a check listed;
- paper size (A4) and page margins.

If the document's structure cannot be identified reliably (a required component such as quốc hiệu or chữ ký was not found, or the document type is unknown), nothing is changed: the result is the plan with a warning. Confirm with the user — ideally ask them which document type it is and pass document_type — then call again with force=true.

Position (left/right column), wording, missing components, component order and lines split in two columns by a tab cannot be fixed this way: they are listed as needing a manual fix.

## When to Use

After check_document_format (or when the user directly asks to normalise the layout). Run with dry_run=true first to show the plan when it would change many paragraphs and the user has not already asked to fix everything.

## Input

- check_ids: rule ids to fix (as reported by the check, e.g. "noi_dung.font", "trich_yeu.size", "page.margin.left"); omit to fix every failed or warned rule that is fixable.
- document_type: rule set to apply (cong_van, quyet_dinh, …); omit to auto-detect.
- dry_run: true to only return the plan without changing the document.
- force: true to apply even though the structure could not be identified reliably; only after the user confirmed.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "check_ids": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Rule ids to fix, e.g. noi_dung.font, trich_yeu.size, page.margin.left; omit to fix every fixable failed/warned rule"
    },
    "document_type": {
      "type": "string",
      "description": "Optional rule set, e.g. cong_van, quyet_dinh, bao_cao, to_trinh; omit to auto-detect"
    },
    "dry_run": {
      "type": "boolean",
      "description": "true: return the fix plan without changing the document"
    },
    "force": {
      "type": "boolean",
      "description": "true: apply even when the document structure could not be identified reliably (only after the user confirmed)"
    }
  }
}`),
}

type applyFormatFixesInput struct {
	CheckIDs     []string `json:"check_ids"`
	DocumentType string   `json:"document_type"`
	DryRun       bool     `json:"dry_run"`
	Force        bool     `json:"force"`
}

// ApplyFormatFixesTool corrects mechanically fixable NĐ30 findings in the
// session's workspace document as tracked formatting changes.
type ApplyFormatFixesTool struct {
	BaseTool
	workspace DocumentWorkspaceSource
	chatModel chat.Chat
	sessionID string
}

// NewApplyFormatFixesTool builds the tool for one session. chatModel may be
// nil: components are then labelled by the positional heuristic.
func NewApplyFormatFixesTool(workspace DocumentWorkspaceSource, chatModel chat.Chat, sessionID string) *ApplyFormatFixesTool {
	return &ApplyFormatFixesTool{BaseTool: applyFormatFixesTool, workspace: workspace, chatModel: chatModel, sessionID: sessionID}
}

// AppliedFormatFix is one rule's planned or applied correction.
type AppliedFormatFix struct {
	CheckID    string `json:"check_id"`
	Paragraphs []int  `json:"paragraphs"`
	Sections   []int  `json:"sections,omitempty"`
	Change     string `json:"change"`
}

// SkippedFormatFix is a fixable-kind rule that was not (fully) applied.
type SkippedFormatFix struct {
	CheckID string `json:"check_id"`
	Reason  string `json:"reason"`
}

// ManualFormatFix is a finding the tool cannot fix mechanically.
type ManualFormatFix struct {
	CheckID string `json:"check_id"`
	Desc    string `json:"desc"`
}

// paraEdit is one check's formatting change for a paragraph.
type paraEdit struct {
	run *docxedit.RunProps
	pp  *docxedit.ParaProps
}

// mergedParaEdit is every change a plan makes to one paragraph: one
// RunProps and one ParaProps, with the checks each comes from.
type mergedParaEdit struct {
	run       *docxedit.RunProps
	pp        *docxedit.ParaProps
	runChecks []string
	ppChecks  []string
}

// sectionEdit is every page-setup change of one section.
type sectionEdit struct {
	props  docxedit.SectionProps
	checks []string
}

type formatPlan struct {
	applied  []AppliedFormatFix
	skipped  []SkippedFormatFix
	manual   []ManualFormatFix
	paras    map[int]*mergedParaEdit
	sections map[int]*sectionEdit
}

func newFormatPlan() *formatPlan {
	return &formatPlan{paras: map[int]*mergedParaEdit{}, sections: map[int]*sectionEdit{}}
}

// hasEdits reports whether applying the plan would change the document.
func (p *formatPlan) hasEdits() bool { return len(p.paras) > 0 || len(p.sections) > 0 }

// addParaEdit merges e (from checkID) into the paragraph's edit.
func (p *formatPlan) addParaEdit(para int, checkID string, e paraEdit) {
	m := p.paras[para]
	if m == nil {
		m = &mergedParaEdit{}
		p.paras[para] = m
	}
	if e.run != nil {
		if m.run == nil {
			m.run = &docxedit.RunProps{}
		}
		mergeRunProps(m.run, e.run)
		m.runChecks = appendUnique(m.runChecks, checkID)
	}
	if e.pp != nil {
		if m.pp == nil {
			m.pp = &docxedit.ParaProps{}
		}
		if e.pp.Alignment != nil {
			m.pp.Alignment = e.pp.Alignment
		}
		m.ppChecks = appendUnique(m.ppChecks, checkID)
	}
}

func mergeRunProps(dst, src *docxedit.RunProps) {
	if src.Font != nil {
		dst.Font = src.Font
	}
	if src.SizePt != nil {
		dst.SizePt = src.SizePt
	}
	if src.Bold != nil {
		dst.Bold = src.Bold
	}
	if src.Italic != nil {
		dst.Italic = src.Italic
	}
	if src.Caps != nil {
		dst.Caps = src.Caps
	}
	if src.Underline != nil {
		dst.Underline = src.Underline
	}
}

func appendUnique(list []string, s string) []string {
	if containsString(list, s) {
		return list
	}
	return append(list, s)
}

// mmToPt converts millimetres to points.
const mmToPt = 2.8346

// Conventional NĐ30 page setup, used as the target when a rule's range
// contains it (else the nearest bound of the range).
var conventionalPageMM = map[string]float64{
	"page_width_mm": 210, "page_height_mm": 297,
	"margin_top_mm": 20, "margin_bottom_mm": 20, "margin_left_mm": 30, "margin_right_mm": 15,
}

var pagePropVI = map[string]string{
	"page_width_mm": "chiều rộng trang", "page_height_mm": "chiều cao trang",
	"margin_top_mm": "lề trên", "margin_bottom_mm": "lề dưới", "margin_left_mm": "lề trái", "margin_right_mm": "lề phải",
}

func (t *ApplyFormatFixesTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var in applyFormatFixesInput
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return &types.ToolResult{Success: false, Error: "invalid arguments: " + err.Error()}, nil
		}
	}
	tenantID, content, ws, err := prepareWorkspaceWrite(ctx, t.workspace, t.sessionID)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	report, err := segmentReport(ctx, content, in.DocumentType, ws.FileName, t.chatModel)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	rs, err := docformat.RuleSetForReport(report)
	if err != nil {
		return &types.ToolResult{Success: false, Error: "không nạp được bộ quy tắc: " + err.Error()}, nil
	}
	layout := docformat.InspectDocx(content)
	plan := buildFormatPlan(report, rs, layout, in.CheckIDs)

	// A mislabelled document would get the wrong component's formatting:
	// when its structure is not recognised, only show the plan.
	missing, typeKnown := structureGaps(report, rs)
	unreliable := len(missing) > 0 || !typeKnown
	dryRun := in.DryRun || (unreliable && !in.Force)

	revision := ws.Revision
	if !dryRun && plan.hasEdits() {
		doc, err := docxedit.Open(content)
		if err != nil {
			return &types.ToolResult{Success: false, Error: "không mở được tài liệu để sửa: " + err.Error()}, nil
		}
		written, err := plan.apply(doc)
		if err != nil {
			return &types.ToolResult{Success: false, Error: "không áp dụng được sửa thể thức: " + err.Error()}, nil
		}
		if written == 0 {
			return &types.ToolResult{Success: false, Error: "không áp dụng được sửa thể thức nào:\n" +
				renderFormatPlan(ws.FileName, ws.Revision, report, plan, true)}, nil
		}
		data, err := doc.Bytes()
		if err != nil {
			return &types.ToolResult{Success: false, Error: "không ghi được tài liệu: " + err.Error()}, nil
		}
		next, err := t.workspace.CommitExternalWrite(ctx, tenantID, t.sessionID, ws.Revision, data)
		if err != nil {
			if isWorkspaceConflict(err) {
				return &types.ToolResult{Success: false, Error: conflictRetryMessage}, nil
			}
			logger.Warnf(ctx, "apply_format_fixes: commit failed: %v", err)
			return &types.ToolResult{Success: false, Error: "không lưu được tài liệu: " + err.Error()}, nil
		}
		revision = next.Revision
	}

	applied := plan.applied
	if applied == nil {
		applied = []AppliedFormatFix{}
	}
	skipped := plan.skipped
	if skipped == nil {
		skipped = []SkippedFormatFix{}
	}
	manual := plan.manual
	if manual == nil {
		manual = []ManualFormatFix{}
	}
	output := renderFormatPlan(ws.FileName, revision, report, plan, dryRun)
	blocked := unreliable && !in.Force && !in.DryRun
	switch {
	case unreliable && !in.Force:
		output = structureWarning(report, missing, typeKnown, blocked) + "\n\n" + output
	case !dryRun && !in.Force:
		if n := plan.paragraphCount(); n > largeFixParagraphs {
			output = fmt.Sprintf("Lưu ý: lần sửa này thay đổi định dạng của %d đoạn (nhiều hơn %d).\n", n, largeFixParagraphs) + output
		}
	}
	if missing == nil {
		missing = []string{}
	}
	return &types.ToolResult{
		Success: true,
		Output:  output,
		Data: map[string]interface{}{
			"file_name":          ws.FileName,
			"document_revision":  revision,
			"document_type":      report.DocumentType.Used,
			"applied":            applied,
			"skipped":            skipped,
			"manual":             manual,
			"dry_run":            dryRun,
			"blocked":            blocked,
			"missing_components": missing,
			"segmentation":       report.Segmentation.Method,
		},
	}, nil
}

// largeFixParagraphs is the paragraph count above which an applied fix is
// called out in the output.
const largeFixParagraphs = 10

// paragraphCount is the number of distinct paragraphs the plan changes.
func (p *formatPlan) paragraphCount() int {
	seen := map[int]bool{}
	for _, f := range p.applied {
		for _, i := range f.Paragraphs {
			seen[i] = true
		}
	}
	return len(seen)
}

// structureGaps lists the rule set's required components the segmentation
// did not find, and whether the document type has its own rule set (an
// unknown type falls back to the shared base rules).
func structureGaps(report *docformat.Report, rs *docformat.RuleSet) ([]string, bool) {
	var missing []string
	for _, key := range rs.RequiredComponents {
		if c := report.Components[key]; c == nil || !c.Found {
			missing = append(missing, key)
		}
	}
	used := ""
	if report.DocumentType != nil {
		used = report.DocumentType.Used
	}
	return missing, containsString(docformat.AvailableTypes(), used)
}

// structureWarning explains why the fixes were not applied (or, on a dry
// run, why they would not be without force).
func structureWarning(report *docformat.Report, missing []string, typeKnown, blocked bool) string {
	var b strings.Builder
	b.WriteString("⚠ CẢNH BÁO: chưa xác định được cấu trúc văn bản một cách tin cậy")
	if blocked {
		b.WriteString(", nên CHƯA sửa tài liệu — dưới đây chỉ là kế hoạch")
	}
	b.WriteString(".\n")
	if len(missing) > 0 {
		names := make([]string, len(missing))
		for i, k := range missing {
			names[i] = docformat.ComponentName(k) + " (" + k + ")"
		}
		b.WriteString("- Không tìm thấy thành phần bắt buộc: " + strings.Join(names, ", ") + ".\n")
	}
	if !typeKnown {
		used := ""
		if report.DocumentType != nil {
			used = report.DocumentType.Used
		}
		fmt.Fprintf(&b, "- Không nhận dạng được loại văn bản (%q), đang dùng bộ quy tắc chung.\n", used)
	}
	b.WriteString("Định dạng có thể bị áp nhầm thành phần. Hãy hỏi người dùng xác nhận kế hoạch (nên hỏi rõ loại văn bản, " +
		"ví dụ công văn, quyết định, tờ trình, rồi truyền document_type), sau đó gọi lại với force=true để áp dụng.")
	return b.String()
}

// apply writes the plan into doc as tracked formatting changes in one
// docxedit batch (one rewrite of document.xml). An edit docxedit rejects is
// left out — recorded under skipped and removed from applied — and the rest
// still go through. It returns how many edits were written.
func (p *formatPlan) apply(doc *docxedit.Document) (int, error) {
	type failure struct {
		checks       []string
		para, sectIx int
		err          error
	}
	var failures []failure
	written := 0
	err := doc.Batch(func(b *docxedit.Batch) error {
		for _, i := range sortedKeys(p.paras) {
			e := p.paras[i]
			if e.run != nil {
				if err := b.SetRunProps(i, *e.run, documentEditAuthor); err != nil {
					failures = append(failures, failure{e.runChecks, i, -1, err})
				} else {
					written++
				}
			}
			if e.pp != nil {
				if err := b.SetParaProps(i, *e.pp, documentEditAuthor); err != nil {
					failures = append(failures, failure{e.ppChecks, i, -1, err})
				} else {
					written++
				}
			}
		}
		for _, i := range sortedKeys(p.sections) {
			e := p.sections[i]
			if err := b.SetSectionProps(i, e.props, documentEditAuthor); err != nil {
				failures = append(failures, failure{e.checks, -1, i, err})
			} else {
				written++
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for _, f := range failures {
		where := fmt.Sprintf("đoạn [%d]", f.para)
		if f.para < 0 {
			where = fmt.Sprintf("section %d", f.sectIx)
		}
		for _, id := range f.checks {
			p.skipped = append(p.skipped, SkippedFormatFix{CheckID: id,
				Reason: where + " không sửa được: " + f.err.Error()})
			p.dropApplied(id, f.para, f.sectIx)
		}
	}
	return written, nil
}

// dropApplied removes a failed paragraph (or section) from check id's
// applied entry, and the entry itself once it is empty.
func (p *formatPlan) dropApplied(id string, para, sect int) {
	out := p.applied[:0]
	for _, f := range p.applied {
		if f.CheckID == id {
			f.Paragraphs = withoutInt(f.Paragraphs, para)
			f.Sections = withoutInt(f.Sections, sect)
			if len(f.Paragraphs) == 0 && len(f.Sections) == 0 {
				continue
			}
		}
		out = append(out, f)
	}
	p.applied = out
}

func withoutInt(xs []int, x int) []int {
	out := []int{}
	for _, v := range xs {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}

func sortedKeys[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

func isFailing(status string) bool {
	return status == docformat.StatusFail || status == docformat.StatusWarn
}

// buildFormatPlan turns the report's failed/warned checks into paragraph
// and section edits; what cannot be fixed mechanically goes to manual.
func buildFormatPlan(report *docformat.Report, rs *docformat.RuleSet, layout *docformat.Layout, only []string) *formatPlan {
	plan := newFormatPlan()
	wanted := map[string]bool{}
	for _, id := range only {
		if id = strings.TrimSpace(id); id != "" {
			wanted[id] = true
		}
	}
	seen := map[string]bool{}
	for _, c := range report.Checks {
		seen[c.ID] = true
		if len(wanted) > 0 && !wanted[c.ID] {
			continue
		}
		if !isFailing(c.Status) {
			if len(wanted) > 0 {
				plan.skipped = append(plan.skipped, SkippedFormatFix{CheckID: c.ID,
					Reason: "quy tắc này đang đạt (" + c.Status + "), không cần sửa"})
			}
			continue
		}
		rule := rs.CheckByID(c.ID)
		if rule == nil {
			plan.manual = append(plan.manual, ManualFormatFix{CheckID: c.ID, Desc: c.Desc})
			continue
		}
		switch rule.Kind {
		case "prop", "":
			if !planPropFix(plan, report, layout, c, *rule) {
				plan.manual = append(plan.manual, ManualFormatFix{CheckID: c.ID, Desc: c.Desc})
			}
		case "page_setup":
			if !planPageFix(plan, layout, c, *rule) {
				plan.manual = append(plan.manual, ManualFormatFix{CheckID: c.ID, Desc: c.Desc})
			}
		default:
			plan.manual = append(plan.manual, ManualFormatFix{CheckID: c.ID, Desc: c.Desc})
		}
	}
	for id := range wanted {
		if !seen[id] {
			plan.skipped = append(plan.skipped, SkippedFormatFix{CheckID: id,
				Reason: "không có quy tắc này trong bộ quy tắc " + report.DocumentType.RuleSet})
		}
	}
	sort.SliceStable(plan.skipped, func(i, j int) bool { return plan.skipped[i].CheckID < plan.skipped[j].CheckID })
	return plan
}

// ruleTarget is the value a fix sets for one paragraph, nil when the rule
// gives no mechanical target.
func ruleTarget(rule docformat.RuleCheck, actual any) any {
	switch rule.Op {
	case "eq":
		return rule.Value
	case "ne":
		if b, ok := rule.Value.(bool); ok {
			return !b
		}
	case "in":
		if list, ok := rule.Value.([]any); ok && len(list) > 0 {
			return list[0]
		}
	case "range":
		list, ok := rule.Value.([]any)
		if !ok || len(list) != 2 {
			return nil
		}
		lo, ok1 := list[0].(float64)
		hi, ok2 := list[1].(float64)
		if !ok1 || !ok2 {
			return nil
		}
		if a, ok := actual.(float64); ok && a > hi {
			return hi
		}
		return lo
	}
	return nil
}

var docxAlignment = map[string]string{"left": "left", "center": "center", "right": "right", "justify": "both", "both": "both"}

// propEdit builds the paragraph edit setting prop to v, with a Vietnamese
// description of the change; ok is false when v does not fit the prop.
func propEdit(prop string, v any) (paraEdit, string, bool) {
	switch prop {
	case "font_name":
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return paraEdit{}, "", false
		}
		return paraEdit{run: &docxedit.RunProps{Font: &s}}, "phông " + s, true
	case "size_pt":
		f, ok := v.(float64)
		if !ok || f <= 0 {
			return paraEdit{}, "", false
		}
		return paraEdit{run: &docxedit.RunProps{SizePt: &f}}, "cỡ chữ " + strconv.FormatFloat(f, 'f', -1, 64), true
	case "bold", "italic":
		b, ok := v.(bool)
		if !ok {
			return paraEdit{}, "", false
		}
		word := map[string]string{"bold": "in đậm", "italic": "in nghiêng"}[prop]
		if !b {
			word = "bỏ " + word
		}
		rp := &docxedit.RunProps{}
		if prop == "bold" {
			rp.Bold = &b
		} else {
			rp.Italic = &b
		}
		return paraEdit{run: rp}, word, true
	case "alignment":
		s, _ := v.(string)
		a, ok := docxAlignment[s]
		if !ok {
			return paraEdit{}, "", false
		}
		return paraEdit{pp: &docxedit.ParaProps{Alignment: &a}}, "căn " + alignmentVI[s], true
	}
	return paraEdit{}, "", false
}

func paraActual(p *docformat.Para, prop string) any {
	switch prop {
	case "size_pt":
		if p.SizePt != nil {
			return *p.SizePt
		}
	case "font_name":
		if p.FontName != nil {
			return *p.FontName
		}
	case "alignment":
		return p.Alignment
	}
	return nil
}

// planPropFix plans a prop rule over every paragraph of its component that
// breaks it. It returns false when the rule is not mechanically fixable.
func planPropFix(plan *formatPlan, report *docformat.Report, layout *docformat.Layout, c docformat.CheckResult, rule docformat.RuleCheck) bool {
	switch rule.Prop {
	case "font_name", "size_pt", "bold", "italic", "alignment":
	default:
		return false
	}
	if t := ruleTarget(rule, nil); t == nil {
		return false
	} else if _, _, ok := propEdit(rule.Prop, t); !ok {
		return false
	}
	comp := report.Components[rule.Component]
	if comp == nil || !comp.Found {
		plan.skipped = append(plan.skipped, SkippedFormatFix{CheckID: c.ID, Reason: "không xác định được các đoạn của thành phần " + rule.Component})
		return true
	}
	fix := AppliedFormatFix{CheckID: c.ID, Paragraphs: []int{}}
	var changes []string
	var split []string
	for _, idx := range comp.Paras {
		if idx < 0 || idx >= len(layout.Paragraphs) {
			continue
		}
		p := layout.Paragraphs[idx]
		if strings.TrimSpace(p.Text) == "" {
			continue
		}
		if p.Zone == docformat.ZoneSplit {
			split = append(split, strconv.Itoa(idx))
			continue
		}
		if ok, err := docformat.ParaSatisfies(p, rule); err == nil && ok {
			continue
		}
		edit, change, ok := propEdit(rule.Prop, ruleTarget(rule, paraActual(p, rule.Prop)))
		if !ok {
			continue
		}
		plan.addParaEdit(idx, c.ID, edit)
		fix.Paragraphs = append(fix.Paragraphs, idx)
		if !containsString(changes, change) {
			changes = append(changes, change)
		}
	}
	if len(split) > 0 {
		plan.skipped = append(plan.skipped, SkippedFormatFix{CheckID: c.ID,
			Reason: "đoạn [" + strings.Join(split, "], [") + "] chia hai cột bằng tab: định dạng từng bên phải sửa thủ công (nên chuyển sang bảng hai cột)"})
	}
	if len(fix.Paragraphs) == 0 {
		if len(split) == 0 {
			plan.skipped = append(plan.skipped, SkippedFormatFix{CheckID: c.ID, Reason: "không có đoạn nào cần sửa"})
		}
		return true
	}
	fix.Change = strings.Join(changes, "; ")
	plan.applied = append(plan.applied, fix)
	return true
}

func sectionMM(s *docformat.Section, prop string) *float64 {
	switch prop {
	case "page_width_mm":
		return s.PageWidthMM
	case "page_height_mm":
		return s.PageHeightMM
	case "margin_top_mm":
		return s.MarginTopMM
	case "margin_bottom_mm":
		return s.MarginBottomMM
	case "margin_left_mm":
		return s.MarginLeftMM
	case "margin_right_mm":
		return s.MarginRightMM
	}
	return nil
}

// pageTargetMM is the value a page-setup rule asks for: the conventional
// NĐ30 value when the rule allows it, else the nearest bound.
func pageTargetMM(rule docformat.RuleCheck) (lo, hi, target float64, ok bool) {
	conv, known := conventionalPageMM[rule.Prop]
	if !known {
		return 0, 0, 0, false
	}
	switch rule.Op {
	case "range":
		list, isList := rule.Value.([]any)
		if !isList || len(list) != 2 {
			return 0, 0, 0, false
		}
		l, ok1 := list[0].(float64)
		h, ok2 := list[1].(float64)
		if !ok1 || !ok2 {
			return 0, 0, 0, false
		}
		target = conv
		if target < l {
			target = l
		}
		if target > h {
			target = h
		}
		return l, h, target, true
	case "eq":
		v, isNum := rule.Value.(float64)
		if !isNum {
			return 0, 0, 0, false
		}
		return v, v, v, true
	}
	return 0, 0, 0, false
}

// planPageFix sets the rule's page value on every section that breaks it.
func planPageFix(plan *formatPlan, layout *docformat.Layout, c docformat.CheckResult, rule docformat.RuleCheck) bool {
	lo, hi, target, ok := pageTargetMM(rule)
	if !ok {
		return false
	}
	fix := AppliedFormatFix{CheckID: c.ID, Paragraphs: []int{}, Sections: []int{}}
	pt := target * mmToPt
	for i, s := range layout.Sections {
		if v := sectionMM(s, rule.Prop); v != nil && *v >= lo && *v <= hi {
			continue
		}
		se := plan.sections[i]
		if se == nil {
			se = &sectionEdit{}
			plan.sections[i] = se
		}
		se.checks = appendUnique(se.checks, c.ID)
		sp := &se.props
		v := pt
		switch rule.Prop {
		case "page_width_mm":
			sp.PageWidthPt = &v
		case "page_height_mm":
			sp.PageHeightPt = &v
		case "margin_top_mm":
			sp.MarginTopPt = &v
		case "margin_bottom_mm":
			sp.MarginBottomPt = &v
		case "margin_left_mm":
			sp.MarginLeftPt = &v
		case "margin_right_mm":
			sp.MarginRightPt = &v
		}
		if (rule.Prop == "page_width_mm" || rule.Prop == "page_height_mm") &&
			s.Orientation != nil && *s.Orientation == "landscape" {
			portrait := "portrait"
			sp.Orientation = &portrait
		}
		fix.Sections = append(fix.Sections, i)
	}
	if len(fix.Sections) == 0 {
		plan.skipped = append(plan.skipped, SkippedFormatFix{CheckID: c.ID, Reason: "không có section nào cần sửa"})
		return true
	}
	fix.Change = pagePropVI[rule.Prop] + " " + strconv.FormatFloat(target, 'f', -1, 64) + "mm"
	plan.applied = append(plan.applied, fix)
	return true
}

func joinInts(xs []int) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.Itoa(x)
	}
	return strings.Join(parts, ", ")
}

func renderFormatPlan(fileName string, revision int, report *docformat.Report, plan *formatPlan, dryRun bool) string {
	var b strings.Builder
	nParas := plan.paragraphCount()
	switch {
	case len(plan.applied) == 0:
		fmt.Fprintf(&b, "Không có lỗi thể thức nào của %s sửa tự động được (bộ quy tắc: %s).\n", fileName, report.DocumentType.RuleSet)
	case dryRun:
		fmt.Fprintf(&b, "KẾ HOẠCH (chưa sửa tài liệu): %d sửa thể thức trên %d đoạn của %s (bộ quy tắc: %s). Gọi lại với dry_run=false để áp dụng.\n",
			len(plan.applied), nParas, fileName, report.DocumentType.RuleSet)
	default:
		fmt.Fprintf(&b, "Đã áp dụng %d sửa thể thức dạng track changes trên %d đoạn của %s (phiên bản %d, bộ quy tắc: %s); người dùng có thể chấp nhận/từ chối từng thay đổi trong trình soạn thảo:\n",
			len(plan.applied), nParas, fileName, revision, report.DocumentType.RuleSet)
	}
	for _, f := range plan.applied {
		if len(f.Sections) > 0 {
			fmt.Fprintf(&b, "- %s: %s (section %s)\n", f.CheckID, f.Change, joinInts(f.Sections))
		} else {
			fmt.Fprintf(&b, "- %s: %s — đoạn [%s]\n", f.CheckID, f.Change, joinInts(f.Paragraphs))
		}
	}
	if len(plan.skipped) > 0 {
		b.WriteString("\nBỏ qua:\n")
		for _, s := range plan.skipped {
			fmt.Fprintf(&b, "- %s: %s\n", s.CheckID, s.Reason)
		}
	}
	if len(plan.manual) > 0 {
		b.WriteString("\nCần sửa thủ công:\n")
		for _, m := range plan.manual {
			fmt.Fprintf(&b, "- %s: %s\n", m.CheckID, m.Desc)
		}
	}
	return b.String()
}
