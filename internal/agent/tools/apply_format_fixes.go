package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

var applyFormatFixesTool = BaseTool{
	name: ToolApplyFormatFixes,
	description: `Fix the layout ("thể thức") of the Word document open in this conversation's editor to Nghị định 30/2020/NĐ-CP. It re-runs the NĐ30 rule checks and plans the corrections that can be made mechanically. It works in two steps, so nothing is changed before the user agrees:

1. review (the default): the document is NOT changed. The result is the plan, and the editor underlines in red every paragraph the plan would change and every finding left for a manual fix, so the user sees where each one is.
2. apply=true: the editor applies the planned corrections as ordinary formatting edits (Ctrl+Z undoes them); findings left for a manual fix stay underlined.

Use it only when the user asks to fix the format ("sửa thể thức", "chuẩn hóa"). It corrects:

- font (Times New Roman), font size, bold/italic and paragraph alignment of each component (quốc hiệu, tiêu ngữ, trích yếu, nội dung, chữ ký, nơi nhận…) — every paragraph of the component that breaks the rule, not only the samples a check listed;
- paper size (A4) and page margins.

If the document's structure cannot be identified reliably (a required component such as quốc hiệu or chữ ký was not found, or the document type is unknown), nothing is changed: the result is the plan with a warning. Confirm with the user — ideally ask them which document type it is and pass document_type — then call again with force=true.

It does NOT change line spacing, spacing before/after paragraphs or indents. Position (left/right column), wording, missing components, component order and lines split in two columns by a tab cannot be fixed this way either: they are listed as needing a manual fix (missing parts can be added with insert_paragraphs).

## When to Use

After check_document_format (or when the user directly asks to normalise the layout). Call it first WITHOUT apply: show the plan, say the red underlines show where, and ask whether to apply. Pass apply=true only when the user then agrees in a later message ("áp dụng", "đồng ý sửa", "sửa đi"), or when the current message itself says to apply without review ("sửa luôn", "áp dụng ngay", "tự sửa giúp"). Report what changed (Ctrl+Z or the revision history undo it). dry_run=true returns the plan without underlining anything — only when the user asks to see the plan alone ("chỉ xem kế hoạch").

## Input

- check_ids: rule ids to fix (as reported by the check, e.g. "noi_dung.font", "trich_yeu.size", "page.margin.left"); omit to fix every fixable rule. Font, size, bold/italic and alignment rules are applied paragraph by paragraph: each paragraph that breaks one is fixed even when the check as a whole passes (e.g. one body paragraph in Arial among paragraphs in Times New Roman).
- document_type: rule set to apply (cong_van, quyet_dinh, …); omit to auto-detect.
- apply: true to change the document — only after the user agreed to the plan (see When to Use). Omitted: review only.
- dry_run: true to only return the plan, without underlining — only when the user asks to see the plan alone.
- force: true to apply even though the structure could not be identified reliably; only after the user confirmed.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + documentParamSchema + `,
    "check_ids": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Rule ids to fix, e.g. noi_dung.font, trich_yeu.size, page.margin.left; omit to fix every fixable rule (formatting rules paragraph by paragraph)"
    },
    "document_type": {
      "type": "string",
      "description": "Optional rule set, e.g. cong_van, quyet_dinh, bao_cao, to_trinh; omit to auto-detect"
    },
    "apply": {
      "type": "boolean",
      "description": "true: apply the planned corrections — only after the user agreed to the plan; omitted: review (plan + red underlines, nothing changed)"
    },
    "dry_run": {
      "type": "boolean",
      "description": "true: return the fix plan only, without underlining anything"
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
	// Apply changes the document; without it a call only reviews.
	Apply    bool   `json:"apply"`
	Force    bool   `json:"force"`
	Document string `json:"document"`
}

// ApplyFormatFixesTool plans the corrections of mechanically fixable NĐ30
// findings in the session's workspace document; the editor applies them.
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
	// ComponentParagraphs is how many non-empty paragraphs of the rule's
	// component were checked (Paragraphs are the ones that break the rule).
	ComponentParagraphs int `json:"component_paragraphs,omitempty"`
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
	// Paragraphs the finding names (its evidence), marked in the editor.
	Paragraphs []int `json:"paragraphs,omitempty"`
	spots      []formatSpot
}

// manualFix records a finding left for the user, with the places it names.
func manualFix(c docformat.CheckResult, rule *docformat.RuleCheck) ManualFormatFix {
	kind := ""
	if rule != nil {
		kind = rule.Kind
	}
	m := ManualFormatFix{CheckID: c.ID, Desc: c.Desc, spots: checkSpots(c, kind)}
	for _, s := range m.spots {
		m.Paragraphs = append(m.Paragraphs, s.Para)
	}
	return m
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

// twipsPerMM converts millimetres to twips, as docformat reads them.
const twipsPerMM = 1440.0 / 25.4

// round2 rounds to two decimals like docformat's reader (Python round).
func round2(x float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 2, 64), 64)
	return v
}

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
	// A real run snapshots first (the restore point, and a force-save so
	// the plan sees the editor's latest text); a dry run only reads.
	var content []byte
	var ws *types.DocumentWorkspace
	seq := 0
	target, err := resolveTargetDocument(ctx, t.workspace, t.sessionID, in.Document, !in.DryRun)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	// review (the default) changes nothing but underlines: it snapshots too
	review := !in.DryRun && !in.Apply
	switch {
	case in.DryRun:
		content, ws, err = readListedDocument(ctx, t.workspace, t.sessionID, target)
	case review:
		content, ws, seq, err = snapshotDocument(ctx, t.workspace, t.sessionID, target.ID, "đánh dấu lỗi thể thức")
	default:
		content, ws, seq, err = snapshotDocument(ctx, t.workspace, t.sessionID, target.ID, "chuẩn hóa thể thức")
	}
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
	guarded := unreliable && !in.Force
	dryRun := in.DryRun || review || guarded

	var ops []DocumentOp
	// underlined where they are: what is left for a manual fix and, in a
	// review, every paragraph the plan would change
	var spots []formatSpot
	if review {
		for _, i := range sortedKeys(plan.paras) {
			spots = append(spots, formatSpot{Para: i})
		}
	}
	for _, m := range plan.manual {
		spots = append(spots, m.spots...)
	}
	if in.DryRun || guarded {
		spots = nil
	}
	marks := 0
	if (!dryRun && plan.hasEdits()) || len(spots) > 0 {
		doc, err := docxedit.Open(content)
		if err != nil {
			return &types.ToolResult{Success: false, Error: "không đọc được tài liệu: " + err.Error()}, nil
		}
		// formatting changes no text: one virtual document serves both
		vdoc := newVirtualDoc(doc.Paragraphs())
		if !dryRun && plan.hasEdits() {
			ops = plan.ops(vdoc)
		}
		markOps := formatMarkOps(vdoc, spots)
		marks = len(markOps)
		ops = append(ops, markOps...)
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
	output := renderFormatPlan(ws.FileName, report, plan, dryRun)
	blocked := guarded && in.Apply
	switch {
	case unreliable && !in.Force:
		output = structureWarning(report, missing, typeKnown, blocked) + "\n\n" + output
	case !dryRun && !in.Force:
		if n := plan.paragraphCount(); n > largeFixParagraphs {
			output = fmt.Sprintf("Lưu ý: lần sửa này đổi định dạng của %d đoạn (nhiều hơn %d).\n", n, largeFixParagraphs) + output
		}
	}
	if missing == nil {
		missing = []string{}
	}
	if len(ops) > 0 {
		output += "\n" + editorAppliedNote + "\n"
	}
	switch {
	case review && len(plan.applied) > 0:
		// the one change the user can agree to: these formatting fixes
		output += "\nCHƯA SỬA GÌ"
		if marks > 0 {
			output += ": các đoạn sẽ được sửa và các lỗi cần sửa thủ công đã được gạch chân đỏ trong trình soạn thảo"
		}
		output += ". Nói ngắn gọn với người dùng: nếu muốn áp dụng các sửa định dạng trong kế hoạch (chỉ font, cỡ chữ, in đậm/nghiêng, căn lề, khổ giấy, lề trang), bấm nút \"Áp dụng các sửa thể thức này\" hoặc trả lời \"đồng ý\"; chỉ khi họ đồng ý mới gọi lại với apply=true. Không đề nghị sửa gì khác (chính tả, nội dung, chèn dòng): những lỗi đó chỉ được chỉ ra kèm cách sửa để người dùng tự sửa.\n"
	case review:
		// nothing mechanical to apply: nothing to offer either
		output += "\nCHƯA SỬA GÌ và không có sửa định dạng nào áp dụng tự động được. Chỉ nêu các lỗi cần sửa thủ công kèm cách sửa để người dùng tự sửa; KHÔNG đề nghị sửa giúp, KHÔNG hỏi \"Bạn có muốn tôi sửa…\".\n"
	case marks > 0:
		output += "Những lỗi cần sửa thủ công được gạch chân đỏ tại đoạn tương ứng trong trình soạn thảo (Ctrl+Z để bỏ gạch chân).\n"
	}
	data := opsData(ops, seq, ws)
	data["file_name"] = ws.FileName
	data["document_type"] = report.DocumentType.Used
	data["applied"] = applied
	data["skipped"] = skipped
	data["manual"] = manual
	data["dry_run"] = dryRun
	data["review"] = review
	data["blocked"] = blocked
	data["missing_components"] = missing
	data["segmentation"] = report.Segmentation.Method
	return &types.ToolResult{Success: true, Output: output, Data: data}, nil
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
		b.WriteString(", nên CHƯA sửa gì — dưới đây chỉ là kế hoạch")
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

// ops turns the plan into editor ops: one formatParagraph per paragraph
// (its run and paragraph changes merged) and one pageSetup for the page
// changes of every section. Formatting changes no text, so every anchor is
// computed on the document as read.
func (p *formatPlan) ops(vdoc *virtualDoc) []DocumentOp {
	var ops []DocumentOp
	for _, i := range sortedKeys(p.paras) {
		e := p.paras[i]
		op := DocumentOp{Op: OpFormatParagraph, Anchor: vdoc.anchor(i)}
		if r := e.run; r != nil {
			if r.Font != nil {
				op.Font = *r.Font
			}
			op.SizePt, op.Bold, op.Italic = r.SizePt, r.Bold, r.Italic
		}
		if e.pp != nil && e.pp.Alignment != nil {
			op.Alignment = *e.pp.Alignment
		}
		ops = append(ops, op)
	}
	if len(p.sections) > 0 {
		op := DocumentOp{Op: OpPageSetup}
		mm := func(pt *float64) float64 { return round2(*pt * 20 / twipsPerMM) }
		for _, i := range sortedKeys(p.sections) {
			sp := p.sections[i].props
			for key, v := range map[string]*float64{"top": sp.MarginTopPt, "bottom": sp.MarginBottomPt,
				"left": sp.MarginLeftPt, "right": sp.MarginRightPt} {
				if v != nil {
					if op.MarginsMm == nil {
						op.MarginsMm = map[string]float64{}
					}
					op.MarginsMm[key] = mm(v)
				}
			}
			if sp.PageWidthPt != nil || sp.PageHeightPt != nil {
				op.A4 = true
			}
		}
		ops = append(ops, op)
	}
	return ops
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

// perParagraphProps are the paragraph formatting props fixed paragraph by
// paragraph: every paragraph of the component that breaks the rule is
// scheduled, whatever the check's overall status. A rule with mode
// "majority" or "any" passes while some paragraphs still break it (one body
// paragraph in Arial among four in Times New Roman), and those are fixed too.
var perParagraphProps = map[string]bool{"font_name": true, "size_pt": true, "bold": true, "italic": true, "alignment": true}

// buildFormatPlan turns the report's checks into paragraph and section
// edits: per-paragraph prop rules are planned for every paragraph that
// breaks them, other fixable rules when their check failed/warned; what
// cannot be fixed mechanically goes to manual. With only (explicit rule
// ids) just those rules are considered.
func buildFormatPlan(report *docformat.Report, rs *docformat.RuleSet, layout *docformat.Layout, only []string) *formatPlan {
	plan := newFormatPlan()
	wanted := map[string]bool{}
	for _, id := range only {
		if id = strings.TrimSpace(id); id != "" {
			wanted[id] = true
		}
	}
	explicit := len(wanted) > 0
	passing := func(c docformat.CheckResult) {
		if explicit {
			plan.skipped = append(plan.skipped, SkippedFormatFix{CheckID: c.ID,
				Reason: "quy tắc này đang đạt (" + c.Status + "), không cần sửa"})
		}
	}
	seen := map[string]bool{}
	for _, c := range report.Checks {
		seen[c.ID] = true
		if explicit && !wanted[c.ID] {
			continue
		}
		failing := isFailing(c.Status)
		rule := rs.CheckByID(c.ID)
		if rule != nil && (rule.Kind == "prop" || rule.Kind == "") && perParagraphProps[rule.Prop] {
			switch planPropFix(plan, report, layout, c, *rule, failing) {
			case propNotFixable:
				if failing {
					plan.manual = append(plan.manual, manualFix(c, rule))
				} else {
					passing(c)
				}
			case propNothing:
				passing(c)
			}
			continue
		}
		if !failing {
			passing(c)
			continue
		}
		if rule == nil {
			plan.manual = append(plan.manual, manualFix(c, rule))
			continue
		}
		switch rule.Kind {
		case "prop", "":
			if planPropFix(plan, report, layout, c, *rule, true) == propNotFixable {
				plan.manual = append(plan.manual, manualFix(c, rule))
			}
		case "page_setup":
			if !planPageFix(plan, layout, c, *rule) {
				plan.manual = append(plan.manual, manualFix(c, rule))
			}
		default:
			plan.manual = append(plan.manual, manualFix(c, rule))
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

// propFixResult is what planPropFix did with a rule.
type propFixResult int

const (
	propNotFixable propFixResult = iota // no mechanical fix for this rule
	propNothing                         // fixable, but nothing planned or reported
	propPlanned                         // paragraphs scheduled and/or a skip reason recorded
)

// planPropFix plans a prop rule over every paragraph of its component that
// breaks it (ParaSatisfies), whatever the rule's mode. verbose records why
// nothing was planned (component not found, no paragraph to fix); it is off
// for a rule whose check passes, where that is expected. Paragraphs split
// in two columns by a tab that break the rule are always reported.
func planPropFix(plan *formatPlan, report *docformat.Report, layout *docformat.Layout, c docformat.CheckResult, rule docformat.RuleCheck, verbose bool) propFixResult {
	if !perParagraphProps[rule.Prop] {
		return propNotFixable
	}
	if t := ruleTarget(rule, nil); t == nil {
		return propNotFixable
	} else if _, _, ok := propEdit(rule.Prop, t); !ok {
		return propNotFixable
	}
	comp := report.Components[rule.Component]
	if comp == nil || !comp.Found {
		if !verbose {
			return propNothing
		}
		plan.skipped = append(plan.skipped, SkippedFormatFix{CheckID: c.ID, Reason: "không xác định được các đoạn của thành phần " + rule.Component})
		return propPlanned
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
		fix.ComponentParagraphs++
		if ok, err := docformat.ParaSatisfies(p, rule); err == nil && ok {
			continue
		}
		if p.Zone == docformat.ZoneSplit {
			split = append(split, strconv.Itoa(idx))
			continue
		}
		edit, change, ok := propEdit(rule.Prop, paraTarget(rule, paraActual(p, rule.Prop)))
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
		if len(split) > 0 {
			return propPlanned
		}
		if !verbose {
			return propNothing
		}
		plan.skipped = append(plan.skipped, SkippedFormatFix{CheckID: c.ID, Reason: "không có đoạn nào cần sửa"})
		return propPlanned
	}
	fix.Change = strings.Join(changes, "; ")
	plan.applied = append(plan.applied, fix)
	return propPlanned
}

// paraTarget is ruleTarget for one paragraph, with a font size range bound
// moved onto the half-point grid Word stores (w:sz) so that the written
// size still satisfies the rule: a lower bound rounds up, an upper bound
// down.
func paraTarget(rule docformat.RuleCheck, actual any) any {
	t := ruleTarget(rule, actual)
	f, isNum := t.(float64)
	if rule.Prop != "size_pt" || rule.Op != "range" || !isNum {
		return t
	}
	lo, hi, ok := rangeBounds(rule.Value)
	if !ok {
		return t
	}
	var cur *float64
	if a, ok := actual.(float64); ok {
		cur = &a
	}
	steps, ok := gridTarget(lo, hi, f, cur, 2, func(n int) float64 { return float64(n) / 2 })
	if !ok {
		return t
	}
	return float64(steps) / 2
}

func rangeBounds(v any) (lo, hi float64, ok bool) {
	list, isList := v.([]any)
	if !isList || len(list) != 2 {
		return 0, 0, false
	}
	l, ok1 := list[0].(float64)
	h, ok2 := list[1].(float64)
	return l, h, ok1 && ok2
}

// gridTarget chooses the value a fix writes for target — a bound of [lo,
// hi] or a value inside it — on the integer grid the file stores (twips,
// half-points…; perUnit grid steps per unit of target), so that the value
// the checker reads back (read) still lies in the range: a lower bound
// rounds up to the next step, an upper bound down, a value inside to the
// nearest step. When that lands on current (the failing value, e.g. 15mm
// rounded to 850 twips = 14.99mm) it moves one step further into the range.
// ok is false when no nearby step reads back inside the range.
func gridTarget(lo, hi, target float64, current *float64, perUnit float64, read func(steps int) float64) (int, bool) {
	const eps = 1e-9
	x := target * perUnit
	var n, inward int
	switch {
	case target <= lo:
		n, inward = int(math.Ceil(x-eps)), 1
	case target >= hi:
		n, inward = int(math.Floor(x+eps)), -1
	default:
		n = int(math.Round(x))
		if read(n) < target {
			inward = 1
		} else {
			inward = -1
		}
	}
	if current != nil && n == int(math.Round(*current*perUnit)) && inward != 0 {
		n += inward
	}
	in := func(n int) bool { v := read(n); return v >= lo-eps && v <= hi+eps }
	for i := 0; i < 3 && !in(n); i++ {
		n += inward
	}
	return n, in(n)
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
	written := target
	for i, s := range layout.Sections {
		cur := sectionMM(s, rule.Prop)
		if cur != nil && *cur >= lo && *cur <= hi {
			continue
		}
		// the value goes into the file in whole twips: pick one that still
		// satisfies the rule as the checker reads it back
		tw, ok := gridTarget(lo, hi, target, cur, twipsPerMM, func(n int) float64 { return round2(float64(n) / twipsPerMM) })
		if !ok {
			continue
		}
		written = round2(float64(tw) / twipsPerMM)
		se := plan.sections[i]
		if se == nil {
			se = &sectionEdit{}
			plan.sections[i] = se
		}
		se.checks = appendUnique(se.checks, c.ID)
		sp := &se.props
		v := float64(tw) / 20 // docxedit writes round(pt*20) twips: exactly tw
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
	fix.Change = pagePropVI[rule.Prop] + " " + strconv.FormatFloat(written, 'f', -1, 64) + "mm"
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

func renderFormatPlan(fileName string, report *docformat.Report, plan *formatPlan, dryRun bool) string {
	var b strings.Builder
	nParas := plan.paragraphCount()
	switch {
	case len(plan.applied) == 0:
		fmt.Fprintf(&b, "Không có lỗi thể thức nào của %s sửa tự động được (bộ quy tắc: %s).\n", fileName, report.DocumentType.RuleSet)
	case dryRun:
		fmt.Fprintf(&b, "KẾ HOẠCH (chưa sửa tài liệu): %d sửa thể thức trên %d đoạn của %s (bộ quy tắc: %s). Chỉ áp dụng (apply=true) khi người dùng đồng ý.\n",
			len(plan.applied), nParas, fileName, report.DocumentType.RuleSet)
	default:
		fmt.Fprintf(&b, "Sẽ áp dụng %d sửa thể thức trên %d đoạn của %s (bộ quy tắc: %s):\n",
			len(plan.applied), nParas, fileName, report.DocumentType.RuleSet)
	}
	for _, f := range plan.applied {
		if len(f.Sections) > 0 {
			fmt.Fprintf(&b, "- %s: %s (section %s)\n", f.CheckID, f.Change, joinInts(f.Sections))
		} else {
			if f.ComponentParagraphs > 0 {
				fmt.Fprintf(&b, "- %s: %s — %d/%d đoạn [%s]\n", f.CheckID, f.Change, len(f.Paragraphs), f.ComponentParagraphs, joinInts(f.Paragraphs))
			} else {
				fmt.Fprintf(&b, "- %s: %s — đoạn [%s]\n", f.CheckID, f.Change, joinInts(f.Paragraphs))
			}
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
