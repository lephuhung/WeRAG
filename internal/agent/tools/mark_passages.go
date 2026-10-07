package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

var markPassagesTool = BaseTool{
	name: ToolMarkPassages,
	description: `Mark passages of the Word document open in this conversation's editor that need the user's attention — a spelling or wording error, a wrong or outdated legal reference, an inconsistent number — without changing the text. The mark (red wavy underline by default, or a yellow highlight, or red text) is a tracked formatting change: the user clears it by rejecting the change in the editor.

## When to Use

The user asks to review, proofread or point out problems ("soát lỗi", "đánh dấu chỗ sai") rather than to rewrite. To actually change the text use rewrite_paragraphs.

The reason is NOT written into the document: explain each mark in your answer (the result lists them).

## Input

- marks: up to 30 marks, each with
  - paragraph: the paragraph index from read_document_outline, or
  - match: a distinctive piece of the paragraph's text (an ambiguous match fails that mark);
  - text: the exact passage inside the paragraph to mark; omit to mark the whole paragraph (when paragraph and match are both omitted, text itself locates the paragraph);
  - style: underline (red wavy, default) | highlight (yellow) | color (red text);
  - reason: why the passage is marked (required).`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
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
          "style": {"type": "string", "enum": ["underline", "highlight", "color"], "description": "Default underline (red wavy)"},
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
	Marks []markSpec `json:"marks"`
}

// markResult is one mark's outcome, reported in Data.marks.
type markResult struct {
	Paragraph int    `json:"paragraph"`
	Text      string `json:"text"`
	Style     string `json:"style"`
	Reason    string `json:"reason"`
	Status    string `json:"status"` // applied | unchanged (already marked) | failed
	Error     string `json:"error,omitempty"`
}

// MarkPassagesTool marks passages of the session's workspace document as
// tracked formatting changes.
type MarkPassagesTool struct {
	BaseTool
	workspace DocumentWorkspaceSource
	sessionID string
}

// NewMarkPassagesTool builds the tool for one session.
func NewMarkPassagesTool(workspace DocumentWorkspaceSource, sessionID string) *MarkPassagesTool {
	return &MarkPassagesTool{BaseTool: markPassagesTool, workspace: workspace, sessionID: sessionID}
}

// markStyle maps a style name to the docxedit mark.
func markStyle(style string) (docxedit.Mark, bool) {
	str := func(s string) *string { return &s }
	switch style {
	case "", "underline":
		return docxedit.Mark{Underline: str("wave"), UnderlineColor: str("FF0000")}, true
	case "highlight":
		return docxedit.Mark{Highlight: str("yellow")}, true
	case "color":
		return docxedit.Mark{Color: str("FF0000")}, true
	}
	return docxedit.Mark{}, false
}

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
		if _, ok := markStyle(m.Style); !ok {
			return &types.ToolResult{Success: false, Error: fmt.Sprintf(
				"marks[%d]: style must be underline, highlight or color", i)}, nil
		}
	}

	tenantID, content, ws, err := prepareWorkspaceWrite(ctx, t.workspace, t.sessionID)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	doc, err := docxedit.Open(content)
	if err != nil {
		return &types.ToolResult{Success: false, Error: "không mở được tài liệu để sửa: " + err.Error()}, nil
	}

	// Marks never add or remove paragraphs nor change text, so the
	// paragraph list read once stays valid. Each mark is its own batch:
	// several marks may hit one paragraph, and Pending tells a mark the
	// document already carries (docxedit records nothing for it).
	paras := doc.Paragraphs()
	results := make([]markResult, len(in.Marks))
	applied, unchanged, failed := 0, 0, 0
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
		noop := false
		if msg == "" {
			r.Paragraph = idx
			mark, _ := markStyle(style)
			if m.Text == "" {
				r.Text = strings.TrimSpace(paras[idx].Text)
			}
			err := doc.Batch(func(b *docxedit.Batch) error {
				var err error
				if m.Text != "" {
					err = b.MarkSubstring(idx, m.Text, mark, documentEditAuthor)
				} else {
					err = b.MarkParagraph(idx, mark, documentEditAuthor)
				}
				noop = err == nil && b.Pending() == 0
				return err
			})
			switch {
			case err != nil && m.Text != "":
				msg = fmt.Sprintf("không tìm thấy “%s” trong đoạn [%d]: %v", clipRunes(m.Text, 80), idx, err)
			case err != nil:
				msg = err.Error()
			}
		}
		switch {
		case msg != "":
			r.Status, r.Error = "failed", msg
			failed++
		case noop:
			r.Status = "unchanged"
			unchanged++
		default:
			r.Status = "applied"
			applied++
		}
		results[i] = r
	}

	revision := ws.Revision
	if doc.Dirty() {
		data, err := doc.Bytes()
		if err != nil {
			return &types.ToolResult{Success: false, Error: "không ghi được tài liệu: " + err.Error()}, nil
		}
		next, err := t.workspace.CommitExternalWrite(ctx, tenantID, t.sessionID, ws.Revision, data)
		if err != nil {
			if isWorkspaceConflict(err) {
				return &types.ToolResult{Success: false, Error: conflictRetryMessage}, nil
			}
			logger.Warnf(ctx, "mark_passages: commit failed: %v", err)
			return &types.ToolResult{Success: false, Error: "không lưu được tài liệu: " + err.Error()}, nil
		}
		revision = next.Revision
	}

	styleVI := map[string]string{"underline": "gạch chân lượn sóng đỏ", "highlight": "tô vàng", "color": "chữ đỏ"}
	var out strings.Builder
	switch {
	case applied > 0:
		fmt.Fprintf(&out, "Đã đánh dấu %d chỗ trong %s (phiên bản %d):\n", applied, ws.FileName, revision)
	case unchanged > 0:
		fmt.Fprintf(&out, "Các chỗ này đã được đánh dấu trước đó trong %s; tài liệu giữ nguyên (phiên bản %d):\n", ws.FileName, revision)
	default:
		fmt.Fprintf(&out, "Chưa đánh dấu được chỗ nào trong %s; tài liệu giữ nguyên.\n", ws.FileName)
	}
	n := 0
	for _, r := range results {
		if r.Status == "failed" {
			continue
		}
		n++
		fmt.Fprintf(&out, "%d. Đoạn [%d] “%s” (%s): %s", n, r.Paragraph, clipRunes(r.Text, 120), styleVI[r.Style], r.Reason)
		if r.Status == "unchanged" {
			out.WriteString(" — đã được đánh dấu trước đó")
		}
		out.WriteString("\n")
	}
	for _, r := range results {
		if r.Status == "failed" {
			fmt.Fprintf(&out, "- KHÔNG đánh dấu (%s): %s\n", clipRunes(r.Reason, 80), r.Error)
		}
	}
	if applied+unchanged > 0 {
		out.WriteString("\nCác dấu này là thay đổi định dạng dạng track changes, không sửa nội dung; lý do chỉ nằm trong câu trả lời này. " +
			"Người dùng từ chối (reject) thay đổi trong trình soạn thảo để xóa dấu.\n")
	}
	data := map[string]interface{}{
		"file_name":         ws.FileName,
		"document_revision": revision,
		"applied":           applied,
		"unchanged":         unchanged,
		"failed":            failed,
		"marks":             results,
	}
	if applied+unchanged == 0 {
		return &types.ToolResult{Success: false, Error: out.String(), Data: data}, nil
	}
	return &types.ToolResult{Success: true, Output: out.String(), Data: data}, nil
}
