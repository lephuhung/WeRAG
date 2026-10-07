package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

var insertParagraphsTool = BaseTool{
	name: ToolInsertParagraphs,
	description: `Insert new paragraphs into the Word document open in this conversation's editor, as Word tracked insertions (signed "Trợ lý AI (WeRAG)") the user accepts or rejects in the editor.

## When to Use

- A component is missing and must be added: the Nơi nhận block, a Căn cứ line, an Điều khoản thi hành article, a closing sentence, a signature line.
- The user asks to add a sentence or paragraph at a given place.

It only adds paragraphs. To change the text of an existing paragraph use rewrite_paragraphs; paragraphs cannot be deleted.

## Input

- inserts: up to 20 inserts, each with
  - after: index (from read_document_outline) of the paragraph the new text goes after, or
  - after_match: a distinctive piece of that paragraph's text (an ambiguous match fails that insert), or
  - at_start: true to insert before the first paragraph;
  - text: the text to insert; each line ("\n") becomes its own paragraph, in order (blank lines are skipped);
  - like: index of a paragraph whose formatting the new paragraphs copy;
  - alignment (left | center | right | both), bold, italic: optional formatting on top of that.
  at_start wins over after / after_match.

Formatting: a new paragraph copies the paragraph and run formatting of its ANCHOR (the paragraph it is inserted after) unless like, alignment, bold or italic say otherwise. When the anchor is a centred, bold or otherwise different line (a signature, a title), pass like pointing at a paragraph of the same component as the new text — e.g. an existing "- ...;" line of Nơi nhận, or a body paragraph for a new sentence of the body.
- note: optional short reason, shown back in the result.

Paragraph indices refer to the document as it is before this call; inserts in one call do not shift each other's anchors.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "inserts": {
      "type": "array",
      "minItems": 1,
      "maxItems": 20,
      "items": {
        "type": "object",
        "properties": {
          "after": {"type": "integer", "minimum": 0, "description": "Index of the paragraph to insert after (ignored when at_start is true)"},
          "after_match": {"type": "string", "description": "Text identifying the paragraph to insert after"},
          "at_start": {"type": "boolean", "description": "Insert before the first paragraph; wins over after and after_match"},
          "text": {"type": "string", "description": "Text to insert; each line becomes a paragraph, blank lines are skipped"},
          "like": {"type": "integer", "minimum": 0, "description": "Index of a paragraph whose formatting to copy; default is the anchor paragraph's formatting"},
          "alignment": {"type": "string", "enum": ["left", "center", "right", "both"]},
          "bold": {"type": "boolean"},
          "italic": {"type": "boolean"}
        },
        "required": ["text"]
      }
    },
    "note": {"type": "string", "description": "Optional short reason for the inserts"}
  },
  "required": ["inserts"]
}`),
}

const maxInserts = 20

type insertSpec struct {
	After      *int    `json:"after"`
	AfterMatch string  `json:"after_match"`
	AtStart    bool    `json:"at_start"`
	Text       string  `json:"text"`
	Like       *int    `json:"like"`
	Alignment  *string `json:"alignment"`
	Bold       *bool   `json:"bold"`
	Italic     *bool   `json:"italic"`
}

type insertParagraphsInput struct {
	Inserts []insertSpec `json:"inserts"`
	Note    string       `json:"note"`
}

// insertChange is one insert's outcome, reported in Data.changes.
type insertChange struct {
	After  int    `json:"after"` // -1 = at the start
	Text   string `json:"text"`
	Status string `json:"status"` // applied | failed
	Error  string `json:"error,omitempty"`
}

// InsertParagraphsTool adds paragraphs to the session's workspace document
// as tracked insertions.
type InsertParagraphsTool struct {
	BaseTool
	workspace DocumentWorkspaceSource
	sessionID string
}

// NewInsertParagraphsTool builds the tool for one session.
func NewInsertParagraphsTool(workspace DocumentWorkspaceSource, sessionID string) *InsertParagraphsTool {
	return &InsertParagraphsTool{BaseTool: insertParagraphsTool, workspace: workspace, sessionID: sessionID}
}

// insertLines splits an insert's text into its paragraphs.
func insertLines(text string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if l = strings.TrimRight(l, " \t"); strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func (t *InsertParagraphsTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var in insertParagraphsInput
	if err := json.Unmarshal(args, &in); err != nil {
		return &types.ToolResult{Success: false, Error: "invalid arguments: " + err.Error()}, nil
	}
	if len(in.Inserts) == 0 {
		return &types.ToolResult{Success: false, Error: "inserts is empty"}, nil
	}
	if len(in.Inserts) > maxInserts {
		return &types.ToolResult{Success: false, Error: fmt.Sprintf(
			"too many inserts (%d): at most %d per call", len(in.Inserts), maxInserts)}, nil
	}
	for i, ins := range in.Inserts {
		if len(insertLines(ins.Text)) == 0 {
			return &types.ToolResult{Success: false, Error: fmt.Sprintf("inserts[%d]: text is empty", i)}, nil
		}
		if ins.Alignment != nil {
			if _, ok := docxAlignment[*ins.Alignment]; !ok {
				return &types.ToolResult{Success: false, Error: fmt.Sprintf(
					"inserts[%d]: alignment must be left, center, right or both", i)}, nil
			}
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

	// Resolve every anchor against the document as it was read, then insert
	// from the last anchor backwards so no insert shifts a later anchor.
	paras := doc.Paragraphs()
	changes := make([]insertChange, len(in.Inserts))
	anchors := make([]int, len(in.Inserts))
	var order []int
	for i, ins := range in.Inserts {
		changes[i] = insertChange{After: -1, Text: strings.Join(insertLines(ins.Text), "\n")}
		anchor, msg := resolveInsertAnchor(doc, paras, ins)
		if msg == "" && ins.Like != nil && (*ins.Like < 0 || *ins.Like >= len(paras)) {
			msg = fmt.Sprintf("like=%d nằm ngoài phạm vi 0..%d", *ins.Like, len(paras)-1)
		}
		changes[i].After = anchor
		if msg != "" {
			if !ins.AtStart && ins.After != nil {
				changes[i].After = *ins.After // report what was asked for
			}
			changes[i].Status, changes[i].Error = "failed", msg
			continue
		}
		anchors[i] = anchor
		order = append(order, i)
	}
	// descending anchor; equal anchors in reverse request order, so the
	// first requested insert ends up first in the document
	sort.SliceStable(order, func(a, b int) bool {
		if anchors[order[a]] != anchors[order[b]] {
			return anchors[order[a]] > anchors[order[b]]
		}
		return order[a] > order[b]
	})

	type done struct{ anchor, count int }
	var inserted []done
	shift := func(idx int) int { // original index → current index
		out := idx
		for _, d := range inserted {
			if d.anchor < idx {
				out += d.count
			}
		}
		return out
	}
	wrote := false
	for _, i := range order {
		ins := in.Inserts[i]
		np := docxedit.NewParagraph{}
		if ins.Like != nil {
			like := shift(*ins.Like)
			np.InheritFrom = &like
		}
		if ins.Alignment != nil {
			a := docxAlignment[*ins.Alignment]
			np.Para = &docxedit.ParaProps{Alignment: &a}
		}
		if ins.Bold != nil || ins.Italic != nil {
			np.Run = &docxedit.RunProps{Bold: ins.Bold, Italic: ins.Italic}
		}
		// anchors are processed from the last one back, so this anchor's
		// index has not moved
		at, count := anchors[i], 0
		var insErr error
		for _, line := range insertLines(ins.Text) {
			np.Text = line
			idx, err := doc.InsertParagraphAfter(at, np, documentEditAuthor)
			if err != nil {
				insErr = err
				break
			}
			at = idx
			count++
		}
		if count > 0 {
			inserted = append(inserted, done{anchors[i], count})
			wrote = true
		}
		switch {
		case insErr == nil:
			changes[i].Status = "applied"
		case count > 0:
			changes[i].Status = "failed"
			changes[i].Error = fmt.Sprintf("chỉ chèn được %d dòng đầu (đã ghi): %v", count, insErr)
		default:
			changes[i].Status, changes[i].Error = "failed", insErr.Error()
		}
	}
	applied, failed := 0, 0
	for _, c := range changes {
		if c.Status == "applied" {
			applied++
		} else {
			failed++
		}
	}

	revision := ws.Revision
	if wrote {
		data, err := doc.Bytes()
		if err != nil {
			return &types.ToolResult{Success: false, Error: "không ghi được tài liệu: " + err.Error()}, nil
		}
		next, err := t.workspace.CommitExternalWrite(ctx, tenantID, t.sessionID, ws.Revision, data)
		if err != nil {
			if isWorkspaceConflict(err) {
				return &types.ToolResult{Success: false, Error: conflictRetryMessage}, nil
			}
			logger.Warnf(ctx, "insert_paragraphs: commit failed: %v", err)
			return &types.ToolResult{Success: false, Error: "không lưu được tài liệu: " + err.Error()}, nil
		}
		revision = next.Revision
	}

	var out strings.Builder
	if wrote {
		fmt.Fprintf(&out, "Đã chèn %d mục vào %s (phiên bản %d) dưới dạng track changes; người dùng có thể chấp nhận/từ chối trong trình soạn thảo.\n",
			applied, ws.FileName, revision)
	} else {
		fmt.Fprintf(&out, "Chưa chèn được đoạn nào vào %s; tài liệu giữ nguyên.\n", ws.FileName)
	}
	if note := strings.TrimSpace(in.Note); note != "" {
		fmt.Fprintf(&out, "Lý do: %s\n", note)
	}
	out.WriteString("\n")
	for _, c := range changes {
		where := fmt.Sprintf("sau đoạn [%d]", c.After)
		if c.After < 0 {
			where = "ở đầu văn bản"
		}
		if c.Status == "applied" {
			fmt.Fprintf(&out, "- Đã chèn %s: “%s” (track changes)\n", where, clipRunes(strings.ReplaceAll(c.Text, "\n", " ↵ "), 160))
		} else {
			fmt.Fprintf(&out, "- KHÔNG chèn “%s”: %s\n", clipRunes(strings.ReplaceAll(c.Text, "\n", " ↵ "), 80), c.Error)
		}
	}
	data := map[string]interface{}{
		"file_name":         ws.FileName,
		"document_revision": revision,
		"applied":           applied,
		"failed":            failed,
		"changes":           changes,
	}
	if !wrote {
		return &types.ToolResult{Success: false, Error: out.String(), Data: data}, nil
	}
	return &types.ToolResult{Success: true, Output: out.String(), Data: data}, nil
}

// resolveInsertAnchor returns the paragraph an insert goes after (-1 for
// the start of the document).
func resolveInsertAnchor(doc *docxedit.Document, paras []docxedit.Paragraph, ins insertSpec) (int, string) {
	switch {
	case ins.AtStart:
		return -1, ""
	case ins.After != nil:
		if *ins.After < 0 || *ins.After >= len(paras) {
			return -1, fmt.Sprintf("chỉ số đoạn %d nằm ngoài phạm vi 0..%d", *ins.After, len(paras)-1)
		}
		return *ins.After, ""
	case strings.TrimSpace(ins.AfterMatch) != "":
		return resolveParagraph(doc, paras, rewriteEdit{Match: ins.AfterMatch})
	}
	return -1, "cần after (chỉ số đoạn), after_match hoặc at_start để biết chèn ở đâu"
}
