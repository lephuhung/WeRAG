package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/types"
)

var insertParagraphsTool = BaseTool{
	name: ToolInsertParagraphs,
	description: `Insert new paragraphs into the Word document open in this conversation's editor. The tool plans the insertion; the editor applies it as an ordinary edit, which the user undoes with Ctrl+Z.

## When to Use

Only when the user explicitly asks in this turn to add or insert something ("chèn", "bổ sung", "thêm"):
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

Paragraph indices refer to the document as it is before this call; inserts in one call do not shift each other's anchors, and several inserts after the same paragraph keep their order.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + documentParamSchema + `,
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
	Inserts  []insertSpec `json:"inserts"`
	Note     string       `json:"note"`
	Document string       `json:"document"`
}

// insertChange is one insert's outcome, reported in Data.changes.
type insertChange struct {
	After  int    `json:"after"` // -1 = at the start
	Text   string `json:"text"`
	Status string `json:"status"` // planned | failed
	Error  string `json:"error,omitempty"`
}

// InsertParagraphsTool plans new paragraphs for the session's workspace
// document; the editor inserts them.
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

	target, err := resolveDocument(ctx, t.workspace, t.sessionID, in.Document, true)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	content, ws, seq, err := snapshotDocument(ctx, t.workspace, t.sessionID, target.ID, "chèn đoạn văn")
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	doc, err := docxedit.Open(content)
	if err != nil {
		return &types.ToolResult{Success: false, Error: "không đọc được tài liệu: " + err.Error()}, nil
	}

	// Anchors and like are original paragraph indices; vdoc follows the
	// document as the plugin will see it while applying the ops in order.
	// Inserts after the same paragraph chain after one another, so they
	// keep the requested order.
	paras := doc.Paragraphs()
	vdoc := newVirtualDoc(paras)
	lastAfter := map[int]int{} // anchor id → id of the last line inserted after it
	var ops []DocumentOp
	changes := make([]insertChange, len(in.Inserts))
	for i, ins := range in.Inserts {
		lines := insertLines(ins.Text)
		changes[i] = insertChange{After: -1, Text: strings.Join(lines, "\n")}
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
		key := anchor // -1 = the start
		at := anchor
		if last, ok := lastAfter[key]; ok {
			at = last
		}
		for _, line := range lines {
			p := -1
			if at != -1 {
				p = vdoc.pos(at)
			}
			op := DocumentOp{Op: OpInsertAfter, Anchor: vdoc.anchor(p), Text: line,
				Bold: ins.Bold, Italic: ins.Italic}
			if ins.Like != nil {
				op.Like = vdoc.anchor(vdoc.pos(*ins.Like))
			}
			if ins.Alignment != nil {
				op.Alignment = docxAlignment[*ins.Alignment]
			}
			ops = append(ops, op)
			at = vdoc.insertAfter(p, line)
		}
		lastAfter[key] = at
		changes[i].Status = "planned"
	}
	planned, failed := 0, 0
	for _, c := range changes {
		if c.Status == "planned" {
			planned++
		} else {
			failed++
		}
	}

	var out strings.Builder
	if planned > 0 {
		fmt.Fprintf(&out, "Sẽ chèn %d mục (%d đoạn) vào %s:\n", planned, len(ops), ws.FileName)
	} else {
		fmt.Fprintf(&out, "Không chèn được đoạn nào vào %s; tài liệu giữ nguyên.\n", ws.FileName)
	}
	if note := strings.TrimSpace(in.Note); note != "" {
		fmt.Fprintf(&out, "Lý do: %s\n", note)
	}
	for _, c := range changes {
		where := fmt.Sprintf("sau đoạn [%d]", c.After)
		if c.After < 0 {
			where = "ở đầu văn bản"
		}
		if c.Status == "planned" {
			fmt.Fprintf(&out, "- Chèn %s: “%s”\n", where, clipRunes(strings.ReplaceAll(c.Text, "\n", " ↵ "), 160))
		} else {
			fmt.Fprintf(&out, "- KHÔNG chèn “%s”: %s\n", clipRunes(strings.ReplaceAll(c.Text, "\n", " ↵ "), 80), c.Error)
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
	if planned == 0 {
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
