package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

var rewriteParagraphsTool = BaseTool{
	name: ToolRewriteParagraphs,
	description: `Rewrite the passage the user highlighted in the Word document open in this conversation's editor.

## When to Use

ONLY when the user asks in this turn to rewrite, shorten, correct or reword text, or asks for a suggestion of how to word it ("viết lại", "gợi ý viết lại", "đề xuất cách viết", "sửa câu này", "rút gọn"), AND has highlighted the passage (the <document_selection> block). Such a request on a highlighted passage always goes through this tool — never answer it by writing the new text in the chat, where it cannot be applied: a proposal does not change the document, it shows the text with a "Thay vào văn bản" button. Without a selection the tool refuses; an edit outside the selected passage is refused too. To point out problems without changing the text, use mark_passages.

## Propose or apply

- mode "propose" (the default for a highlighted passage): the document is NOT changed. The rewritten versions are shown to the user under your answer, each with a "Thay vào văn bản" button; the user picks one and it replaces the highlighted passage. Never say the text was changed — tell the user to choose a version with the button.
- mode "apply": the edit is applied in the editor at once (the user undoes it with Ctrl+Z). Pass it ONLY when the user explicitly asked to apply the change right away ("sửa luôn", "thay luôn vào văn bản").

## Input

- edits: up to 30 edits, each with
  - paragraph: the paragraph index from read_document_outline, or
  - match: a distinctive piece of the paragraph's current text (the selected text works). An ambiguous match fails that edit and lists the candidates — retry with the paragraph index.
  - old: the exact substring to replace inside that paragraph (preferred); omit to replace the whole paragraph text.
  - new: the replacement text (required; it may be empty only together with old, to delete that substring).
- One version is the default: edits[0].new, written in Vietnamese administrative register (văn phong hành chính) or as the user's guidance says ("gọn hơn", "bỏ câu cuối").
- options_requested: true ONLY when the user explicitly asked for several options in this turn ("vài phương án", "2 cách viết", "cho tôi lựa chọn"). Without it, variants are ignored and only edits[0].new is proposed.
- variants: with options_requested and exactly one edit: other wordings of that edit's new text. edits[0].new is version 1; at most 3 versions in all.
- labels: optional short names of the versions in order (version 1 = edits[0].new); default "Văn phong hành chính" for a single version, "Phương án 1", "Phương án 2"… for options.
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
    "mode": {
      "type": "string",
      "enum": ["propose", "apply"],
      "description": "propose (default with a highlighted passage): show the versions for the user to choose, the document is not changed; apply: change the document at once — only when the user explicitly asked to apply immediately"
    },
    "options_requested": {
      "type": "boolean",
      "description": "true only when the user explicitly asked for several options; variants are ignored otherwise"
    },
    "variants": {
      "type": "array",
      "maxItems": 3,
      "items": {"type": "string"},
      "description": "Only with options_requested and exactly one edit: alternative wordings of edits[0].new (which is version 1); at most 3 versions in all"
    },
    "labels": {
      "type": "array",
      "maxItems": 3,
      "items": {"type": "string"},
      "description": "Optional short names of the versions in order (version 1 = edits[0].new)"
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
	Mode     string        `json:"mode"`
	Variants []string      `json:"variants"`
	Labels   []string      `json:"labels"`
	// OptionsRequested: the user asked for several versions; without it
	// variants are dropped (one version is the default).
	OptionsRequested bool `json:"options_requested"`
}

// singleRewriteLabel names the one version of a proposal by default.
const singleRewriteLabel = "Văn phong hành chính"

// noteVariantsIgnored is said when variants came without options_requested.
const noteVariantsIgnored = "Chỉ đề xuất một phương án (các phương án khác bị bỏ qua: người dùng không yêu cầu nhiều lựa chọn)."

// Rewrite modes: a proposal leaves the document as it is and lets the user
// apply one version from the chat; apply hands the ops to the editor now.
const (
	rewriteModePropose = "propose"
	rewriteModeApply   = "apply"
)

// maxRewriteVariants caps the versions of one proposal.
const maxRewriteVariants = 3

// rewriteMode is the mode a call runs in: the one asked for, else propose
// when the user's selection is in the target document, else apply.
func rewriteMode(asked string, sel *types.DocumentSelection, target *types.DocumentWorkspace) string {
	switch asked {
	case rewriteModePropose, rewriteModeApply:
		return asked
	}
	if selectionIn(sel, target) {
		return rewriteModePropose
	}
	return rewriteModeApply
}

// rewriteVersions is the list of new texts a proposal offers: edits[0].new
// then the variants (a variant repeating an earlier text is dropped).
func rewriteVersions(in rewriteParagraphsInput) ([]string, string) {
	if len(in.Variants) == 0 {
		return nil, ""
	}
	if len(in.Edits) != 1 {
		return nil, "variants chỉ dùng được khi edits có đúng một chỗ sửa"
	}
	versions := []string{*in.Edits[0].New}
	for i, v := range in.Variants {
		if in.Edits[0].Old == nil && strings.TrimSpace(v) == "" {
			return nil, fmt.Sprintf("variants[%d] is empty", i)
		}
		dup := false
		for _, have := range versions {
			if anchorText(have) == anchorText(v) {
				dup = true
				break
			}
		}
		if !dup {
			versions = append(versions, v)
		}
	}
	if len(versions) > maxRewriteVariants {
		return nil, fmt.Sprintf("too many versions (%d): at most %d in all, edits[0].new counting as version 1",
			len(versions), maxRewriteVariants)
	}
	return versions, ""
}

// rewriteVariant is one version of a proposal, reported in Data.variants:
// the frontend applies its ops when the user picks it.
type rewriteVariant struct {
	ID    string       `json:"id"`
	Label string       `json:"label"`
	Old   string       `json:"old"`
	New   string       `json:"new"`
	Ops   []DocumentOp `json:"ops"`
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

	switch in.Mode {
	case "", rewriteModePropose, rewriteModeApply:
	default:
		return &types.ToolResult{Success: false, Error: fmt.Sprintf("mode %q: use propose or apply", in.Mode)}, nil
	}
	variantsIgnored := false
	if len(in.Variants) > 0 && !in.OptionsRequested {
		in.Variants, variantsIgnored = nil, true
	}
	versions, msg := rewriteVersions(in)
	if msg != "" {
		return &types.ToolResult{Success: false, Error: msg}, nil
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
	if rewriteMode(in.Mode, sel, target) == rewriteModePropose {
		return t.propose(ctx, in, versions, sel, target, variantsIgnored)
	}
	if len(versions) > 0 {
		return &types.ToolResult{Success: false, Error: "variants chỉ dùng khi đề xuất (mode propose): khi áp dụng ngay chỉ có một nội dung mới"}, nil
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
	ops, changes, planned, failed := planRewrites(doc, paras, sel, in.Edits)

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
		default:
			writeRewriteSkip(&out, ch)
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

// planRewrites plans the edits in order on one virtual copy of paras.
func planRewrites(doc *docxedit.Document, paras []docxedit.Paragraph, sel *types.DocumentSelection, edits []rewriteEdit) ([]DocumentOp, []rewriteChange, int, int) {
	vdoc := newVirtualDoc(paras)
	var ops []DocumentOp
	changes := make([]rewriteChange, 0, len(edits))
	planned, failed := 0, 0
	for _, e := range edits {
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
	return ops, changes, planned, failed
}

// writeRewriteSkip reports an edit that was not planned.
func writeRewriteSkip(out *strings.Builder, ch rewriteChange) {
	switch {
	case ch.Status == "unchanged":
		fmt.Fprintf(out, "- Đoạn [%d]: nội dung mới trùng nội dung cũ, không thay đổi\n", ch.Paragraph)
	case ch.Paragraph >= 0:
		fmt.Fprintf(out, "- Đoạn [%d]: KHÔNG sửa — %s\n", ch.Paragraph, ch.Error)
	default:
		fmt.Fprintf(out, "- KHÔNG sửa — %s\n", ch.Error)
	}
}

// rewriteSingleProposalNote ends the Output of a one-version proposal.
const rewriteSingleProposalNote = "Văn bản CHƯA thay đổi. Người dùng bấm nút “Thay vào văn bản” dưới câu trả lời để thay đoạn đã bôi đen; " +
	"hãy nói ngắn là đã viết lại đoạn đó (theo văn phong hành chính hoặc theo yêu cầu) và mời họ bấm nút — không nói là đã sửa."

// rewriteProposalNote ends the Output of a proposal with several versions.
const rewriteProposalNote = "Văn bản CHƯA thay đổi. Người dùng chọn một phương án bằng nút “Thay vào văn bản” dưới câu trả lời; " +
	"hãy giới thiệu ngắn các phương án và mời họ chọn — không nói là đã sửa."

// propose plans every version of the rewrite on the document as stored, takes
// no snapshot and changes nothing: the user applies one version from the
// chat (the proposals/apply route snapshots first).
func (t *RewriteParagraphsTool) propose(ctx context.Context, in rewriteParagraphsInput, versions []string,
	sel *types.DocumentSelection, target *types.DocumentWorkspace, variantsIgnored bool,
) (*types.ToolResult, error) {
	content, ws, err := readListedDocument(ctx, t.workspace, t.sessionID, target)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	doc, err := docxedit.Open(content)
	if err != nil {
		return &types.ToolResult{Success: false, Error: "không đọc được tài liệu: " + err.Error()}, nil
	}
	paras := doc.Paragraphs()
	if len(versions) == 0 {
		versions = []string{*in.Edits[0].New}
	}
	var (
		variants []rewriteVariant
		changes  []rewriteChange
		planned  int
		failed   int
	)
	for k, text := range versions {
		edits := in.Edits
		if len(in.Variants) > 0 {
			e := in.Edits[0]
			nw := text
			e.New = &nw
			edits = []rewriteEdit{e}
		}
		ops, chs, p, f := planRewrites(doc, paras, sel, edits)
		if k == 0 {
			changes, planned, failed = chs, p, f
		}
		if len(ops) == 0 {
			continue
		}
		var olds, news []string
		for _, ch := range chs {
			if ch.Status == "planned" {
				olds, news = append(olds, ch.Old), append(news, ch.New)
			}
		}
		label := singleRewriteLabel
		if len(versions) > 1 {
			label = fmt.Sprintf("Phương án %d", len(variants)+1)
		}
		if k < len(in.Labels) && strings.TrimSpace(in.Labels[k]) != "" {
			label = clipRunes(strings.TrimSpace(in.Labels[k]), 60)
		}
		variants = append(variants, rewriteVariant{
			ID: fmt.Sprintf("v%d", len(variants)+1), Label: label,
			Old: strings.Join(olds, "\n"), New: strings.Join(news, "\n"), Ops: ops,
		})
	}

	var out strings.Builder
	switch {
	case len(variants) == 1:
		fmt.Fprintf(&out, "Đề xuất viết lại đoạn đã bôi đen trong %s (chưa áp dụng):\n", ws.FileName)
	case len(variants) > 1:
		fmt.Fprintf(&out, "Đề xuất %d phương án viết lại đoạn đã bôi đen trong %s (chưa áp dụng):\n", len(variants), ws.FileName)
	default:
		fmt.Fprintf(&out, "Không có thay đổi nào cho %s; tài liệu giữ nguyên.\n", ws.FileName)
	}
	if note := strings.TrimSpace(in.Note); note != "" {
		fmt.Fprintf(&out, "Lý do: %s\n", note)
	}
	if len(variants) > 0 {
		fmt.Fprintf(&out, "Đoạn gốc: “%s”\n", variants[0].Old)
		for _, v := range variants {
			fmt.Fprintf(&out, "- %s: “%s”\n", v.Label, v.New)
		}
	}
	for _, ch := range changes {
		if ch.Status != "planned" {
			writeRewriteSkip(&out, ch)
		}
	}
	if variantsIgnored {
		out.WriteString(noteVariantsIgnored + "\n")
	}
	switch {
	case len(variants) == 1:
		out.WriteString("\n" + rewriteSingleProposalNote + "\n")
	case len(variants) > 1:
		out.WriteString("\n" + rewriteProposalNote + "\n")
	}
	if variants == nil {
		variants = []rewriteVariant{}
	}
	data := map[string]interface{}{
		"proposal":       true,
		"ops_batch_id":   uuid.NewString(),
		"document_id":    ws.ID,
		"document":       DocumentLabel(ws),
		"file_name":      ws.FileName,
		"selection_text": sel.Text,
		"variants":       variants,
		"planned":        planned,
		"failed":         failed,
		"changes":        changes,
	}
	if len(variants) == 0 && failed > 0 {
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
