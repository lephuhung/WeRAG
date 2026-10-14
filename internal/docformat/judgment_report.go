package docformat

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The evaluation report written in code, with the structure the-thuc-chung
// prescribes ("Cách trả lời"): Kết luận, Bảng kiểm (one parsable line per
// item), Lỗi grouped by component (quy định → dòng vi phạm → cách sửa),
// Cảnh báo, and the reminder about spelling and manual checks.

// ChecklistLine is item it's Bảng kiểm line: "2. Ký thay — Lỗi: …".
func ChecklistLine(it JudgmentItem) string {
	if it.Line != "" {
		return it.Line
	}
	head := fmt.Sprintf("%d. %s — ", it.Item, it.Title)
	summaries := func(sev string) string {
		var s []string
		for _, f := range it.Findings {
			if f.Severity == sev {
				s = append(s, f.Summary)
			}
		}
		return strings.Join(s, "; ")
	}
	switch it.Status {
	case JudgeFail:
		return head + "Lỗi: " + summaries("error")
	case JudgeWarn:
		return head + "Cảnh báo: " + summaries("warn")
	case JudgeUnknown, JudgeUndetermined:
		s := head + "Không xác định được từ dữ liệu"
		if it.Reason != "" {
			s += " (" + it.Reason + ")"
		}
		return s
	}
	s := head + "Đạt"
	if it.Note != "" {
		s += " (" + it.Note + ")"
	}
	return s
}

// reportFinding is one entry of the Lỗi / Cảnh báo sections.
type reportFinding struct {
	group string // component name
	text  string // "quy định → dòng vi phạm → cách sửa"
}

// componentGroup names a measured check's group.
func componentGroup(c CheckResult) string {
	if c.Component != nil && *c.Component != "" {
		return ComponentName(*c.Component)
	}
	key, _, _ := strings.Cut(c.ID, ".")
	switch key {
	case "page":
		return "Trang"
	case "component":
		return "Thành phần"
	case "text":
		return "Toàn văn"
	}
	if n, ok := componentNames[key]; ok {
		return n
	}
	return "Khác"
}

// measuredFix is how to fix a measured finding, from its rule.
func measuredFix(rule *RuleCheck) string {
	if rule == nil {
		return "Sửa theo quy định trên."
	}
	v := rule.Value
	num := func(x any) string {
		if f, ok := x.(float64); ok {
			return strconv.FormatFloat(f, 'f', -1, 64)
		}
		return fmt.Sprint(x)
	}
	switch rule.Prop {
	case "size_pt":
		if r, ok := v.([]any); ok && len(r) == 2 {
			if num(r[0]) == num(r[1]) {
				return "Đặt cỡ chữ " + num(r[0]) + "."
			}
			return "Đặt cỡ chữ " + num(r[0]) + "–" + num(r[1]) + "."
		}
	case "bold", "italic":
		word := map[string]string{"bold": "in đậm", "italic": "in nghiêng"}[rule.Prop]
		on := (rule.Op == "eq" && v == true) || (rule.Op == "ne" && v == false)
		if on {
			return "Chuyển sang " + word + "."
		}
		return "Bỏ " + word + "."
	case "alignment":
		names := map[string]string{"center": "căn giữa", "both": "căn đều hai bên", "left": "căn trái", "right": "căn phải"}
		if s, ok := v.(string); ok && names[s] != "" {
			return "Chuyển sang " + names[s] + "."
		}
	case "zone":
		names := map[string]string{"left": "cột trái", "right": "cột phải", "full": "toàn trang"}
		if s, ok := v.(string); ok && names[s] != "" {
			return "Đặt ở " + names[s] + "."
		}
	case "font_name":
		return "Dùng phông chữ theo quy định (Times New Roman, bộ mã Unicode TCVN 6909:2001)."
	}
	switch rule.Kind {
	case "present":
		return "Bổ sung thành phần này."
	case "page_setup":
		return "Chỉnh lại trong Page Setup cho đúng quy định."
	case "page_number":
		return "Đặt số trang ở giữa phần đầu trang (header), không hiện ở trang đầu."
	}
	return "Sửa theo quy định trên."
}

var quotedPrefixRe = regexp.MustCompile(`^"`)

// measuredFindings lists the measured checks of one status.
func measuredFindings(r *Report, status string) []reportFinding {
	rs, _ := RuleSetForReport(r)
	var out []reportFinding
	for _, c := range r.Checks {
		if c.Status != status {
			continue
		}
		rule := "Theo NĐ30: " + c.Desc
		var lines []string
		for _, ev := range c.Evidence {
			txt, ok := ev["text"]
			if !ok {
				continue
			}
			s := fmt.Sprintf("%q", fmt.Sprint(txt))
			if a, ok := ev["actual"]; ok && a != nil {
				s += " (thực tế: " + compactJSON(a) + ")"
			}
			lines = append(lines, s)
			if len(lines) == 3 {
				break
			}
		}
		line := strings.Join(lines, "; ")
		if line == "" && c.Actual != nil {
			line = "thực tế: " + compactJSON(c.Actual)
		}
		var fix string
		if strings.HasPrefix(c.ID, "component.") && strings.HasSuffix(c.ID, ".present") {
			fix = "Bổ sung thành phần này đúng vị trí theo NĐ30."
		} else {
			fix = measuredFix(rs.CheckByID(c.ID))
		}
		out = append(out, reportFinding{componentGroup(c), findingText(rule, line, fix)})
	}
	return out
}

func findingText(rule, line, fix string) string {
	s := rule
	if line != "" {
		if !quotedPrefixRe.MatchString(line) && !strings.HasPrefix(line, "thực tế") {
			line = fmt.Sprintf("%q", line)
		}
		s += " → dòng vi phạm: " + line
	}
	if fix != "" {
		s += " → cách sửa: " + fix
	}
	return s
}

// judgedFindings lists the rule findings of one severity ("error"/"warn").
func judgedFindings(items []JudgmentItem, sev string) []reportFinding {
	var out []reportFinding
	for _, it := range items {
		if it.Item == 1 {
			continue // the measured component.*.present checks
		}
		for _, f := range it.Findings {
			if f.Severity != sev {
				continue
			}
			group := ComponentName(f.Component)
			if f.Component == "" {
				group = checklistTitles[it.Item]
			}
			out = append(out, reportFinding{group, findingText(f.Rule, f.Line, f.Fix)})
		}
	}
	return out
}

func writeGrouped(b *strings.Builder, fs []reportFinding) {
	var groups []string
	by := map[string][]string{}
	for _, f := range fs {
		if _, ok := by[f.group]; !ok {
			groups = append(groups, f.group)
		}
		by[f.group] = append(by[f.group], f.text)
	}
	for _, g := range groups {
		fmt.Fprintf(b, "- **%s**\n", g)
		for _, t := range by[g] {
			fmt.Fprintf(b, "  - %s\n", t)
		}
	}
}

// BuildEvaluation writes the evaluation Markdown from the measured checks
// and the judged items. extraErr / extraWarn are entries the model wrote
// for the items it judged.
func BuildEvaluation(r *Report, items []JudgmentItem, extraErr, extraWarn []string) string {
	errs := append(measuredFindings(r, StatusFail), judgedFindings(items, "error")...)
	warns := append(measuredFindings(r, StatusWarn), judgedFindings(items, "warn")...)
	nErr, nWarn := len(errs)+len(extraErr), len(warns)+len(extraWarn)
	for _, it := range items {
		if it.By == "llm" {
			// the model's line counts when it wrote no entry for it
			if it.Status == JudgeFail && len(extraErr) == 0 {
				nErr++
			}
			if it.Status == JudgeWarn && len(extraWarn) == 0 {
				nWarn++
			}
		}
	}

	var b strings.Builder
	b.WriteString("## Kết luận\n")
	if nErr > 0 {
		fmt.Fprintf(&b, "**Chưa đạt thể thức**: %d lỗi cần sửa", nErr)
	} else {
		b.WriteString("**Đạt thể thức**: không có lỗi")
	}
	if nWarn > 0 {
		fmt.Fprintf(&b, ", %d cảnh báo nên sửa", nWarn)
	}
	b.WriteString(".\n\n## Bảng kiểm\n")
	for _, it := range items {
		b.WriteString(ChecklistLine(it) + "\n")
	}

	b.WriteString("\n## Lỗi (bắt buộc sửa)\n")
	if nErr == 0 {
		b.WriteString("Không có.\n")
	}
	writeGrouped(&b, errs)
	for _, it := range items {
		if it.By == "llm" && it.Status == JudgeFail && len(extraErr) == 0 {
			fmt.Fprintf(&b, "- **%s**\n  - %s\n", it.Title, strings.TrimPrefix(it.Line, fmt.Sprintf("%d. ", it.Item)))
		}
	}
	for _, e := range extraErr {
		b.WriteString(e + "\n")
	}

	b.WriteString("\n## Cảnh báo (nên sửa)\n")
	if nWarn == 0 {
		b.WriteString("Không có.\n")
	}
	writeGrouped(&b, warns)
	for _, it := range items {
		if it.By == "llm" && it.Status == JudgeWarn && len(extraWarn) == 0 {
			fmt.Fprintf(&b, "- **%s**\n  - %s\n", it.Title, strings.TrimPrefix(it.Line, fmt.Sprintf("%d. ", it.Item)))
		}
	}
	for _, w := range extraWarn {
		b.WriteString(w + "\n")
	}

	b.WriteString("\n---\nLưu ý: chính tả do công cụ kiểm tra chính tả rà riêng; con dấu, chữ ký, số trang cần rà soát thủ công trên bản in.\n")
	return b.String()
}
