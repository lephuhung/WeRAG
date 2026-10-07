package tools

import (
	"context"
	"encoding/json"
	"errors"
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
- document_type: optional rule set (cong_van, quyet_dinh, bao_cao, to_trinh, ke_hoach, thong_bao, …). Omit it: the type is detected from the document. Pass it only when the user names the type or confirms it after a result asked which type the document is — never guess it yourself.

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
      "description": "Rule set named by the user, e.g. cong_van, quyet_dinh, bao_cao, to_trinh; omit to auto-detect (default)"
    }
  }
}`),
}

type checkDocumentFormatInput struct {
	FileName     string `json:"file_name"`
	DocumentType string `json:"document_type"`
}

// formatCheckSource loads the .docx to check: its bytes, its display name
// and, for a workspace document, its revision (-1 otherwise). The error
// text is shown to the agent as is.
type formatCheckSource func(ctx context.Context, tenantID uint64, fileName string) ([]byte, string, int, error)

// CheckDocumentFormatTool reads a session document and runs the NĐ30
// format check, labelling components with the agent's chat model.
type CheckDocumentFormatTool struct {
	BaseTool
	source    formatCheckSource
	chatModel chat.Chat
	sessionID string
}

// NewCheckDocumentFormatTool builds the tool for one session, checking the
// session's uploaded attachments. chatModel may be nil: the positional
// heuristic is used then.
func NewCheckDocumentFormatTool(documents interfaces.TemporaryDocumentService, chatModel chat.Chat, sessionID string) *CheckDocumentFormatTool {
	t := &CheckDocumentFormatTool{BaseTool: checkDocumentFormatTool, chatModel: chatModel, sessionID: sessionID}
	t.source = func(ctx context.Context, tenantID uint64, fileName string) ([]byte, string, int, error) {
		content, name, err := loadUploadedDocx(ctx, documents, tenantID, sessionID, fileName)
		return content, name, -1, err
	}
	return t
}

// NewCheckDocumentFormatToolForWorkspace builds the tool for a session with
// an editable document: it checks the latest saved version of that
// document instead of the uploads.
func NewCheckDocumentFormatToolForWorkspace(workspace DocumentWorkspaceSource, chatModel chat.Chat, sessionID string) *CheckDocumentFormatTool {
	base := checkDocumentFormatTool
	base.description = strings.Replace(base.description,
		"administrative document the user uploaded in this conversation",
		"administrative document — the document open in this conversation's editor (its latest saved version; file_name is ignored)", 1)
	t := &CheckDocumentFormatTool{BaseTool: base, chatModel: chatModel, sessionID: sessionID}
	t.source = func(ctx context.Context, _ uint64, _ string) ([]byte, string, int, error) {
		content, ws, err := readWorkspaceDocument(ctx, workspace, sessionID)
		if err != nil {
			return nil, "", -1, err
		}
		return content, ws.FileName, ws.Revision, nil
	}
	return t
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
	content, fileName, revision, err := t.source(ctx, tenantID, in.FileName)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}

	out := t.check(ctx, content, fileName, in.DocumentType)
	if out.result == nil {
		return &types.ToolResult{Success: false, Error: out.failure}, nil
	}
	data := out.result.data()
	if revision >= 0 {
		data["document_revision"] = revision
	}
	return &types.ToolResult{Success: true, Output: out.result.Output, Data: data}, nil
}

func (t *CheckDocumentFormatTool) modelName() string {
	if t.chatModel == nil {
		return ""
	}
	return t.chatModel.GetModelName()
}

// check runs the format check of content, or returns the evaluation already
// made for the same bytes, model and rule set (see formatCheckCache).
func (t *CheckDocumentFormatTool) check(ctx context.Context, content []byte, fileName, docType string) formatCheckOutcome {
	model := t.modelName()
	key := formatCheckKey(content, model, docType)
	return formatChecks.do(ctx, key, func(runCtx context.Context) formatCheckOutcome {
		opts := docformat.Options{DocumentType: docType, SourceName: fileName}
		if t.chatModel != nil {
			opts.LLM = docformat.ChatCompleter(t.chatModel)
			opts.ModelName = model
		}
		report := docformat.Check(runCtx, content, opts)
		if !report.OK {
			return formatCheckOutcome{failure: docformat.RenderText(report)}
		}
		output, evaluated := t.evaluate(runCtx, report)
		r := &formatCheckResult{
			Output: output, FileName: fileName, DocumentType: report.DocumentType, Summary: report.Summary,
			Skills: report.Skills, Evaluated: evaluated, At: time.Now(),
		}
		if report.Segmentation != nil {
			r.Method = report.Segmentation.Method
		}
		// only a finished evaluation is kept: the unevaluated fallback
		// comes from a failed model call worth retrying
		if evaluated {
			keys := []string{key}
			if report.DocumentType != nil && report.DocumentType.Used != "" {
				// a later call naming the rule set that was used gets the same rules
				keys = append(keys, formatCheckKey(content, model, report.DocumentType.Used))
			}
			formatChecks.put(runCtx, r, keys...)
		}
		return formatCheckOutcome{result: r}
	})
}

// Prewarm runs the check of the session's document in the background, once
// per session, so a later format question is answered from the cache; its
// progress is reported by SessionFormatCheck. It returns at once.
func (t *CheckDocumentFormatTool) Prewarm(ctx context.Context) {
	if t.chatModel == nil || t.sessionID == "" {
		return
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		return
	}
	if _, done := formatChecks.prewarmed.LoadOrStore(t.sessionID, struct{}{}); done {
		return
	}
	// checked before a restart (state kept in Redis), or running elsewhere
	if SessionFormatCheck(ctx, t.sessionID) != nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	go func() {
		content, fileName, revision, err := t.source(ctx, tenantID, "")
		if err != nil {
			formatChecks.prewarmed.Delete(t.sessionID)
			return
		}
		state := &types.DocumentFormatCheck{
			Status: types.DocumentFormatCheckRunning, Revision: revision, StartedAt: time.Now(),
		}
		formatChecks.storeState(ctx, t.sessionID, state)
		out := t.check(ctx, content, fileName, "")
		done := *state
		finished := time.Now()
		done.FinishedAt = &finished
		if out.result == nil || !out.result.Evaluated {
			done.Status = types.DocumentFormatCheckFailed
			formatChecks.storeState(ctx, t.sessionID, &done)
			logger.Warnf(ctx, "check_document_format: background check of %s failed: %s", fileName, out.failure)
			return
		}
		done.Status = types.DocumentFormatCheckReady
		if info := out.result.DocumentType; info != nil {
			done.DocumentType = info.Used
			done.DocumentTypeLabel = documentTypeLabel(info.RuleSet)
		}
		formatChecks.storeState(ctx, t.sessionID, &done)
		logger.Infof(ctx, "check_document_format: background check of %s ready in %s", fileName, finished.Sub(state.StartedAt).Round(time.Second))
	}()
}

// loadUploadedDocx reads the session upload to check (see pickDocx).
func loadUploadedDocx(ctx context.Context, documents interfaces.TemporaryDocumentService,
	tenantID uint64, sessionID, fileName string,
) ([]byte, string, error) {
	docs, err := documents.List(ctx, tenantID, sessionID)
	if err != nil {
		return nil, "", errors.New("could not list the uploaded files")
	}
	doc, names := pickDocx(docs, fileName)
	if doc == nil {
		msg := "Không có file .docx nào được tải lên trong cuộc hội thoại này. Kiểm tra thể thức chỉ hỗ trợ file Word .docx (không hỗ trợ .doc, PDF hay ảnh)."
		if len(names) > 0 {
			msg = fmt.Sprintf("Không tìm thấy file %q. Các file .docx đã tải lên: %s", fileName, strings.Join(names, ", "))
		}
		return nil, "", errors.New(msg)
	}
	rc, _, err := documents.OpenFile(ctx, tenantID, sessionID, doc.ID)
	if err != nil {
		return nil, "", errors.New("could not open " + doc.FileName)
	}
	defer rc.Close()
	content, err := io.ReadAll(io.LimitReader(rc, maxFormatCheckBytes+1))
	if err != nil {
		return nil, "", errors.New("could not read " + doc.FileName)
	}
	if len(content) > maxFormatCheckBytes {
		return nil, "", errors.New(doc.FileName + " is too large to check (max 30 MB)")
	}
	return content, doc.FileName, nil
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
