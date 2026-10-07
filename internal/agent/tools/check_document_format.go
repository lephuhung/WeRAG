package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// ToolCheckDocumentFormat checks the thể thức of a .docx the user uploaded
// in the conversation against Nghị định 30/2020/NĐ-CP.
const ToolCheckDocumentFormat = "check_document_format"

// maxFormatCheckBytes caps the attachment read into memory.
const maxFormatCheckBytes = 30 << 20

var checkDocumentFormatTool = BaseTool{
	name: ToolCheckDocumentFormat,
	description: `Check the format ("thể thức") of a Word (.docx) administrative document the user uploaded in this conversation against Nghị định 30/2020/NĐ-CP, Phụ lục I: required components (quốc hiệu, tiêu ngữ, cơ quan ban hành, số ký hiệu, địa danh-ngày tháng, trích yếu, chữ ký, nơi nhận…), their position (left/right column), font, size, bold/italic, alignment, order, paper size and margins.

## When to Use

ONLY when the user explicitly asks to check, review or evaluate the format of an uploaded document — e.g. "kiểm tra thể thức", "văn bản này đúng thể thức chưa", "soát lỗi trình bày / căn lề / cỡ chữ / font", "đúng Nghị định 30 không".

Do NOT call it just because a .docx is attached, nor to summarize, translate, answer questions about or extract content from a document — read the attachment content for that. It checks layout only: for spelling (chính tả) questions, proofread the attachment text yourself.

## Input

- file_name: the uploaded file to check; omit when only one .docx was uploaded (the newest .docx is used).
- document_type: optional rule set (cong_van, quyet_dinh, bao_cao, to_trinh, ke_hoach, thong_bao, …); omit to detect it.

## Output

Usually a finished evaluation: the file's measured formatting judged by a reasoning model against the NĐ30 skills of the document's type (signing authority, Nơi nhận, required parts, wording, spelling, plus the measured font/size/alignment/margin findings). Present it to the user as is — do not re-judge it. The check takes about a minute.

If the evaluation was unavailable, the result instead holds the measured findings, the format data line by line and the <skill> blocks: apply the skills to the data yourself, keep the measured findings, and answer grouped by component with the rule, the quoted line and the fix. Either way, say that seals, signatures and page numbers still need a manual review.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "file_name": {
      "type": "string",
      "description": "Name of the uploaded .docx to check; omit to use the newest .docx of the conversation"
    },
    "document_type": {
      "type": "string",
      "description": "Optional rule set, e.g. cong_van, quyet_dinh, bao_cao, to_trinh; omit to auto-detect"
    }
  }
}`),
}

type checkDocumentFormatInput struct {
	FileName     string `json:"file_name"`
	DocumentType string `json:"document_type"`
}

// CheckDocumentFormatTool reads a session attachment and runs the NĐ30
// format check, labelling components with the agent's chat model.
type CheckDocumentFormatTool struct {
	BaseTool
	documents interfaces.TemporaryDocumentService
	chatModel chat.Chat
	sessionID string
}

// NewCheckDocumentFormatTool builds the tool for one session. chatModel may
// be nil: the positional heuristic is used then.
func NewCheckDocumentFormatTool(documents interfaces.TemporaryDocumentService, chatModel chat.Chat, sessionID string) *CheckDocumentFormatTool {
	return &CheckDocumentFormatTool{BaseTool: checkDocumentFormatTool, documents: documents, chatModel: chatModel, sessionID: sessionID}
}

// IsDocx reports whether a temporary document is a Word .docx file.
func IsDocx(doc *types.TemporaryDocument) bool {
	if doc == nil {
		return false
	}
	return strings.EqualFold(strings.TrimPrefix(doc.FileType, "."), "docx") ||
		strings.HasSuffix(strings.ToLower(doc.FileName), ".docx")
}

// pickDocx selects the attachment to check: an exact (then partial) name
// match when a name is given, else the newest .docx.
func pickDocx(docs []*types.TemporaryDocument, name string) (*types.TemporaryDocument, []string) {
	var docx []*types.TemporaryDocument
	var names []string
	for _, d := range docs {
		if IsDocx(d) {
			docx = append(docx, d)
			names = append(names, d.FileName)
		}
	}
	if len(docx) == 0 {
		return nil, nil
	}
	sort.SliceStable(docx, func(i, j int) bool { return docx[i].CreatedAt.After(docx[j].CreatedAt) })
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return docx[0], names
	}
	for _, d := range docx {
		if strings.ToLower(d.FileName) == name {
			return d, names
		}
	}
	for _, d := range docx {
		if strings.Contains(strings.ToLower(d.FileName), name) {
			return d, names
		}
	}
	return nil, names
}

func (t *CheckDocumentFormatTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var in checkDocumentFormatInput
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return &types.ToolResult{Success: false, Error: "invalid arguments: " + err.Error()}, nil
		}
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || t.sessionID == "" {
		return &types.ToolResult{Success: false, Error: "no conversation context to read uploads from"}, nil
	}
	docs, err := t.documents.List(ctx, tenantID, t.sessionID)
	if err != nil {
		return &types.ToolResult{Success: false, Error: "could not list the uploaded files"}, nil
	}
	doc, names := pickDocx(docs, in.FileName)
	if doc == nil {
		msg := "Không có file .docx nào được tải lên trong cuộc hội thoại này. Kiểm tra thể thức chỉ hỗ trợ file Word .docx (không hỗ trợ .doc, PDF hay ảnh)."
		if len(names) > 0 {
			msg = fmt.Sprintf("Không tìm thấy file %q. Các file .docx đã tải lên: %s", in.FileName, strings.Join(names, ", "))
		}
		return &types.ToolResult{Success: false, Error: msg}, nil
	}
	rc, _, err := t.documents.OpenFile(ctx, tenantID, t.sessionID, doc.ID)
	if err != nil {
		return &types.ToolResult{Success: false, Error: "could not open " + doc.FileName}, nil
	}
	defer rc.Close()
	content, err := io.ReadAll(io.LimitReader(rc, maxFormatCheckBytes+1))
	if err != nil {
		return &types.ToolResult{Success: false, Error: "could not read " + doc.FileName}, nil
	}
	if len(content) > maxFormatCheckBytes {
		return &types.ToolResult{Success: false, Error: doc.FileName + " is too large to check (max 30 MB)"}, nil
	}

	opts := docformat.Options{DocumentType: in.DocumentType, SourceName: doc.FileName}
	if t.chatModel != nil {
		opts.LLM = docformat.ChatCompleter(t.chatModel)
		opts.ModelName = t.chatModel.GetModelName()
	}
	report := docformat.Check(ctx, content, opts)
	if !report.OK {
		return &types.ToolResult{Success: false, Error: docformat.RenderText(report)}, nil
	}
	output, evaluated := t.evaluate(ctx, report)
	return &types.ToolResult{
		Success: true,
		Output:  output,
		Data: map[string]interface{}{
			"file_name":     doc.FileName,
			"document_type": report.DocumentType,
			"summary":       report.Summary,
			"method":        report.Segmentation.Method,
			"skills":        report.Skills,
			"evaluated":     evaluated,
		},
	}, nil
}

// evaluateTimeout keeps the reasoning call inside the agent's tool budget
// (checkDocumentFormatToolTimeout) with room left for the fallback.
const evaluateTimeout = 3*time.Minute + 30*time.Second

// evaluate has the chat model judge the report against the document-type
// skills with thinking on. Without a model, or when the call fails, the
// agent gets the data and the skills to judge itself.
func (t *CheckDocumentFormatTool) evaluate(ctx context.Context, report *docformat.Report) (string, bool) {
	if t.chatModel != nil {
		evalCtx, cancel := context.WithTimeout(ctx, evaluateTimeout)
		defer cancel()
		text, err := docformat.EvaluateWithSkills(evalCtx, docformat.ChatEvaluator(t.chatModel), report)
		if err == nil {
			report.Evaluation = text
			return "# Đánh giá thể thức văn bản " + report.Source + "\n" +
				"(đã thẩm định theo NĐ30/2020/NĐ-CP: số đo từ file và các skill " + strings.Join(report.Skills, ", ") + ")\n\n" +
				text + "\n\n---\nTrình bày đánh giá trên cho người dùng; giữ nguyên các kết luận, không tự chấm lại.", true
		}
		logger.Warnf(ctx, "check_document_format: skill evaluation failed, returning data: %v", err)
	}
	return docformat.RenderForAgent(report), false
}
