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
	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// DocumentWorkspaceSource is the part of the document workspace service the
// document-assistant tools use (interfaces.DocumentWorkspaceService
// satisfies it). Reads go through OpenCurrent; every write follows the
// PrepareExternalWrite → edit → CommitExternalWrite protocol so unsaved
// editor changes are flushed first and a concurrent save is detected.
type DocumentWorkspaceSource interface {
	GetBySession(ctx context.Context, tenantID uint64, sessionID string) (*types.DocumentWorkspace, error)
	OpenCurrent(ctx context.Context, tenantID uint64, sessionID string) (io.ReadCloser, *types.DocumentWorkspace, error)
	PrepareExternalWrite(ctx context.Context, tenantID uint64, sessionID string, wait time.Duration) (*types.DocumentWorkspace, []byte, error)
	CommitExternalWrite(ctx context.Context, tenantID uint64, sessionID string, expectedRevision int, data []byte) (*types.DocumentWorkspace, error)
}

// documentEditAuthor signs every tracked change the agent writes.
var documentEditAuthor = docxedit.Author{Name: "Trợ lý AI (WeRAG)"}

// prepareWriteWait bounds how long a write waits for the editor to flush
// unsaved changes before the agent edits the file.
const prepareWriteWait = 20 * time.Second

const errNoWorkspace = "Cuộc hội thoại này chưa mở tài liệu nào trong trình soạn thảo."

// readWorkspaceDocument returns the latest bytes of the session's document.
func readWorkspaceDocument(ctx context.Context, src DocumentWorkspaceSource, sessionID string) ([]byte, *types.DocumentWorkspace, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || src == nil || sessionID == "" {
		return nil, nil, errors.New(errNoWorkspace)
	}
	rc, ws, err := src.OpenCurrent(ctx, tenantID, sessionID)
	if err != nil {
		return nil, nil, fmt.Errorf("không mở được tài liệu đang soạn thảo: %w", err)
	}
	defer rc.Close()
	content, err := io.ReadAll(io.LimitReader(rc, maxFormatCheckBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("không đọc được tài liệu đang soạn thảo: %w", err)
	}
	if len(content) > maxFormatCheckBytes {
		return nil, nil, fmt.Errorf("%s quá lớn (tối đa 30 MB)", ws.FileName)
	}
	return content, ws, nil
}

// prepareWorkspaceWrite flushes the editor and returns the bytes to edit
// with the revision a commit must expect.
func prepareWorkspaceWrite(ctx context.Context, src DocumentWorkspaceSource, sessionID string) (uint64, []byte, *types.DocumentWorkspace, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || src == nil || sessionID == "" {
		return 0, nil, nil, errors.New(errNoWorkspace)
	}
	ws, content, err := src.PrepareExternalWrite(ctx, tenantID, sessionID, prepareWriteWait)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("không chuẩn bị được tài liệu để sửa: %w", err)
	}
	if len(content) > maxFormatCheckBytes {
		return 0, nil, nil, fmt.Errorf("%s quá lớn (tối đa 30 MB)", ws.FileName)
	}
	return tenantID, content, ws, nil
}

// isWorkspaceConflict reports a commit refused because the document's
// revision moved after it was read.
func isWorkspaceConflict(err error) bool {
	var appErr *apperrors.AppError
	return errors.As(err, &appErr) && appErr.Code == apperrors.ErrConflict
}

const conflictRetryMessage = "Tài liệu vừa được lưu từ trình soạn thảo trong lúc sửa nên thay đổi chưa được ghi. " +
	"Hãy gọi lại công cụ (đọc lại dàn ý nếu cần) để sửa trên bản mới nhất."

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
	description: `List the paragraphs of the Word document open in this conversation's editor, numbered, with the NĐ30 component each belongs to (quoc_hieu, trich_yeu, noi_dung, …) and its formatting (font, size, bold/italic, alignment).

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

	content, ws, err := readWorkspaceDocument(ctx, t.workspace, t.sessionID)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	layout := docformat.InspectDocx(content)
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
