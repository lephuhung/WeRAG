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

var rewriteParagraphsTool = BaseTool{
	name: ToolRewriteParagraphs,
	description: `Rewrite text in the Word document open in this conversation's editor. Every edit is written as a Word tracked change (deletion + insertion signed "Trợ lý AI (WeRAG)") that the user accepts or rejects in the editor; the original text is never lost.

## When to Use

- The user asks to rewrite, shorten, correct or reword a passage (often the passage in <document_selection>).
- Fixing spelling or wording found while reviewing the document.

## Input

- edits: up to 30 edits, each with
  - paragraph: the paragraph index from read_document_outline (preferred), or
  - match: a distinctive piece of the paragraph's current text when the index is unknown (the selected text works). An ambiguous match fails that edit and lists the candidates — retry with the paragraph index.
  - old: the exact substring to replace inside that paragraph; omit to replace the whole paragraph text.
  - new: the replacement text (required; it may be empty only together with old, to delete that substring).
- note: optional short reason for the change, shown back in the result.

Keep the administrative register and the original meaning; change only what was asked. Formatting (font, size, alignment) is handled by apply_format_fixes, not here. Paragraphs cannot be added or removed.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "edits": {
      "type": "array",
      "minItems": 1,
      "maxItems": 30,
      "description": "Edits to apply, in order",
      "items": {
        "type": "object",
        "properties": {
          "paragraph": {
            "type": "integer",
            "minimum": 0,
            "description": "Paragraph index from read_document_outline"
          },
          "match": {
            "type": "string",
            "description": "Text that identifies the paragraph when its index is unknown"
          },
          "old": {
            "type": "string",
            "description": "Exact substring of the paragraph to replace; omit to replace the whole paragraph"
          },
          "new": {
            "type": "string",
            "description": "Replacement text"
          }
        },
        "required": ["new"]
      }
    },
    "note": {
      "type": "string",
      "description": "Optional short reason for the edits"
    }
  },
  "required": ["edits"]
}`),
}

const maxRewriteEdits = 30

type rewriteEdit struct {
	Paragraph *int    `json:"paragraph"`
	Match     string  `json:"match"`
	Old       *string `json:"old"`
	New       *string `json:"new"`
}

type rewriteParagraphsInput struct {
	Edits []rewriteEdit `json:"edits"`
	Note  string        `json:"note"`
}

// RewriteParagraphsTool replaces paragraph text in the session's workspace
// document as tracked changes.
type RewriteParagraphsTool struct {
	BaseTool
	workspace DocumentWorkspaceSource
	sessionID string
}

// NewRewriteParagraphsTool builds the tool for one session.
func NewRewriteParagraphsTool(workspace DocumentWorkspaceSource, sessionID string) *RewriteParagraphsTool {
	return &RewriteParagraphsTool{BaseTool: rewriteParagraphsTool, workspace: workspace, sessionID: sessionID}
}

// rewriteChange is one edit's outcome, reported in Data.changes.
type rewriteChange struct {
	Paragraph int    `json:"paragraph"`
	Old       string `json:"old"`
	New       string `json:"new"`
	Status    string `json:"status"` // applied | unchanged | failed
	Error     string `json:"error,omitempty"`
}

func (t *RewriteParagraphsTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var in rewriteParagraphsInput
	if err := json.Unmarshal(args, &in); err != nil {
		return &types.ToolResult{Success: false, Error: "invalid arguments: " + err.Error()}, nil
	}
	if len(in.Edits) == 0 {
		return &types.ToolResult{Success: false, Error: "edits is empty"}, nil
	}
	if len(in.Edits) > maxRewriteEdits {
		return &types.ToolResult{Success: false, Error: fmt.Sprintf(
			"too many edits (%d): at most %d per call — split the work into several calls", len(in.Edits), maxRewriteEdits)}, nil
	}
	for i, e := range in.Edits {
		if e.New == nil {
			return &types.ToolResult{Success: false, Error: fmt.Sprintf("edits[%d]: new is required", i)}, nil
		}
		if e.Old == nil && strings.TrimSpace(*e.New) == "" {
			return &types.ToolResult{Success: false, Error: fmt.Sprintf(
				"edits[%d]: new is empty — deleting a whole paragraph is not supported; give old to delete part of it", i)}, nil
		}
		if e.Old != nil && *e.Old == "" {
			return &types.ToolResult{Success: false, Error: fmt.Sprintf("edits[%d]: old is empty; omit it to replace the whole paragraph", i)}, nil
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

	changes := make([]rewriteChange, 0, len(in.Edits))
	applied, failed := 0, 0
	for _, e := range in.Edits {
		ch := t.applyEdit(doc, e)
		switch ch.Status {
		case "applied":
			applied++
		case "failed":
			failed++
		}
		changes = append(changes, ch)
	}

	revision := ws.Revision
	if applied > 0 {
		data, err := doc.Bytes()
		if err != nil {
			return &types.ToolResult{Success: false, Error: "không ghi được tài liệu: " + err.Error()}, nil
		}
		next, err := t.workspace.CommitExternalWrite(ctx, tenantID, t.sessionID, ws.Revision, data)
		if err != nil {
			if isWorkspaceConflict(err) {
				return &types.ToolResult{Success: false, Error: conflictRetryMessage}, nil
			}
			logger.Warnf(ctx, "rewrite_paragraphs: commit failed: %v", err)
			return &types.ToolResult{Success: false, Error: "không lưu được tài liệu: " + err.Error()}, nil
		}
		revision = next.Revision
	}

	var out strings.Builder
	switch {
	case applied > 0:
		fmt.Fprintf(&out, "Đã sửa %d đoạn trong %s (phiên bản %d) dưới dạng track changes; người dùng có thể chấp nhận/từ chối từng thay đổi trong trình soạn thảo.\n",
			applied, ws.FileName, revision)
	default:
		fmt.Fprintf(&out, "Chưa sửa được đoạn nào trong %s; tài liệu giữ nguyên.\n", ws.FileName)
	}
	if note := strings.TrimSpace(in.Note); note != "" {
		fmt.Fprintf(&out, "Lý do: %s\n", note)
	}
	out.WriteString("\n")
	for _, ch := range changes {
		switch ch.Status {
		case "applied":
			fmt.Fprintf(&out, "- Đoạn [%d]: đã thay “%s” → “%s” (dạng track changes, người dùng có thể chấp nhận/từ chối trong trình soạn thảo)\n",
				ch.Paragraph, clipRunes(ch.Old, 120), clipRunes(ch.New, 120))
		case "unchanged":
			fmt.Fprintf(&out, "- Đoạn [%d]: nội dung mới trùng nội dung cũ, không thay đổi\n", ch.Paragraph)
		default:
			if ch.Paragraph >= 0 {
				fmt.Fprintf(&out, "- Đoạn [%d]: KHÔNG sửa — %s\n", ch.Paragraph, ch.Error)
			} else {
				fmt.Fprintf(&out, "- KHÔNG sửa — %s\n", ch.Error)
			}
		}
	}
	data := map[string]interface{}{
		"file_name":         ws.FileName,
		"document_revision": revision,
		"applied":           applied,
		"failed":            failed,
		"changes":           changes,
	}
	if applied == 0 && failed > 0 {
		return &types.ToolResult{Success: false, Error: out.String(), Data: data}, nil
	}
	return &types.ToolResult{Success: true, Output: out.String(), Data: data}, nil
}

// applyEdit resolves one edit's paragraph and writes it as a tracked change.
func (t *RewriteParagraphsTool) applyEdit(doc *docxedit.Document, e rewriteEdit) rewriteChange {
	ch := rewriteChange{Paragraph: -1, New: *e.New}
	paras := doc.Paragraphs()
	idx, msg := resolveParagraph(doc, paras, e)
	if msg != "" {
		ch.Status, ch.Error = "failed", msg
		return ch
	}
	ch.Paragraph = idx
	current := paras[idx].Text
	if e.Old != nil {
		ch.Old = *e.Old
		if *e.Old == *e.New {
			ch.Status = "unchanged"
			return ch
		}
		if err := doc.ReplaceSubstring(idx, *e.Old, *e.New, documentEditAuthor); err != nil {
			ch.Status = "failed"
			ch.Error = fmt.Sprintf("không tìm thấy “%s” trong đoạn; nội dung hiện tại: “%s”",
				clipRunes(*e.Old, 80), clipRunes(current, 160))
			return ch
		}
		ch.Status = "applied"
		return ch
	}
	ch.Old = current
	if current == *e.New {
		ch.Status = "unchanged"
		return ch
	}
	if err := doc.ReplaceText(idx, *e.New, documentEditAuthor); err != nil {
		ch.Status, ch.Error = "failed", err.Error()
		return ch
	}
	ch.Status = "applied"
	return ch
}

// resolveParagraph picks the edit's paragraph: the index when given, else a
// unique text match (match, then old). It never guesses between several.
func resolveParagraph(doc *docxedit.Document, paras []docxedit.Paragraph, e rewriteEdit) (int, string) {
	if e.Paragraph != nil {
		if *e.Paragraph < 0 || *e.Paragraph >= len(paras) {
			return -1, fmt.Sprintf("chỉ số đoạn %d nằm ngoài phạm vi 0..%d", *e.Paragraph, len(paras)-1)
		}
		return *e.Paragraph, ""
	}
	needle := strings.TrimSpace(e.Match)
	if needle == "" && e.Old != nil {
		needle = strings.TrimSpace(*e.Old)
	}
	if needle == "" {
		return -1, "cần paragraph (chỉ số đoạn) hoặc match (đoạn văn bản để định vị)"
	}
	idx, ambiguous := doc.FindParagraph(needle)
	if idx < 0 {
		return -1, fmt.Sprintf("không tìm thấy đoạn nào chứa “%s” — dùng read_document_outline để lấy chỉ số đoạn", clipRunes(needle, 80))
	}
	if ambiguous {
		var cands []string
		for _, i := range candidateParagraphs(paras, needle, idx) {
			cands = append(cands, fmt.Sprintf("[%d] “%s”", i, clipRunes(strings.TrimSpace(paras[i].Text), 80)))
		}
		return -1, fmt.Sprintf("“%s” khớp nhiều đoạn: %s — gọi lại với paragraph là chỉ số đoạn cần sửa",
			clipRunes(needle, 80), strings.Join(cands, "; "))
	}
	return idx, ""
}

// candidateParagraphs lists (up to 8) paragraphs containing needle for an
// ambiguity message; first is the match docxedit reported.
func candidateParagraphs(paras []docxedit.Paragraph, needle string, first int) []int {
	norm := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	n := norm(needle)
	out := []int{first}
	for _, p := range paras {
		if len(out) >= 8 {
			break
		}
		if p.Index != first && strings.Contains(norm(p.Text), n) {
			out = append(out, p.Index)
		}
	}
	return out
}
