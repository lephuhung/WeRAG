package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/types"
)

// Structured format findings for the Word add-in's review (its contract
// docs/backend-format-review-contract.md, P0): every failing or warning
// measured check becomes one finding per place it names, so the user goes
// through them one by one. A finding the add-in may correct carries exactly
// one formatParagraph on its own paragraph, with only the property its rule
// sets: the user applies it with a click, and the add-in reads Word back to
// verify. Findings without a mechanical fix (wording, a missing component,
// spacing, page setup) carry no ops: they are shown, and the user fixes them.
// Anchors are computed on the checked file, so they hold for the revision
// the report names.

const (
	// maxFindingsPerCheck caps the places of one rule: a rule broken by
	// every body paragraph would otherwise drown the others.
	maxFindingsPerCheck = 20
	// maxFormatFindings caps one report's findings.
	maxFormatFindings = 150
)

// formatFinding is one reviewable finding of a check (as kept in the cache).
type formatFinding struct {
	ID             string       `json:"id"`
	CheckID        string       `json:"check_id"`
	Title          string       `json:"title"`
	Reason         string       `json:"reason,omitempty"`
	Current        string       `json:"current,omitempty"`
	Expected       string       `json:"expected,omitempty"`
	Anchor         *OpAnchor    `json:"anchor,omitempty"`
	Text           string       `json:"text,omitempty"`
	TextOccurrence int          `json:"text_occurrence,omitempty"`
	Ops            []DocumentOp `json:"ops,omitempty"`
}

// formatFindingsOf builds the findings of a report on the checked content.
// It returns an empty (non-nil) list when the report has none, nil when the
// content cannot be read.
func formatFindingsOf(report *docformat.Report, rs *docformat.RuleSet, content []byte) []formatFinding {
	if report == nil {
		return nil
	}
	doc, err := docxedit.Open(content)
	if err != nil {
		return nil
	}
	findings := buildFormatFindings(report, rs, docformat.InspectDocx(content), newVirtualDoc(doc.Paragraphs()))
	// a document whose structure was not recognised may have its lines
	// labelled as the wrong component, whose formatting a fix would copy:
	// the findings are shown, none is offered as a fix (as apply_format_fixes
	// refuses without force)
	if rs == nil {
		return findings
	}
	if missing, typeKnown := structureGaps(report, rs); len(missing) > 0 || !typeKnown {
		for i := range findings {
			if len(findings[i].Ops) > 0 {
				findings[i].Ops = nil
				findings[i].Reason = unsureStructureReason
			}
		}
	}
	return findings
}

// unsureStructureReason replaces a fix withheld because the document's
// structure was not recognised reliably.
const unsureStructureReason = "Chưa nhận dạng chắc chắn cấu trúc văn bản nên không đề xuất sửa tự động; người dùng xem lại và tự sửa."

func buildFormatFindings(report *docformat.Report, rs *docformat.RuleSet, layout *docformat.Layout, vdoc *virtualDoc) []formatFinding {
	out := []formatFinding{}
	for _, c := range report.Checks {
		if !isFailing(c.Status) || len(out) >= maxFormatFindings {
			continue
		}
		var rule *docformat.RuleCheck
		if rs != nil {
			rule = rs.CheckByID(c.ID)
		}
		found := checkFindings(report, layout, vdoc, c, rule)
		if len(found) > maxFindingsPerCheck {
			found = found[:maxFindingsPerCheck]
		}
		if room := maxFormatFindings - len(out); len(found) > room {
			found = found[:room]
		}
		out = append(out, found...)
	}
	return out
}

// checkFindings are the findings of one failing check: the paragraphs that
// break a per-paragraph formatting rule (each with its fix), else the
// places the check's evidence names, else the check alone.
func checkFindings(report *docformat.Report, layout *docformat.Layout, vdoc *virtualDoc, c docformat.CheckResult, rule *docformat.RuleCheck) []formatFinding {
	base := formatFinding{CheckID: c.ID, Title: findingTitle(c, rule)}
	if rule != nil {
		base.Expected = expectedText(*rule)
	}
	n := 0
	next := func() formatFinding {
		n++
		f := base
		f.ID = c.ID + "#" + strconv.Itoa(n)
		return f
	}
	if found, ok := propFindings(report, layout, vdoc, c, rule, next); ok {
		return found
	}
	kind := ""
	if rule != nil {
		kind = rule.Kind
	}
	var out []formatFinding
	spots := checkSpots(c, kind)
	for _, s := range spots {
		f := next()
		if a := spotAnchor(vdoc, s.Para); a != nil {
			f.Anchor = a
			if s.Text != "" && strings.Contains(vdoc.texts[s.Para], s.Text) {
				f.Text, f.TextOccurrence = s.Text, 1
			}
		}
		f.Current = evidenceValue(c.Evidence, s.Para, rule)
		f.Reason = manualReason(c, rule)
		out = append(out, f)
	}
	if len(out) > 0 {
		return out
	}
	f := next()
	if c.Actual != nil {
		f.Current = valueText(propOf(rule), c.Actual)
	}
	f.Reason = manualReason(c, rule)
	if c.Note != "" {
		f.Reason = strings.TrimSpace(c.Note + " " + f.Reason)
	}
	return []formatFinding{f}
}

// propFindings are the findings of a per-paragraph formatting rule (font,
// size, bold, italic, alignment) that sets a definite value: one for each
// paragraph of its component that breaks it, with the formatParagraph
// fixing only that property. ok is false for any other rule.
func propFindings(report *docformat.Report, layout *docformat.Layout, vdoc *virtualDoc, c docformat.CheckResult, rule *docformat.RuleCheck, next func() formatFinding) ([]formatFinding, bool) {
	if rule == nil || (rule.Kind != "prop" && rule.Kind != "") || !perParagraphProps[rule.Prop] {
		return nil, false
	}
	if t := ruleTarget(*rule, nil); t == nil {
		return nil, false
	} else if _, _, ok := propEdit(rule.Prop, t); !ok {
		return nil, false
	}
	comp := report.Components[rule.Component]
	if comp == nil || !comp.Found || layout == nil {
		return nil, false
	}
	var out []formatFinding
	for _, idx := range docformat.RuleLines(layout, comp, *rule) {
		if idx < 0 || idx >= len(layout.Paragraphs) {
			continue
		}
		p := layout.Paragraphs[idx]
		if ok, err := docformat.ParaSatisfies(p, *rule); err == nil && ok {
			continue
		}
		f := next()
		f.Anchor = spotAnchor(vdoc, idx)
		f.Current = valueText(rule.Prop, docformat.ParaAttr(p, rule.Prop))
		if p.Zone == docformat.ZoneSplit {
			f.Reason = "Dòng chia hai cột bằng tab: định dạng từng bên phải sửa thủ công (nên chuyển sang bảng hai cột)."
			out = append(out, f)
			continue
		}
		edit, change, ok := propEdit(rule.Prop, paraTarget(*rule, paraActual(p, rule.Prop)))
		if !ok {
			continue
		}
		f.Reason = "Đoạn này chưa đúng quy định; sửa: " + change + "."
		if f.Anchor != nil {
			f.Ops = []DocumentOp{paraEditOp(f.Anchor, edit)}
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		// the check failed but no paragraph is to blame (too few lines,
		// the majority rule): report it as a whole
		return nil, false
	}
	return out, true
}

// paraEditOp is the formatParagraph of one rule's edit.
func paraEditOp(anchor *OpAnchor, e paraEdit) DocumentOp {
	op := DocumentOp{Op: OpFormatParagraph, Anchor: anchor}
	if r := e.run; r != nil {
		if r.Font != nil {
			op.Font = *r.Font
		}
		op.SizePt, op.Bold, op.Italic = r.SizePt, r.Bold, r.Italic
	}
	if e.pp != nil && e.pp.Alignment != nil {
		op.Alignment = *e.pp.Alignment
	}
	return op
}

// spotAnchor anchors a body paragraph, nil when it is outside the document
// or has no text to find it by.
func spotAnchor(vdoc *virtualDoc, para int) *OpAnchor {
	if para < 0 || para >= len(vdoc.texts) || anchorText(vdoc.texts[para]) == "" {
		return nil
	}
	return vdoc.anchor(para)
}

func findingTitle(c docformat.CheckResult, rule *docformat.RuleCheck) string {
	title := strings.TrimSpace(docformat.UserDesc(c))
	if title == "" && rule != nil {
		title = strings.TrimSpace(rule.Desc)
	}
	if title == "" {
		title = c.ID
	}
	if comp := componentOf(c, rule); comp != "" {
		name := docformat.ComponentName(comp)
		switch {
		case name == "" || name == comp:
		case strings.Contains(title, "'"+comp+"'"):
			// "phải có thành phần 'quoc_hieu'": the name, not the slug
			title = strings.ReplaceAll(title, "'"+comp+"'", "“"+name+"”")
		case !strings.Contains(strings.ToLower(title), strings.ToLower(name)):
			title = name + ": " + title
		}
	}
	return title
}

func componentOf(c docformat.CheckResult, rule *docformat.RuleCheck) string {
	if c.Component != nil {
		return *c.Component
	}
	if rule != nil {
		return rule.Component
	}
	return ""
}

func manualReason(c docformat.CheckResult, rule *docformat.RuleCheck) string {
	if rule != nil && rule.Kind == "page_setup" {
		return "Chưa đúng quy định; sửa trong Bố cục > Lề/Khổ giấy của Word."
	}
	if c.Status == docformat.StatusWarn {
		return "Cần xem lại; người dùng tự sửa nếu đúng là lỗi."
	}
	return "Chưa đúng quy định; người dùng tự sửa."
}

func propOf(rule *docformat.RuleCheck) string {
	if rule == nil {
		return ""
	}
	return rule.Prop
}

// evidenceValue is the measured value the check quotes for a paragraph.
func evidenceValue(evidence []map[string]any, para int, rule *docformat.RuleCheck) string {
	for _, ev := range evidence {
		if p, ok := evidenceIndex(ev["para"]); ok && p == para {
			return valueText(propOf(rule), ev["actual"])
		}
	}
	return ""
}

// propUnits are the units of the measured numeric props.
var propUnits = map[string]string{
	"size_pt": " pt", "space_before_pt": " pt", "space_after_pt": " pt", "line_spacing_pt": " pt",
	"page_width_mm": " mm", "page_height_mm": " mm", "margin_top_mm": " mm", "margin_bottom_mm": " mm",
	"margin_left_mm": " mm", "margin_right_mm": " mm", "indent_left_mm": " mm", "indent_right_mm": " mm",
	"indent_first_line_mm": " mm", "indent_hanging_mm": " mm",
}

// valueText writes a measured or required value for the user.
func valueText(prop string, v any) string {
	switch x := v.(type) {
	case nil:
		return "không đo được (định dạng trộn hoặc để mặc định)"
	case string:
		if x == "missing" {
			return "không có"
		}
		if prop == "alignment" {
			if vi, ok := alignmentVI[x]; ok {
				return "căn " + vi
			}
		}
		return x
	case bool:
		word := map[string]string{"bold": "in đậm", "italic": "in nghiêng", "underline": "gạch chân", "caps": "chữ in hoa"}[prop]
		switch {
		case word == "" && x:
			return "có"
		case word == "":
			return "không"
		case x:
			return word
		default:
			return "không " + word
		}
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64) + propUnits[prop]
	case int:
		return strconv.Itoa(x) + propUnits[prop]
	case map[string]any, []any:
		// a structured measurement (page numbering…): the title says it
		return ""
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

// expectedText is what a rule requires, for the user.
func expectedText(rule docformat.RuleCheck) string {
	switch rule.Op {
	case "eq":
		return valueText(rule.Prop, rule.Value)
	case "ne":
		if b, ok := rule.Value.(bool); ok {
			return valueText(rule.Prop, !b)
		}
		return "khác " + valueText(rule.Prop, rule.Value)
	case "in":
		if list, ok := rule.Value.([]any); ok {
			parts := make([]string, 0, len(list))
			for _, v := range list {
				parts = append(parts, valueText(rule.Prop, v))
			}
			return strings.Join(parts, " hoặc ")
		}
	case "range":
		if lo, hi, ok := rangeBounds(rule.Value); ok {
			if lo == hi {
				return valueText(rule.Prop, lo)
			}
			return strconv.FormatFloat(lo, 'f', -1, 64) + "–" + valueText(rule.Prop, hi)
		}
	}
	return ""
}

// formatReportID names one evaluation of one file content: the same check
// served from the cache keeps it, a new check gets another.
func formatReportID(key string, at time.Time) string {
	sum := sha256.Sum256([]byte(key + "|" + at.UTC().Format(time.RFC3339Nano)))
	return "fc-" + hex.EncodeToString(sum[:8])
}

// reportFindings converts the kept findings for the API.
func reportFindings(in []formatFinding) []types.DocumentFormatFinding {
	out := make([]types.DocumentFormatFinding, 0, len(in))
	for _, f := range in {
		rf := types.DocumentFormatFinding{
			ID: f.ID, CheckID: f.CheckID, Title: f.Title, Reason: f.Reason, Current: f.Current, Expected: f.Expected,
			Text: f.Text, TextOccurrence: f.TextOccurrence,
		}
		if f.Anchor != nil {
			rf.Anchor = &types.DocumentFormatAnchor{Text: f.Anchor.Text, Occurrence: max(f.Anchor.Occurrence, 1)}
		}
		for _, op := range f.Ops {
			rf.Ops = append(rf.Ops, op)
		}
		out = append(out, rf)
	}
	return out
}
