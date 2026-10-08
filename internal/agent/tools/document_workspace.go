package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// DocumentWorkspaceSource is the part of the document workspace service the
// document-assistant tools use (interfaces.DocumentWorkspaceService
// satisfies it). A session may hold several documents (editor tabs); every
// tool resolves its target with resolveDocument and passes its ID on. The
// tools never write the file: an editing tool takes a Snapshot (which
// force-saves the editor and records a revision), reads the current version
// with OpenCurrent, and returns an edit plan (document_ops) that the editor
// plugin of that document applies inside ONLYOFFICE, where Ctrl+Z undoes it.
type DocumentWorkspaceSource interface {
	GetBySession(ctx context.Context, tenantID uint64, sessionID string) (*types.DocumentWorkspace, error)
	Get(ctx context.Context, tenantID uint64, sessionID, documentID string) (*types.DocumentWorkspace, error)
	List(ctx context.Context, tenantID uint64, sessionID string) ([]*types.DocumentWorkspace, error)
	OpenCurrent(ctx context.Context, tenantID uint64, sessionID, documentID string) (io.ReadCloser, *types.DocumentWorkspace, error)
	Snapshot(ctx context.Context, tenantID uint64, sessionID, documentID, label, source string, wait time.Duration) (*types.DocumentRevision, error)
}

// snapshotWait bounds how long a snapshot waits for the editor to flush
// unsaved changes before the agent plans an edit.
const snapshotWait = 20 * time.Second

const errNoWorkspace = "Cuộc hội thoại này chưa mở tài liệu nào trong trình soạn thảo."

// documentParamSchema is the "document" property every document tool takes.
const documentParamSchema = `"document": {
      "type": "string",
      "description": "Target document: its handle from <session_documents> (vb1, vb2, …). Required when the conversation holds more than one document; for an edit it must be a document the user named with @ (or selected text in) in this request"
    }`

// DocumentLabel names a document for the model: "vb2 · Tờ trình.docx".
func DocumentLabel(ws *types.DocumentWorkspace) string {
	if ws == nil {
		return ""
	}
	return ws.Label()
}

func documentList(docs []*types.DocumentWorkspace) string {
	labels := make([]string, 0, len(docs))
	for _, d := range docs {
		labels = append(labels, DocumentLabel(d))
	}
	return strings.Join(labels, "; ")
}

// matchDocument finds ref among docs: a handle (vb2), a workspace ID, then
// an exact and finally a unique partial file-name match.
func matchDocument(docs []*types.DocumentWorkspace, ref string) *types.DocumentWorkspace {
	ref = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ref), "@"))
	if ref == "" {
		return nil
	}
	lower := strings.ToLower(ref)
	for _, d := range docs {
		if strings.EqualFold(d.Handle(), ref) || d.ID == ref || strings.EqualFold(DocumentLabel(d), ref) {
			return d
		}
	}
	for _, d := range docs {
		if strings.ToLower(d.FileName) == lower {
			return d
		}
	}
	var found *types.DocumentWorkspace
	for _, d := range docs {
		if strings.Contains(strings.ToLower(d.FileName), lower) {
			if found != nil {
				return nil
			}
			found = d
		}
	}
	return found
}

// namedDocuments returns the IDs of the documents the user designated in
// this turn: the @-mentioned ones and the one their selection is in.
func namedDocuments(ctx context.Context) map[string]bool {
	named := map[string]bool{}
	for _, id := range types.MentionedDocumentsFromContext(ctx) {
		named[id] = true
	}
	if sel := types.DocumentSelectionFromContext(ctx); sel != nil && sel.DocumentID != "" {
		named[sel.DocumentID] = true
	}
	return named
}

// resolveDocument picks the document a tool works on. ref is the tool's
// "document" argument ("" when omitted). Without one it falls back to the
// document the user designated in this turn (selection, then a single
// @-mention), then to the only document, then — for reading only — to the
// active tab. With forEdit and several documents, the target must be one
// the user designated in this turn: an edit never lands on a document the
// user did not name. The error text is shown to the agent.
func resolveDocument(ctx context.Context, src DocumentWorkspaceSource, sessionID, ref string, forEdit bool) (*types.DocumentWorkspace, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || src == nil || sessionID == "" {
		return nil, errors.New(errNoWorkspace)
	}
	docs, err := src.List(ctx, tenantID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("không đọc được danh sách văn bản đang mở: %w", err)
	}
	if len(docs) == 0 {
		return nil, errors.New(errNoWorkspace)
	}
	byID := make(map[string]*types.DocumentWorkspace, len(docs))
	for _, d := range docs {
		byID[d.ID] = d
	}
	named := namedDocuments(ctx)

	var target *types.DocumentWorkspace
	switch {
	case strings.TrimSpace(ref) != "":
		target = matchDocument(docs, ref)
		if target == nil {
			return nil, fmt.Errorf("không có văn bản %q trong cuộc hội thoại; các văn bản đang mở: %s", ref, documentList(docs))
		}
	case len(docs) == 1:
		target = docs[0]
	default:
		if sel := types.DocumentSelectionFromContext(ctx); sel != nil && byID[sel.DocumentID] != nil {
			target = byID[sel.DocumentID]
		} else if mentioned := types.MentionedDocumentsFromContext(ctx); len(mentioned) == 1 && byID[mentioned[0]] != nil {
			target = byID[mentioned[0]]
		} else if forEdit {
			return nil, fmt.Errorf("cuộc hội thoại đang mở %d văn bản (%s); hãy truyền tham số document là văn bản người dùng đã gọi đích danh bằng @", len(docs), documentList(docs))
		} else {
			active, err := src.GetBySession(ctx, tenantID, sessionID)
			if err != nil || active == nil {
				return nil, errors.New(errNoWorkspace)
			}
			target = active
		}
	}
	if forEdit && len(docs) > 1 && !named[target.ID] {
		return nil, fmt.Errorf("không được sửa %s: cuộc hội thoại đang mở %d văn bản và người dùng chưa gọi đích danh văn bản này trong yêu cầu. "+
			"Đừng sửa; hãy hỏi người dùng muốn sửa văn bản nào và nhắc họ gõ @ trong ô chat để chọn văn bản đó.",
			DocumentLabel(target), len(docs))
	}
	return target, nil
}

// selectionIn reports whether the turn's selection was made in ws (a
// selection from a client that does not say where counts as in it).
func selectionIn(sel *types.DocumentSelection, ws *types.DocumentWorkspace) bool {
	return sel != nil && ws != nil && (sel.DocumentID == "" || sel.DocumentID == ws.ID)
}

// errSelectionElsewhere is shown when the passage the user highlighted is
// in another document than the one a tool was asked to change.
func errSelectionElsewhere(sel *types.DocumentSelection, ws *types.DocumentWorkspace) string {
	where := sel.Document
	if where == "" {
		where = "một văn bản khác"
	}
	return fmt.Sprintf("Đoạn người dùng bôi đen nằm ở %s, không phải %s; hãy dùng đúng văn bản chứa đoạn bôi đen hoặc hỏi lại người dùng.",
		where, DocumentLabel(ws))
}

// readWorkspaceDocument returns the latest bytes of one document of the
// session ("" = the active one). The bytes come from workspaceDocs when the row OpenCurrent
// returns still describes the cached file; the reader is then closed
// unread, so the file is not downloaded again.
func readWorkspaceDocument(ctx context.Context, src DocumentWorkspaceSource, sessionID, documentID string) ([]byte, *types.DocumentWorkspace, error) {
	doc, ws, err := openWorkspaceDoc(ctx, src, sessionID, documentID, nil)
	if err != nil {
		return nil, nil, err
	}
	return doc.content, ws, nil
}

// readListedDocument is readWorkspaceDocument for a row the caller already
// has from List (resolveDocument, the session's tabs): when that row still
// describes the cached file, storage is not touched at all.
func readListedDocument(ctx context.Context, src DocumentWorkspaceSource, sessionID string, listed *types.DocumentWorkspace) ([]byte, *types.DocumentWorkspace, error) {
	doc, ws, err := openWorkspaceDoc(ctx, src, sessionID, listed.ID, listed)
	if err != nil {
		return nil, nil, err
	}
	return doc.content, ws, nil
}

// readWorkspaceLayout is readListedDocument plus the parsed layout, parsed
// once per file version. The bytes and the layout are shared: read only.
func readWorkspaceLayout(ctx context.Context, src DocumentWorkspaceSource, sessionID string, listed *types.DocumentWorkspace) ([]byte, *docformat.Layout, *types.DocumentWorkspace, error) {
	doc, ws, err := openWorkspaceDoc(ctx, src, sessionID, listed.ID, listed)
	if err != nil {
		return nil, nil, nil, err
	}
	return doc.content, workspaceDocs.layoutOf(doc), ws, nil
}

// openWorkspaceDoc returns the cached document and the freshest row known
// for it, reading the file only when its version changed.
func openWorkspaceDoc(ctx context.Context, src DocumentWorkspaceSource, sessionID, documentID string, listed *types.DocumentWorkspace) (*workspaceDoc, *types.DocumentWorkspace, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || src == nil || sessionID == "" {
		return nil, nil, errors.New(errNoWorkspace)
	}
	key := workspaceDocKey(tenantID, sessionID, documentID)
	if listed != nil {
		if doc := workspaceDocs.get(key, workspaceDocVersion(listed)); doc != nil {
			return doc, listed, nil
		}
	}
	rc, ws, err := src.OpenCurrent(ctx, tenantID, sessionID, documentID)
	if err != nil {
		return nil, nil, fmt.Errorf("không mở được tài liệu đang soạn thảo: %w", err)
	}
	defer rc.Close()
	key = workspaceDocKey(tenantID, sessionID, ws.ID) // documentID may be "" (active tab)
	if doc := workspaceDocs.get(key, workspaceDocVersion(ws)); doc != nil {
		return doc, ws, nil
	}
	content, err := io.ReadAll(io.LimitReader(rc, maxFormatCheckBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("không đọc được tài liệu đang soạn thảo: %w", err)
	}
	if len(content) > maxFormatCheckBytes {
		return nil, nil, fmt.Errorf("%s quá lớn (tối đa 30 MB)", ws.FileName)
	}
	return workspaceDocs.put(key, ws, content), ws, nil
}

// snapshotDocument records a revision of the document as it is now (the
// point the user can restore if the AI edit is unwanted) and reads that
// version to plan the edit on. label is a short description of the edit.
func snapshotDocument(ctx context.Context, src DocumentWorkspaceSource, sessionID, documentID, label string) ([]byte, *types.DocumentWorkspace, int, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || src == nil || sessionID == "" {
		return nil, nil, 0, errors.New(errNoWorkspace)
	}
	rev, err := src.Snapshot(ctx, tenantID, sessionID, documentID, "ai: "+label, types.DocumentRevisionSourceAI, snapshotWait)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("không lưu được bản chụp tài liệu trước khi sửa: %w", err)
	}
	content, ws, err := readWorkspaceDocument(ctx, src, sessionID, documentID)
	if err != nil {
		return nil, nil, 0, err
	}
	seq := 0
	if rev != nil {
		seq = rev.Seq
	}
	return content, ws, seq, nil
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// paragraphComponents maps each paragraph index to the NĐ30 components the
// segmenter assigned it, in NĐ30 reading order.
func paragraphComponents(report *docformat.Report) map[int][]string {
	out := map[int][]string{}
	if report == nil {
		return out
	}
	for _, def := range docformat.Components {
		c, ok := report.Components[def.Key]
		if !ok || c == nil || !c.Found {
			continue
		}
		for _, idx := range c.Paras {
			keys := out[idx]
			if len(keys) == 0 || keys[len(keys)-1] != def.Key {
				out[idx] = append(keys, def.Key)
			}
		}
	}
	return out
}

// segmentReport runs the NĐ30 checker on content. With a chat model the
// components are labelled by the model (the "auto" segmenter, as in
// check_document_format); without one, or when the labelled check fails,
// the positional heuristic is used. A labelling error already degrades to
// the heuristic inside docformat.Check.
func segmentReport(ctx context.Context, content []byte, docType, name string, chatModel chat.Chat) (*docformat.Report, error) {
	if chatModel != nil {
		report := docformat.Check(ctx, content, docformat.Options{
			DocumentType: docType, SourceName: name,
			LLM: docformat.ChatCompleter(chatModel), ModelName: chatModel.GetModelName(),
		})
		if report.OK {
			return report, nil
		}
		logger.Warnf(ctx, "document tools: labelled format check failed, using the heuristic: %s", report.Error)
	}
	report := docformat.Check(ctx, content, docformat.Options{
		DocumentType: docType, Segmenter: docformat.SegmenterHeuristic, SourceName: name,
	})
	if !report.OK {
		return nil, errors.New("không phân tích được tài liệu: " + report.Error)
	}
	return report, nil
}

// ---------------------------------------------------------------------------
// read_document_outline

var readDocumentOutlineTool = BaseTool{
	name: ToolReadDocumentOutline,
	description: `List the paragraphs of a Word document open in this conversation's editor (the "document" argument picks which), numbered, with the NĐ30 component each belongs to (quoc_hieu, trich_yeu, noi_dung, …) and its formatting (font, size, bold/italic, alignment).

## When to Use

- To find the paragraph index before rewrite_paragraphs, or to see which paragraphs a format fix would touch.
- To read the document's current text (it reflects the latest saved editor state).

## Input

- from: first paragraph index to list (default 0).
- limit: how many non-empty paragraphs to list (default 80, max 200).
- component: list only one component's paragraphs, e.g. "noi_dung" or "trich_yeu".

## Output

Lines like ` + "`[12] (trich_yeu) Times New Roman 14 đậm, giữa | V/v …`" + `: the index in brackets is the paragraph number the editing tools take. Long texts are cut at 160 characters. Empty paragraphs are not listed.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + documentParamSchema + `,
    "from": {
      "type": "integer",
      "minimum": 0,
      "description": "First paragraph index to list (default 0)"
    },
    "limit": {
      "type": "integer",
      "minimum": 1,
      "maximum": 200,
      "description": "Maximum number of non-empty paragraphs to list (default 80, max 200)"
    },
    "component": {
      "type": "string",
      "description": "Only list paragraphs of this NĐ30 component, e.g. noi_dung, trich_yeu, quoc_hieu"
    }
  }
}`),
}

type readDocumentOutlineInput struct {
	From      int    `json:"from"`
	Limit     int    `json:"limit"`
	Component string `json:"component"`
	Document  string `json:"document"`
}

// ReadDocumentOutlineTool lists the paragraphs of the session's workspace
// document with their component labels and formatting.
type ReadDocumentOutlineTool struct {
	BaseTool
	workspace DocumentWorkspaceSource
	chatModel chat.Chat
	sessionID string
}

// NewReadDocumentOutlineTool builds the tool for one session. chatModel may
// be nil: components are then labelled by the positional heuristic.
func NewReadDocumentOutlineTool(workspace DocumentWorkspaceSource, chatModel chat.Chat, sessionID string) *ReadDocumentOutlineTool {
	return &ReadDocumentOutlineTool{BaseTool: readDocumentOutlineTool, workspace: workspace, chatModel: chatModel, sessionID: sessionID}
}

const (
	outlineDefaultLimit = 80
	outlineMaxLimit     = 200
	outlineTextRunes    = 160
)

var alignmentVI = map[string]string{
	"left": "trái", "center": "giữa", "right": "phải", "justify": "đều", "both": "đều", "distribute": "đều",
}

// formatSummary renders a paragraph's formatting compactly in Vietnamese.
func formatSummary(p *docformat.Para) string {
	var parts []string
	if p.FontName != nil && *p.FontName != "" {
		parts = append(parts, *p.FontName)
	}
	if p.SizePt != nil {
		parts = append(parts, strconv.FormatFloat(*p.SizePt, 'f', -1, 64))
	}
	var emph []string
	if p.Bold != nil && *p.Bold {
		emph = append(emph, "đậm")
	}
	if p.Italic != nil && *p.Italic {
		emph = append(emph, "nghiêng")
	}
	head := strings.Join(append(parts, emph...), " ")
	align := alignmentVI[p.Alignment]
	if align == "" {
		align = p.Alignment
	}
	if p.Zone == docformat.ZoneSplit {
		align += ", chia hai cột bằng tab"
	}
	if head == "" {
		return align
	}
	return head + ", " + align
}

func (t *ReadDocumentOutlineTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var in readDocumentOutlineInput
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return &types.ToolResult{Success: false, Error: "invalid arguments: " + err.Error()}, nil
		}
	}
	if in.From < 0 {
		in.From = 0
	}
	if in.Limit <= 0 {
		in.Limit = outlineDefaultLimit
	}
	if in.Limit > outlineMaxLimit {
		in.Limit = outlineMaxLimit
	}
	in.Component = strings.TrimSpace(in.Component)

	target, err := resolveDocument(ctx, t.workspace, t.sessionID, in.Document, false)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	content, layout, ws, err := readWorkspaceLayout(ctx, t.workspace, t.sessionID, target)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	if len(layout.Paragraphs) == 0 && len(layout.Errors) > 0 {
		return &types.ToolResult{Success: false, Error: "không phân tích được tài liệu: " + strings.Join(layout.Errors, "; ")}, nil
	}
	report, err := segmentReport(ctx, content, "", ws.FileName, t.chatModel)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	comps := paragraphComponents(report)
	var only map[int]bool
	if in.Component != "" {
		// read the component itself: aggregates such as "signature" are not
		// among the per-paragraph labels
		only = map[int]bool{}
		if c := report.Components[in.Component]; c != nil && c.Found {
			for _, idx := range c.Paras {
				only[idx] = true
			}
		}
	}

	var b strings.Builder
	listed, to := 0, in.From
	for i := in.From; i < len(layout.Paragraphs); i++ {
		if listed >= in.Limit {
			break
		}
		to = i + 1
		p := layout.Paragraphs[i]
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}
		keys := comps[i]
		if only != nil && !only[i] {
			continue
		}
		label := ""
		if len(keys) > 0 {
			label = "(" + strings.Join(keys, ", ") + ") "
		}
		text = strings.NewReplacer("\t", " ⇥ ", "\n", " ↵ ").Replace(text)
		fmt.Fprintf(&b, "[%d] %s%s | %s\n", i, label, formatSummary(p), clipRunes(text, outlineTextRunes))
		listed++
	}

	var out strings.Builder
	docType := ""
	if report.DocumentType != nil {
		docType = report.DocumentType.Used
	}
	fmt.Fprintf(&out, "# Dàn ý tài liệu %s (phiên bản %d, %d đoạn, loại văn bản nhận dạng: %s)\n",
		ws.FileName, ws.Revision, len(layout.Paragraphs), docType)
	if in.Component != "" {
		fmt.Fprintf(&out, "Chỉ liệt kê thành phần %q.\n", in.Component)
	}
	out.WriteString("Số trong [ ] là chỉ số đoạn dùng cho rewrite_paragraphs. Nhãn thành phần do bộ phân đoạn gán, có thể chưa chính xác.\n\n")
	if listed == 0 {
		out.WriteString("(không có đoạn nào có nội dung trong phạm vi này)\n")
	} else {
		out.WriteString(b.String())
	}
	if to < len(layout.Paragraphs) {
		fmt.Fprintf(&out, "\nCòn các đoạn sau: gọi lại với from=%d.\n", to)
	}
	return &types.ToolResult{
		Success: true,
		Output:  out.String(),
		Data: map[string]interface{}{
			"file_name":         ws.FileName,
			"document_revision": ws.Revision,
			"paragraph_count":   len(layout.Paragraphs),
			"from":              in.From,
			"to":                to,
		},
	}, nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
