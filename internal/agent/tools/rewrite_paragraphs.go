package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/types"
)

var rewriteParagraphsTool = BaseTool{
	name: ToolRewriteParagraphs,
	description: `Rewrite the passage the user highlighted in the Word document open in this conversation's editor. The tool plans the replacement; the editor applies it as an ordinary edit, which the user undoes with Ctrl+Z.

## When to Use

ONLY when the user explicitly asks in this turn to rewrite, shorten, correct or reword text ("viết lại", "sửa câu này", "rút gọn") AND has highlighted the passage (the <document_selection> block). Without a selection the tool refuses; an edit outside the selected passage is refused too. To point out problems without changing the text, use mark_passages.

## Input

- edits: up to 30 edits, each with
  - paragraph: the paragraph index from read_document_outline, or
  - match: a distinctive piece of the paragraph's current text (the selected text works). An ambiguous match fails that edit and lists the candidates — retry with the paragraph index.
  - old: the exact substring to replace inside that paragraph (preferred); omit to replace the whole paragraph text.
  - new: the replacement text (required; it may be empty only together with old, to delete that substring).
- note: optional short reason for the change, shown back in the result.

Keep the administrative register and the original meaning; change only what was asked. It only changes text inside existing paragraphs: it cannot add paragraphs (use insert_paragraphs) or delete them, and formatting (font, size, alignment) is handled by apply_format_fixes.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + documentParamSchema + `,
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
	Edits    []rewriteEdit `json:"edits"`
	Note     string        `json:"note"`
	Document string        `json:"document"`
}

// RewriteParagraphsTool plans text replacements in the user's selected
// passage of the session's workspace document; the editor applies them.
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
	Status    string `json:"status"` // planned | unchanged | failed
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

	sel := types.DocumentSelectionFromContext(ctx)
	if sel == nil {
		return &types.ToolResult{Success: false, Error: errRewriteNeedsSelection}, nil
	}

	target, err := resolveDocument(ctx, t.workspace, t.sessionID, in.Document, true)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	if !selectionIn(sel, target) {
		return &types.ToolResult{Success: false, Error: errSelectionElsewhere(sel, target)}, nil
	}
	content, ws, seq, err := snapshotDocument(ctx, t.workspace, t.sessionID, target.ID, "viết lại đoạn văn")
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	doc, err := docxedit.Open(content)
	if err != nil {
		return &types.ToolResult{Success: false, Error: "không đọc được tài liệu: " + err.Error()}, nil
	}
	paras := doc.Paragraphs()
	vdoc := newVirtualDoc(paras)

	var ops []DocumentOp
	changes := make([]rewriteChange, 0, len(in.Edits))
	planned, failed := 0, 0
	for _, e := range in.Edits {
		ch, op := planRewrite(doc, paras, vdoc, sel, e)
		switch ch.Status {
		case "planned":
			planned++
			ops = append(ops, op)
		case "failed":
			failed++
		}
		changes = append(changes, ch)
	}

	var out strings.Builder
	switch {
	case planned > 0:
		fmt.Fprintf(&out, "Sẽ sửa %d chỗ trong %s:\n", planned, ws.FileName)
	default:
		fmt.Fprintf(&out, "Không có thay đổi nào cho %s; tài liệu giữ nguyên.\n", ws.FileName)
	}
	if note := strings.TrimSpace(in.Note); note != "" {
		fmt.Fprintf(&out, "Lý do: %s\n", note)
	}
	for _, ch := range changes {
		switch ch.Status {
		case "planned":
			fmt.Fprintf(&out, "- Đoạn [%d]: “%s” → “%s”\n", ch.Paragraph, clipRunes(ch.Old, 120), clipRunes(ch.New, 120))
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
	if planned > 0 {
		out.WriteString("\n" + editorAppliedNote + "\n")
	}
	data := opsData(ops, seq, ws)
	data["file_name"] = ws.FileName
	data["planned"] = planned
	data["failed"] = failed
	data["changes"] = changes
	if planned == 0 && failed > 0 {
		return &types.ToolResult{Success: false, Error: out.String(), Data: data}, nil
	}
	return &types.ToolResult{Success: true, Output: out.String(), Data: data}, nil
}

const errRewriteNeedsSelection = "Không viết lại: lượt này người dùng chưa bôi đen đoạn nào trong trình soạn thảo. " +
	"Chỉ viết lại đoạn người dùng đã chọn — hãy nhắc họ bôi đen đoạn cần sửa rồi gửi lại yêu cầu; " +
	"nếu chỉ cần chỉ ra lỗi, dùng mark_passages."

// planRewrite resolves one edit against the document and the selection and
// returns its op; vdoc follows the text the plugin will see.
func planRewrite(doc *docxedit.Document, paras []docxedit.Paragraph, vdoc *virtualDoc, sel *types.DocumentSelection, e rewriteEdit) (rewriteChange, DocumentOp) {
	ch := rewriteChange{Paragraph: -1, New: *e.New}
	idx, msg := resolveParagraph(doc, paras, e)
	if msg != "" {
		ch.Status, ch.Error = "failed", msg
		return ch, DocumentOp{}
	}
	ch.Paragraph = idx
	pos := vdoc.pos(idx)
	current := vdoc.texts[pos]
	target := current
	if e.Old != nil {
		target = *e.Old
	}
	if !selectionOverlaps(sel.Text, target) && !(e.Old == nil && sel.ParagraphHint != "" && selectionOverlaps(sel.ParagraphHint, current)) {
		ch.Status = "failed"
		ch.Error = fmt.Sprintf("đoạn này không nằm trong phần người dùng đã bôi đen (“%s”); chỉ được viết lại phần đã chọn",
			clipRunes(sel.Text, 80))
		return ch, DocumentOp{}
	}
	anchor := vdoc.anchor(pos)
	if e.Old != nil {
		ch.Old = *e.Old
		if *e.Old == *e.New {
			ch.Status = "unchanged"
			return ch, DocumentOp{}
		}
		next, ok := replaceFirst(current, *e.Old, *e.New)
		if !ok {
			ch.Status = "failed"
			ch.Error = fmt.Sprintf("không tìm thấy “%s” trong đoạn; nội dung hiện tại: “%s”",
				clipRunes(*e.Old, 80), clipRunes(current, 160))
			return ch, DocumentOp{}
		}
		vdoc.setText(pos, next)
		ch.Status = "planned"
		old, nw := *e.Old, *e.New
		return ch, DocumentOp{Op: OpReplaceText, Anchor: anchor, Old: &old, New: &nw}
	}
	ch.Old = current
	if anchorText(current) == anchorText(*e.New) {
		ch.Status = "unchanged"
		return ch, DocumentOp{}
	}
	vdoc.setText(pos, *e.New)
	ch.Status = "planned"
	nw := *e.New
	return ch, DocumentOp{Op: OpReplaceParagraph, Anchor: anchor, New: &nw}
}

// replaceFirst replaces the first occurrence of old in text, also when the
// two differ only in whitespace runs.
func replaceFirst(text, old, new string) (string, bool) {
	if strings.Contains(text, old) {
		return strings.Replace(text, old, new, 1), true
	}
	if n := anchorText(old); n != "" && strings.Contains(anchorText(text), n) {
		return strings.Replace(anchorText(text), n, new, 1), true
	}
	return "", false
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
