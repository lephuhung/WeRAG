package tools

import (
	"strings"

	"github.com/Tencent/WeKnora/internal/docformat"
)

// Format findings shown in the editor. A measured NĐ30 finding that names
// its paragraphs (the check's evidence) is marked there, so the user sees
// where it is and not only the list in the chat: the paragraph's text turns
// red, and a finding that quotes the offending characters (a stray symbol
// inside a word) gets them underlined in red, as a spelling error. Findings
// without a paragraph (a missing component, the margins) stay in the chat.

// maxFormatMarks caps the marks of one check: a rule broken by every body
// paragraph would otherwise paint the whole document.
const maxFormatMarks = 40

// formatMarksNote ends the Output of a tool that returned format marks.
const formatMarksNote = "Các đoạn có lỗi thể thức đo được đã được tô đỏ trong trình soạn thảo để người dùng thấy vị trí; nội dung không bị sửa (Ctrl+Z để bỏ đánh dấu). Lỗi không gắn với đoạn nào (thiếu thành phần, lề trang) chỉ nêu trong câu trả lời."

// formatSpot is where one finding is: a paragraph index (as docxedit and
// the layout number them) and, for a character-level finding, the exact
// text inside it.
type formatSpot struct {
	Para int    `json:"para"`
	Text string `json:"text,omitempty"`
}

// formatFlag is one failing or warning check with the places it names.
type formatFlag struct {
	CheckID string       `json:"check_id"`
	Spots   []formatSpot `json:"spots"`
}

// textLevelKinds are the rule kinds whose evidence "actual" is the
// offending text itself (for the others it is a measured property value).
var textLevelKinds = map[string]bool{"stray_chars": true}

// checkSpots reads the places a check's evidence names. kind is the rule's
// kind ("" when unknown).
func checkSpots(c docformat.CheckResult, kind string) []formatSpot {
	var spots []formatSpot
	for _, ev := range c.Evidence {
		p, ok := evidenceIndex(ev["para"])
		if !ok {
			continue
		}
		s := formatSpot{Para: p}
		if textLevelKinds[kind] {
			if a, ok := ev["actual"].(string); ok && strings.TrimSpace(a) != "" {
				s.Text = a
			}
		}
		spots = append(spots, s)
	}
	return spots
}

func evidenceIndex(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	}
	return 0, false
}

// formatFlags keeps the failing and warning checks of a report that name
// a paragraph. rs (may be nil) gives the rule kinds.
func formatFlags(report *docformat.Report, rs *docformat.RuleSet) []formatFlag {
	if report == nil {
		return nil
	}
	var out []formatFlag
	for _, c := range report.Checks {
		if c.Status != docformat.StatusFail && c.Status != docformat.StatusWarn {
			continue
		}
		kind := ""
		if rs != nil {
			if rule := rs.CheckByID(c.ID); rule != nil {
				kind = rule.Kind
			}
		}
		if spots := checkSpots(c, kind); len(spots) > 0 {
			out = append(out, formatFlag{CheckID: c.ID, Spots: spots})
		}
	}
	return out
}

// formatMarkOps turns spots into mark ops on the document as read: a red
// underline on a quoted text found in its paragraph, else red text on the
// paragraph (once per paragraph). Empty paragraphs and indexes outside the
// document are skipped; at most maxFormatMarks ops.
func formatMarkOps(vdoc *virtualDoc, spots []formatSpot) []DocumentOp {
	var ops []DocumentOp
	marked := map[int]bool{}
	underlined := map[string]bool{}
	for _, s := range spots {
		if len(ops) >= maxFormatMarks {
			break
		}
		if s.Para < 0 || s.Para >= len(vdoc.texts) || anchorText(vdoc.texts[s.Para]) == "" {
			continue
		}
		if s.Text != "" && strings.Contains(vdoc.texts[s.Para], s.Text) {
			key := anchorText(vdoc.texts[s.Para]) + "\x00" + s.Text
			if !underlined[key] {
				underlined[key] = true
				ops = append(ops, DocumentOp{Op: OpMark, Anchor: vdoc.anchor(s.Para), Text: s.Text, Style: "underline"})
			}
			continue
		}
		if !marked[s.Para] {
			marked[s.Para] = true
			ops = append(ops, DocumentOp{Op: OpMark, Anchor: vdoc.anchor(s.Para), Style: "color"})
		}
	}
	return ops
}

// flagSpots flattens the spots of flags in order.
func flagSpots(flags []formatFlag) []formatSpot {
	var out []formatSpot
	for _, f := range flags {
		out = append(out, f.Spots...)
	}
	return out
}
