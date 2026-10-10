package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
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

Do NOT call it just because a .docx is attached, nor to summarize, translate, answer questions about or extract content from a document — read the attachment content for that. It checks layout only: for spelling (chính tả) questions use check_spelling when it is available, otherwise proofread the text yourself.

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
	// Document picks the editable document (workspace variant only).
	Document string `json:"document"`
	// Mark marks the measured findings in the editor (workspace variant
	// only; default true).
	Mark *bool `json:"mark"`
}

// checkWorkspaceFormatSchema is the workspace variant's schema: the target
// is an open document, not an upload.
var checkWorkspaceFormatSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + documentParamSchema + `,
    "document_type": {
      "type": "string",
      "description": "Rule set named by the user, e.g. cong_van, quyet_dinh, bao_cao, to_trinh; omit to auto-detect (default)"
    },
    "mark": {"type": "boolean", "description": "Underline the measured findings in red in the editor: each paragraph that breaks a rule, or the stray character itself (default true)"}
  }
}`)

// formatCheckSource loads the .docx to check: its bytes, its display name
// and, for a workspace document, its revision (-1 otherwise). ref is the
// upload's file name, or the workspace document reference (see
// resolveDocument). The error text is shown to the agent as is.
type formatCheckSource func(ctx context.Context, tenantID uint64, fileName string) ([]byte, string, int, error)

// CheckDocumentFormatTool reads a session document and runs the NĐ30
// format check, labelling components with the agent's chat model.
type CheckDocumentFormatTool struct {
	BaseTool
	source    formatCheckSource
	chatModel chat.Chat
	sessionID string
	// workspace is the editable document's source (nil for uploads); the
	// background re-check reads its save time.
	workspace DocumentWorkspaceSource
	// documentID is the workspace document Prewarm and Recheck work on
	// (see ForDocument); their state is kept under it.
	documentID string
	// profiler makes the document's first profile inside the background
	// run's slot, before the evaluation (see WithProfiler); nil skips it.
	profiler *DocumentProfiler
	// speller reviews the spelling alongside a check that marks the
	// editor (see WithSpelling); nil skips it.
	speller *CheckSpellingTool
}

// WithSpelling returns a copy that also reviews the document's spelling
// when it marks the editor: the spelling pass runs alongside the format
// evaluation and its mistakes are underlined in red with the format
// findings, whatever the model then writes — the agent model alone often
// listed spelling mistakes in the chat without underlining any.
func (t *CheckDocumentFormatTool) WithSpelling(s *CheckSpellingTool) *CheckDocumentFormatTool {
	c := *t
	c.speller = s
	if s != nil {
		c.description += "\n\nIt also reviews the spelling of the document in the same run: each mistake is underlined in red in the editor and listed in the result under \"Lỗi chính tả\". Present that list as the spelling part of your answer; do not call check_spelling again for the same document in this turn."
	}
	return &c
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
// editable documents: it checks the latest saved version of one of them
// (the "document" argument) instead of the uploads.
func NewCheckDocumentFormatToolForWorkspace(workspace DocumentWorkspaceSource, chatModel chat.Chat, sessionID string) *CheckDocumentFormatTool {
	base := checkDocumentFormatTool
	base.description = strings.Replace(base.description,
		"administrative document the user uploaded in this conversation",
		"administrative document — a document open in this conversation's editor (its latest saved version)", 1)
	base.description = strings.Replace(base.description,
		"- file_name: the uploaded file to check; omit when only one .docx was uploaded (the newest .docx is used).",
		"- document: the open document to check (vb1, vb2, …); omit when only one is open.", 1)
	base.description += "\n\nIn the editor, the measured findings that name a paragraph are also underlined in red (unless mark=false): the paragraph, or just the stray character inside a word; the content and formatting are not changed. Tell the user the red underlines show where each finding is; findings without a paragraph (a missing component, the margins) are only in your answer."
	base.schema = checkWorkspaceFormatSchema
	t := &CheckDocumentFormatTool{BaseTool: base, chatModel: chatModel, sessionID: sessionID, workspace: workspace}
	t.source = func(ctx context.Context, _ uint64, ref string) ([]byte, string, int, error) {
		target, err := resolveTargetDocument(ctx, workspace, sessionID, ref, false)
		if err != nil {
			return nil, "", -1, err
		}
		content, ws, err := readListedDocument(ctx, workspace, sessionID, target)
		if err != nil {
			return nil, "", -1, err
		}
		return content, ws.FileName, ws.Revision, nil
	}
	return t
}

// ForDocument returns a copy of the workspace tool bound to one document,
// for the background Prewarm and Recheck.
func (t *CheckDocumentFormatTool) ForDocument(documentID string) *CheckDocumentFormatTool {
	c := *t
	c.documentID = documentID
	return &c
}

// WithProfiler returns a copy whose background run first makes the bound
// document's profile when it has none ready: the profile is the cheap
// call the chat needs to tell documents apart, the evaluation the long
// one, and both share the format-check slots.
func (t *CheckDocumentFormatTool) WithProfiler(p *DocumentProfiler) *CheckDocumentFormatTool {
	c := *t
	c.profiler = p
	return &c
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
	ref := in.FileName
	if t.workspace != nil {
		ref = in.Document
	}
	content, fileName, revision, err := t.source(ctx, tenantID, ref)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}

	// marking the editor: the spelling pass runs alongside the evaluation
	mark := t.workspace != nil && (in.Mark == nil || *in.Mark)
	var spelling chan *spellingPass
	if mark && t.speller != nil {
		spelling = make(chan *spellingPass, 1)
		go func() { spelling <- t.speller.spellingPassOf(ctx, content) }()
	}

	out := t.check(ctx, content, fileName, in.DocumentType)
	if out.result == nil {
		return &types.ToolResult{Success: false, Error: out.failure}, nil
	}
	data := out.result.data()
	output := out.result.Output
	if mark {
		var sp *spellingPass
		if spelling != nil {
			select {
			case sp = <-spelling:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		ops, ws, seq, formatMarks := t.markFindings(ctx, in.Document, content, out.result.Flags, sp)
		if len(ops) > 0 {
			for k, v := range opsData(ops, seq, ws) {
				data[k] = v
			}
		}
		if formatMarks > 0 {
			output += "\n\n" + formatMarksNote + "\n"
		}
		if sp != nil {
			output += "\n" + sp.render(len(ops) > 0)
			data["spelling"] = sp.findings
		}
	}
	if revision >= 0 {
		data["document_revision"] = revision
	}
	return &types.ToolResult{Success: true, Output: output, Data: data}, nil
}

// markFindings plans the editor marks on the checked version: the
// measured format findings and the spelling pass's mistakes (one red
// underline per place). Marking is an edit of the document the user named
// (or the only one): otherwise, or when the snapshot before it fails,
// nothing is marked and the check stands alone. formatMarks counts the
// format findings' marks.
func (t *CheckDocumentFormatTool) markFindings(
	ctx context.Context, ref string, content []byte, flags []formatFlag, sp *spellingPass,
) (ops []DocumentOp, target *types.DocumentWorkspace, seq int, formatMarks int) {
	target, err := resolveTargetDocument(ctx, t.workspace, t.sessionID, ref, true)
	if err != nil {
		return nil, nil, 0, 0
	}
	doc, err := docxedit.Open(content)
	if err != nil {
		return nil, nil, 0, 0
	}
	vdoc := newVirtualDoc(doc.Paragraphs())
	ops = formatMarkOps(vdoc, flagSpots(flags))
	formatMarks = len(ops)
	if sp != nil {
		ops = mergeMarkOps(ops, spellingMarkOps(vdoc, sp.findings))
	}
	if len(ops) == 0 {
		return nil, nil, 0, 0
	}
	tenantID, _ := types.TenantIDFromContext(ctx)
	// the undo point (and, for a Word document, the upload the next tool waits for)
	rev, err := t.workspace.Snapshot(ctx, tenantID, t.sessionID, target.ID,
		"ai: đánh dấu lỗi thể thức", types.DocumentRevisionSourceAI, snapshotWait)
	if err != nil {
		logger.Warnf(ctx, "check_document_format: no snapshot before marking, findings not marked: %v", err)
		return nil, nil, 0, 0
	}
	if rev != nil {
		seq = rev.Seq
	}
	return ops, target, seq, formatMarks
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
			Skills: report.Skills, Evaluated: evaluated, Evaluation: report.Evaluation, At: time.Now(),
		}
		if report.Segmentation != nil {
			r.Method = report.Segmentation.Method
		}
		rs, _ := docformat.RuleSetForReport(report)
		r.Flags = formatFlags(report, rs)
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

// Prewarm runs the check of the bound document (see ForDocument) in the
// background, once per document, so a later format question is answered
// from the cache; its progress is reported by SessionFormatCheck under the
// document's ID. It returns at once.
func (t *CheckDocumentFormatTool) Prewarm(ctx context.Context) {
	if t.chatModel == nil || t.sessionID == "" || t.documentID == "" {
		return
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		return
	}
	if _, done := formatChecks.prewarmed.LoadOrStore(t.documentID, struct{}{}); done {
		return
	}
	// checked before a restart (state kept in Redis), or running elsewhere
	if SessionFormatCheck(ctx, t.documentID) != nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	go func() {
		content, fileName, revision, err := t.source(ctx, tenantID, t.documentID)
		if err != nil {
			formatChecks.prewarmed.Delete(t.documentID)
			return
		}
		t.runBackground(ctx, content, fileName, revision)
	}()
}

// runBackground checks content and records its progress and result as the
// bound document's format-check state, after waiting for a format-check
// slot (see formatCheckSlots).
func (t *CheckDocumentFormatTool) runBackground(ctx context.Context, content []byte, fileName string, revision int) {
	readAt := time.Now()
	state := &types.DocumentFormatCheck{
		Revision: revision, StartedAt: readAt, CheckedSavedAt: &readAt, Fingerprint: formatFingerprint(content),
	}
	release, free := tryFormatCheckSlot()
	if !free {
		// Queued: hold no private copy of the file (up to 30 MB) for the
		// minutes the wait can take; read it again once a slot is free —
		// from workspaceDocs, so usually without a download — which also
		// checks a save made while waiting.
		key := formatCheckKey(content, t.modelName(), "")
		content = nil
		var ok bool
		if release, ok = t.waitFormatCheckSlot(ctx, key, state); !ok {
			return
		}
		tenantID, _ := types.TenantIDFromContext(ctx)
		var err error
		content, fileName, revision, err = t.source(ctx, tenantID, t.documentID)
		if err != nil {
			if release != nil {
				release()
			}
			failed := time.Now()
			state.Status, state.FinishedAt = types.DocumentFormatCheckFailed, &failed
			formatChecks.storeState(ctx, t.documentID, state)
			logger.Warnf(ctx, "check_document_format: background check of document %s could not read it: %v", t.documentID, err)
			return
		}
		readAt = time.Now()
		state.Revision, state.CheckedSavedAt, state.Fingerprint = revision, &readAt, formatFingerprint(content)
	}
	if release != nil {
		defer release()
	}
	if t.profiler != nil && t.documentID != "" {
		t.profiler.ensureFirst(ctx, t.documentID)
	}
	started := time.Now()
	state.Status, state.StartedAt = types.DocumentFormatCheckRunning, started
	formatChecks.storeState(ctx, t.documentID, state)
	out := t.check(ctx, content, fileName, "")
	done := *state
	finished := time.Now()
	done.FinishedAt = &finished
	if out.result == nil || !out.result.Evaluated {
		done.Status = types.DocumentFormatCheckFailed
		formatChecks.storeState(ctx, t.documentID, &done)
		logger.Warnf(ctx, "check_document_format: background check of %s failed: %s", fileName, out.failure)
		return
	}
	done.Status = types.DocumentFormatCheckReady
	// the result shown by the chat's format-check view
	formatChecks.put(ctx, out.result, formatCheckDocumentKey(t.documentID))
	if info := out.result.DocumentType; info != nil {
		done.DocumentType = info.Used
		done.DocumentTypeLabel = documentTypeLabel(info.RuleSet)
	}
	formatChecks.storeState(ctx, t.documentID, &done)
	logger.Infof(ctx, "check_document_format: background check of %s ready in %s", fileName, finished.Sub(started).Round(time.Second))
}

// A format-relevant edit is checked again once the document has stayed
// unsaved for formatRecheckQuiet (the user or the assistant may still be
// editing), waiting at most formatRecheckWaitMax.
var (
	formatRecheckQuiet   = 30 * time.Second
	formatRecheckWaitMax = 5 * time.Minute
	formatRecheckPoll    = 5 * time.Second
)

// formatRechecking holds the documents whose re-check is under way here.
var formatRechecking sync.Map

// FormatCheckNeedsRecheck reports whether the document (workspace ID) was
// saved after its finished background check, so Recheck has work to do. It
// reads only the state, so callers can test it before resolving a model.
func FormatCheckNeedsRecheck(ctx context.Context, documentID string, lastSavedAt *time.Time) bool {
	if lastSavedAt == nil {
		return false
	}
	st := SessionFormatCheck(ctx, documentID)
	if st == nil || st.InProgress() {
		return false
	}
	covered := st.StartedAt
	if st.CheckedSavedAt != nil {
		covered = *st.CheckedSavedAt
	}
	return lastSavedAt.After(covered)
}

// Recheck follows a save of the document after its background check: when
// the format fingerprint is unchanged (only body wording changed) the
// result is kept and marked as covering the save; otherwise the check runs
// again once editing settles. It returns at once.
func (t *CheckDocumentFormatTool) Recheck(ctx context.Context) {
	if t.chatModel == nil || t.workspace == nil || t.sessionID == "" || t.documentID == "" {
		return
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		return
	}
	ws, err := t.workspace.Get(ctx, tenantID, t.sessionID, t.documentID)
	if err != nil || ws == nil || !FormatCheckNeedsRecheck(ctx, t.documentID, ws.LastSavedAt) {
		return
	}
	if _, busy := formatRechecking.LoadOrStore(t.documentID, struct{}{}); busy {
		return
	}
	prev := SessionFormatCheck(ctx, t.documentID)
	savedAt := *ws.LastSavedAt
	ctx = context.WithoutCancel(ctx)
	go func() {
		defer formatRechecking.Delete(t.documentID)
		content, _, revision, err := t.source(ctx, tenantID, t.documentID)
		if err != nil || prev == nil {
			return
		}
		if prev.Fingerprint != "" && formatFingerprint(content) == prev.Fingerprint {
			kept := *prev
			kept.CheckedSavedAt = &savedAt
			kept.Revision = revision
			formatChecks.storeState(ctx, t.documentID, &kept)
			// keep the shown result as long as the state that points to it
			docKey := formatCheckDocumentKey(t.documentID)
			if r := formatChecks.get(ctx, docKey); r != nil {
				formatChecks.put(ctx, r, docKey)
			}
			logger.Infof(ctx, "check_document_format: save of document %s kept the format; evaluation still current", t.documentID)
			return
		}
		// show progress at once, then wait for the editing to settle
		now := time.Now()
		formatChecks.storeState(ctx, t.documentID, &types.DocumentFormatCheck{
			Status: types.DocumentFormatCheckRunning, Revision: revision, StartedAt: now, CheckedSavedAt: &now,
		})
		for deadline := now.Add(formatRecheckWaitMax); time.Now().Before(deadline); {
			cur, err := t.workspace.Get(ctx, tenantID, t.sessionID, t.documentID)
			if err != nil || cur == nil || cur.LastSavedAt == nil || time.Since(*cur.LastSavedAt) >= formatRecheckQuiet {
				break
			}
			time.Sleep(formatRecheckPoll)
		}
		content, fileName, revision, err := t.source(ctx, tenantID, t.documentID)
		if err != nil {
			failed := time.Now()
			formatChecks.storeState(ctx, t.documentID, &types.DocumentFormatCheck{
				Status: types.DocumentFormatCheckFailed, Revision: revision, StartedAt: now, FinishedAt: &failed, CheckedSavedAt: &now,
			})
			return
		}
		logger.Infof(ctx, "check_document_format: format of document %s changed; checking again", t.documentID)
		t.runBackground(ctx, content, fileName, revision)
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
