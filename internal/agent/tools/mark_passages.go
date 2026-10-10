package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/types"
)

var markPassagesTool = BaseTool{
	name: ToolMarkPassages,
	description: `Mark passages of the Word document open in this conversation's editor that need the user's attention — a spelling or wording error, a wrong or outdated legal reference, an inconsistent number — without changing the text. The mark (red underline by default, or a yellow highlight, or red text) is applied by the editor as an ordinary formatting edit; the user removes it with Ctrl+Z.

This is the default way to act on a review: point out problems with marks and explain them in the answer, instead of changing the document.

## When to Use

The user asks to review, proofread or point out problems ("soát lỗi", "đánh dấu chỗ sai") rather than to rewrite. To actually change the text use rewrite_paragraphs.

The reason is NOT written into the document: explain each mark in your answer (the result lists them).

## Input

- marks: up to 30 marks, each with
  - paragraph: the paragraph index from read_document_outline, or
  - match: a distinctive piece of the paragraph's text (an ambiguous match fails that mark);
  - text: the exact passage inside the paragraph to mark; omit to mark the whole paragraph (when paragraph and match are both omitted, text itself locates the paragraph);
  - style: underline (red, default) | highlight (yellow) | color (red text);
  - reason: why the passage is marked (required).`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + documentParamSchema + `,
    "marks": {
      "type": "array",
      "minItems": 1,
      "maxItems": 30,
      "items": {
        "type": "object",
        "properties": {
          "paragraph": {"type": "integer", "minimum": 0, "description": "Paragraph index from read_document_outline"},
          "match": {"type": "string", "description": "Text identifying the paragraph when its index is unknown"},
          "text": {"type": "string", "description": "Exact passage to mark; omit to mark the whole paragraph"},
          "style": {"type": "string", "enum": ["underline", "highlight", "color"], "description": "Default underline (red)"},
          "reason": {"type": "string", "description": "Why the passage is marked"}
        },
        "required": ["reason"]
      }
    }
  },
  "required": ["marks"]
}`),
}

const maxMarks = 30

type markSpec struct {
	Paragraph *int   `json:"paragraph"`
	Match     string `json:"match"`
	Text      string `json:"text"`
	Style     string `json:"style"`
	Reason    string `json:"reason"`
}

type markPassagesInput struct {
	Marks    []markSpec `json:"marks"`
	Document string     `json:"document"`
}

// markResult is one mark's outcome, reported in Data.marks.
type markResult struct {
	Paragraph int    `json:"paragraph"`
	Text      string `json:"text"`
	Style     string `json:"style"`
	Reason    string `json:"reason"`
	Status    string `json:"status"` // planned | failed
	Error     string `json:"error,omitempty"`
}

// MarkPassagesTool plans marks on passages of the session's workspace
// document; the editor draws them.
type MarkPassagesTool struct {
	BaseTool
	workspace DocumentWorkspaceSource
	sessionID string
}

// NewMarkPassagesTool builds the tool for one session.
func NewMarkPassagesTool(workspace DocumentWorkspaceSource, sessionID string) *MarkPassagesTool {
	return &MarkPassagesTool{BaseTool: markPassagesTool, workspace: workspace, sessionID: sessionID}
}

// markStyles are the mark styles the editor plugin draws: underline (red
// underline), highlight (yellow) and color (red text).
var markStyles = map[string]string{"underline": "gạch chân đỏ", "highlight": "tô vàng", "color": "chữ đỏ"}

func (t *MarkPassagesTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var in markPassagesInput
	if err := json.Unmarshal(args, &in); err != nil {
		return &types.ToolResult{Success: false, Error: "invalid arguments: " + err.Error()}, nil
	}
	if len(in.Marks) == 0 {
		return &types.ToolResult{Success: false, Error: "marks is empty"}, nil
	}
	if len(in.Marks) > maxMarks {
		return &types.ToolResult{Success: false, Error: fmt.Sprintf(
			"too many marks (%d): at most %d per call", len(in.Marks), maxMarks)}, nil
	}
	for i, m := range in.Marks {
		if strings.TrimSpace(m.Reason) == "" {
			return &types.ToolResult{Success: false, Error: fmt.Sprintf("marks[%d]: reason is required", i)}, nil
		}
		if _, ok := markStyles[m.Style]; !ok && m.Style != "" {
			return &types.ToolResult{Success: false, Error: fmt.Sprintf(
				"marks[%d]: style must be underline, highlight or color", i)}, nil
		}
	}

	target, err := resolveDocument(ctx, t.workspace, t.sessionID, in.Document, true)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	content, ws, seq, err := snapshotDocument(ctx, t.workspace, t.sessionID, target.ID, "đánh dấu chỗ cần xem lại")
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	doc, err := docxedit.Open(content)
	if err != nil {
		return &types.ToolResult{Success: false, Error: "không đọc được tài liệu: " + err.Error()}, nil
	}

	// Marks change neither text nor paragraphs: every anchor is computed on
	// the document as read.
	paras := doc.Paragraphs()
	vdoc := newVirtualDoc(paras)
	var ops []DocumentOp
	results := make([]markResult, len(in.Marks))
	planned, failed := 0, 0
	for i, m := range in.Marks {
		style := m.Style
		if style == "" {
			style = "underline"
		}
		r := markResult{Paragraph: -1, Text: m.Text, Style: style, Reason: strings.TrimSpace(m.Reason)}
		e := rewriteEdit{Paragraph: m.Paragraph, Match: m.Match}
		if m.Text != "" {
			text := m.Text
			e.Old = &text
		}
		idx, msg := resolveParagraph(doc, paras, e)
		if msg == "" {
			r.Paragraph = idx
			switch {
			case m.Text == "":
				r.Text = strings.TrimSpace(paras[idx].Text)
				if r.Text == "" {
					msg = fmt.Sprintf("đoạn [%d] trống", idx)
				}
			case !paragraphHas(paras[idx], m.Text):
				// a paragraph index recalled from an evaluation or an earlier
				// turn may be a few paragraphs off: the text decides, nearest
				// paragraph holding it first
				if near := nearestParagraphWith(paras, idx, m.Text); near >= 0 {
					idx, r.Paragraph = near, near
				} else {
					msg = fmt.Sprintf("không tìm thấy “%s” trong đoạn [%d]: “%s”", clipRunes(m.Text, 80), idx, clipRunes(paras[idx].Text, 120))
				}
			}
		}
		if msg != "" {
			r.Status, r.Error = "failed", msg
			failed++
			results[i] = r
			continue
		}
		ops = append(ops, DocumentOp{Op: OpMark, Anchor: vdoc.anchor(idx), Text: m.Text, Style: style})
		r.Status = "planned"
		planned++
		results[i] = r
	}

	var out strings.Builder
	if planned > 0 {
		fmt.Fprintf(&out, "Sẽ đánh dấu %d chỗ trong %s:\n", planned, ws.FileName)
	} else {
		fmt.Fprintf(&out, "Không đánh dấu được chỗ nào trong %s; tài liệu giữ nguyên.\n", ws.FileName)
	}
	n := 0
	for _, r := range results {
		if r.Status != "planned" {
			continue
		}
		n++
		fmt.Fprintf(&out, "%d. Đoạn [%d] “%s” (%s): %s\n", n, r.Paragraph, clipRunes(r.Text, 120), markStyles[r.Style], r.Reason)
	}
	for _, r := range results {
		if r.Status == "failed" {
			fmt.Fprintf(&out, "- KHÔNG đánh dấu (%s): %s\n", clipRunes(r.Reason, 80), r.Error)
		}
	}
	if planned > 0 {
		out.WriteString("\nDấu chỉ là định dạng, không sửa nội dung; lý do chỉ nằm trong câu trả lời này. " + editorAppliedNote + "\n")
	}
	data := opsData(ops, seq, ws)
	data["file_name"] = ws.FileName
	data["planned"] = planned
	data["failed"] = failed
	data["marks"] = results
	if planned == 0 {
		return &types.ToolResult{Success: false, Error: out.String(), Data: data}, nil
	}
	return &types.ToolResult{Success: true, Output: out.String(), Data: data}, nil
}

// paragraphHas reports whether text is in p, as written or with whitespace
// and composition normalised.
func paragraphHas(p docxedit.Paragraph, text string) bool {
	return strings.Contains(p.Text, text) || strings.Contains(anchorText(p.Text), anchorText(text))
}

// nearestParagraphWith is the index of the paragraph holding text that is
// closest to idx (the earlier one on a tie), or -1 when none does.
func nearestParagraphWith(paras []docxedit.Paragraph, idx int, text string) int {
	if strings.TrimSpace(text) == "" {
		return -1
	}
	for d := 1; d < len(paras); d++ {
		for _, i := range []int{idx - d, idx + d} {
			if i >= 0 && i < len(paras) && paragraphHas(paras[i], text) {
				return i
			}
		}
	}
	return -1
}
