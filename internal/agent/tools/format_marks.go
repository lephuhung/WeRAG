package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
)

// Format findings shown in the editor. A measured NĐ30 finding that names
// its paragraphs (the check's evidence) is marked there, so the user sees
// where it is and not only the list in the chat: the paragraph is
// underlined in red, or, for a finding that quotes the offending characters
// (a stray symbol inside a word), just those characters, as a spelling
// error. The assistant points out; it changes neither the wording nor the
// formatting. Findings without a paragraph (a missing component, the
// margins) stay in the chat.

// maxFormatMarks caps the marks of one check: a rule broken by every body
// paragraph would otherwise paint the whole document.
const maxFormatMarks = 40

// pointOutClosing ends a format check's Output: the assistant points out and
// suggests; it must not close by offering to make the changes itself, which
// read as if it would edit the document unasked (seen on Qwen3.6 2026-10-10:
// "Bạn có muốn tôi sửa tất cả 20 lỗi chính tả? … xác nhận để tôi tiến hành").
const pointOutClosing = "CÁCH KẾT THÚC CÂU TRẢ LỜI: chỉ ra lỗi và gợi ý cách sửa, người dùng tự sửa trong văn bản. KHÔNG đề nghị tự sửa, tự chèn hay tự thay đổi văn bản, KHÔNG hỏi \"Bạn có muốn tôi sửa…\" hay \"xác nhận để tôi tiến hành\". Nếu hữu ích, chỉ nhắc: muốn có đề xuất viết lại cho một đoạn thì bôi đen đoạn đó và yêu cầu viết lại (đề xuất có nút thay vào văn bản)."

// formatMarksNote ends the Output of a tool that returned format marks.
const formatMarksNote = "Các đoạn có lỗi thể thức đo được đã được gạch chân đỏ trong trình soạn thảo để người dùng thấy vị trí; nội dung và định dạng không bị sửa (Ctrl+Z để bỏ gạch chân). Lỗi không gắn với đoạn nào (thiếu thành phần, lề trang) chỉ nêu trong câu trả lời."

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

// formatMarkOps turns spots into red-underline mark ops on the document as
// read: the quoted text when it is found in its paragraph, else the whole
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
			ops = append(ops, DocumentOp{Op: OpMark, Anchor: vdoc.anchor(s.Para), Style: "underline"})
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

// mergeMarkOps appends extra to ops, skipping a mark already planned for
// the same place (a stray character is found by both passes).
func mergeMarkOps(ops, extra []DocumentOp) []DocumentOp {
	key := func(op DocumentOp) string {
		k := op.Text + "\x00" + op.Style
		if op.Anchor != nil {
			k += "\x00" + op.Anchor.Text + "\x00" + strconv.Itoa(op.Anchor.Occurrence)
		}
		return k + "\x00" + strconv.Itoa(max(op.TextOccurrence, 1))
	}
	seen := make(map[string]bool, len(ops))
	for _, op := range ops {
		seen[key(op)] = true
	}
	for _, op := range extra {
		if k := key(op); !seen[k] {
			seen[k] = true
			ops = append(ops, op)
		}
	}
	return ops
}

// spellingPass is the format check's companion spelling review.
type spellingPass struct {
	findings []SpellingFinding
	checked  int  // paragraphs reviewed
	ok       bool // false: the model answered no batch
}

// spellingPassOf reviews the spelling of a .docx's paragraphs.
func (t *CheckSpellingTool) spellingPassOf(ctx context.Context, content []byte) *spellingPass {
	doc, err := docxedit.Open(content)
	if err != nil {
		return &spellingPass{}
	}
	findings, checked, ok := t.documentSpelling(ctx, doc.Paragraphs())
	return &spellingPass{findings: findings, checked: checked, ok: ok}
}

// render is the spelling section of the format check's Output. marked says
// whether the editor got the underlines.
func (sp *spellingPass) render(marked bool) string {
	var b strings.Builder
	switch {
	case !sp.ok:
		b.WriteString("## Lỗi chính tả\nLần này không kiểm tra được chính tả (mô hình kiểm tra chính tả không trả lời). Không tự liệt kê lỗi chính tả; đề nghị người dùng thử lại.\n")
		return b.String()
	case len(sp.findings) == 0:
		fmt.Fprintf(&b, "## Lỗi chính tả\nĐã rà %d đoạn: không phát hiện lỗi chính tả.\n", sp.checked)
		return b.String()
	}
	if marked {
		fmt.Fprintf(&b, "## Lỗi chính tả (%d lỗi, đã gạch chân đỏ trong trình soạn thảo, chưa sửa)\n", len(sp.findings))
	} else {
		fmt.Fprintf(&b, "## Lỗi chính tả (%d lỗi, CHƯA gạch chân được: hãy nêu tên văn bản cần đánh dấu)\n", len(sp.findings))
	}
	for i, f := range sp.findings {
		fmt.Fprintf(&b, "%d. Đoạn [%d]: “%s” → “%s”", i+1, f.Paragraph, f.Wrong, f.Correct)
		if f.Reason != "" {
			fmt.Fprintf(&b, " (%s)", f.Reason)
		}
		if marked && f.Unmarked {
			b.WriteString(" (không gạch chân được)")
		}
		b.WriteString("\n")
	}
	b.WriteString("Khi trả lời, nêu lỗi chính tả theo đúng danh sách này, kèm từ đúng: đó là gợi ý để người dùng tự sửa. Nếu bạn thấy thêm lỗi chính tả khác, gạch chân bằng mark_passages trước khi nêu.\n")
	return b.String()
}
