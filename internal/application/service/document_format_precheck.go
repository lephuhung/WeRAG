package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// DocumentFormatPrecheck serves the document assistant's work outside the
// chat: each document's profile (in the background), the live spellcheck
// of what the user types, and the state of a format check. The NĐ30 format
// check itself runs only when the user asks in the chat.
type DocumentFormatPrecheck struct {
	workspaces interfaces.DocumentWorkspaceService
	agents     interfaces.CustomAgentService
	models     interfaces.ModelService
	cfg        *config.Config
	// profiling holds the documents whose profile job is being handed over
	// (model resolution), for the same reason.
	profiling sync.Map
	// spelling: the live spellcheck's verifier and the users' verdicts
	spelling spellingSupport
	// formatFeedback stores the users' verdicts on format check reports
	formatFeedback interfaces.DocumentFormatFeedbackRepository
}

// NewDocumentFormatPrecheck wires the background format check.
func NewDocumentFormatPrecheck(
	workspaces interfaces.DocumentWorkspaceService,
	agents interfaces.CustomAgentService,
	models interfaces.ModelService,
	cfg *config.Config,
) *DocumentFormatPrecheck {
	p := &DocumentFormatPrecheck{workspaces: workspaces, agents: agents, models: models, cfg: cfg}
	// a chat upload's profile starts once its text is stored
	if n, ok := workspaces.(interface {
		OnSourceTextReady(func(context.Context, *types.DocumentWorkspace))
	}); ok {
		n.OnSourceTextReady(p.StartProfile)
	}
	return p
}

// ReviewSpelling reviews paragraphs the user typed in a target document
// (the live spellcheck, see tools.CheckSpellingTool.LiveMarks) with the
// document assistant's spellcheck model, and returns the editor ops that
// underline the mistakes. missed are the paragraphs (positions in paras)
// the model did not answer; off is true when the live spellcheck does not
// apply (not an open target, or an agent that may not check spelling), so
// the editor stops asking rather than treating its paragraphs as reviewed.
func (p *DocumentFormatPrecheck) ReviewSpelling(
	ctx context.Context, tenantID uint64, sessionID, documentID string, paras []tools.LiveParagraph,
) (ops []tools.DocumentOp, found int, missed []int, off bool, err error) {
	if p == nil || p.workspaces == nil || !p.workspaces.DocumentsEnabled() {
		return nil, 0, nil, false, errors.New("documents are disabled")
	}
	ws, err := p.workspaces.Get(ctx, tenantID, sessionID, documentID)
	if err != nil {
		return nil, 0, nil, false, err
	}
	if !ws.IsTarget() || ws.Status != types.DocumentWorkspaceStatusOpen {
		return nil, 0, nil, true, nil
	}
	if len(paras) == 0 {
		return nil, 0, nil, false, nil
	}
	agent := p.agent(ctx)
	if agent == nil || !slices.Contains(agent.Config.AllowedTools, tools.ToolCheckSpelling) {
		return nil, 0, nil, true, nil
	}
	spellModel := p.model(ctx, "spellcheck", agent.Config.SpellcheckModelID, agent.Config.ModelID)
	if spellModel == nil {
		return nil, 0, allPositions(len(paras)), false, nil
	}
	ops, found, missed = p.spelling.apply(ctx, tools.NewCheckSpellingTool(p.workspaces, spellModel, sessionID).
		WithPrompt(p.spellcheckPrompt())).ForDocument(documentID).LiveMarks(ctx, paras)
	return ops, found, missed, false, nil
}

// SpellingVerdict is the user's verdict on one underlined mistake of the
// live spellcheck (the spelling list's "not a mistake").
type SpellingVerdict struct {
	Paragraph      string
	Wrong          string
	Suggestion     string
	TextOccurrence int
}

// RecordSpellingVerdict stores that an underlined text of a document the
// user owns is no mistake: it is kept as training data and not underlined
// again in a paragraph with the same text.
func (p *DocumentFormatPrecheck) RecordSpellingVerdict(
	ctx context.Context, tenantID uint64, sessionID, documentID string, v SpellingVerdict,
) error {
	if p == nil || p.workspaces == nil || !p.workspaces.DocumentsEnabled() {
		return errors.New("documents are disabled")
	}
	if p.spelling.feedback == nil {
		return errors.New("spelling feedback is not available")
	}
	ws, err := p.workspaces.Get(ctx, tenantID, sessionID, documentID)
	if err != nil {
		return err
	}
	userID, _ := types.UserIDFromContext(ctx)
	row := types.DocumentSpellingFeedback{
		TenantID:       tenantID,
		UserID:         userID,
		SessionID:      sessionID,
		DocumentID:     ws.ID,
		Paragraph:      v.Paragraph,
		ParagraphHash:  tools.SpellingParagraphHash(v.Paragraph),
		Wrong:          v.Wrong,
		Suggestion:     v.Suggestion,
		TextOccurrence: max(1, v.TextOccurrence),
		Verdict:        types.SpellingVerdictNotError,
		Source:         types.SpellingFeedbackSourceUser,
	}
	if agent := p.agent(ctx); agent != nil {
		if m := p.model(ctx, "spellcheck", agent.Config.SpellcheckModelID, agent.Config.ModelID); m != nil {
			row.Model = m.GetModelName()
		}
	}
	return p.spelling.feedback.Record(ctx, []types.DocumentSpellingFeedback{row})
}

// SetFormatFeedback wires the store of the users' verdicts on format check
// reports.
func (p *DocumentFormatPrecheck) SetFormatFeedback(repo interfaces.DocumentFormatFeedbackRepository) {
	p.formatFeedback = repo
}

// FormatVerdict is the user's feedback on a format check report: the whole
// report (FindingID "") or one finding of it.
type FormatVerdict struct {
	ReportID  string
	FindingID string
	Verdict   string
	Note      string
}

// ErrFormatReportStale: the feedback names a report that is no longer the
// document's current one (checked again since).
var ErrFormatReportStale = errors.New("the format check report changed")

// ErrFormatFindingUnknown: the feedback names a finding the report has not.
var ErrFormatFindingUnknown = errors.New("no such finding in the report")

// RecordFormatVerdict stores the user's feedback on the current format
// check report of a document, with the report and the checked file's ref,
// as labelled data for tuning the checker. Nothing is rechecked or changed.
func (p *DocumentFormatPrecheck) RecordFormatVerdict(
	ctx context.Context, tenantID uint64, sessionID, documentID string, v FormatVerdict,
) error {
	if p == nil || p.workspaces == nil || !p.workspaces.DocumentsEnabled() {
		return errors.New("documents are disabled")
	}
	if p.formatFeedback == nil {
		return errors.New("format feedback is not available")
	}
	ws, err := p.workspaces.Get(ctx, tenantID, sessionID, documentID)
	if err != nil {
		return err
	}
	report := p.Report(ctx, ws.ID)
	if report == nil || report.ReportID == "" || report.ReportID != v.ReportID {
		return ErrFormatReportStale
	}
	userID, _ := types.UserIDFromContext(ctx)
	row := &types.DocumentFormatFeedback{
		TenantID: tenantID, UserID: userID, SessionID: sessionID, DocumentID: ws.ID,
		FileName: ws.FileName, DocumentRef: ws.CurrentRef, DocumentRevision: report.DocumentRevision,
		ReportID: report.ReportID, DocumentType: report.DocumentType, Scope: report.Scope,
		FindingID: v.FindingID, Verdict: v.Verdict, Note: v.Note,
	}
	if v.FindingID != "" {
		i := slices.IndexFunc(report.Findings, func(f types.DocumentFormatFinding) bool { return f.ID == v.FindingID })
		if i < 0 {
			return ErrFormatFindingUnknown
		}
		row.CheckID, row.FindingTitle = report.Findings[i].CheckID, report.Findings[i].Title
	}
	if raw, err := json.Marshal(report); err == nil {
		row.Report = raw
	}
	if agent := p.agent(ctx); agent != nil {
		if m := p.chatModel(ctx, agent); m != nil {
			row.Model = m.GetModelName()
		}
	}
	return p.formatFeedback.Record(ctx, row)
}

func allPositions(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

// spellcheckPrompt is the configured spellcheck_review template, "" when
// none (the tool then uses its built-in prompt).
func (p *DocumentFormatPrecheck) spellcheckPrompt() string {
	if p.cfg == nil || p.cfg.PromptTemplates == nil {
		return ""
	}
	if t := config.DefaultTemplate(p.cfg.PromptTemplates.SpellcheckReview); t != nil {
		return t.Content
	}
	return ""
}

// profiler is the session's document profiler with the knowledge-base
// profile's reconciliation of số hiệu and type.
func (p *DocumentFormatPrecheck) profiler(sessionID string, chatModel chat.Chat) *tools.DocumentProfiler {
	return tools.NewDocumentProfiler(p.workspaces, chatModel, sessionID).WithIdentity(applyLegalIdentity)
}

// StartProfile makes sure a document of either role has a profile: it
// queues one when none exists (or a source's text changed, or the role
// changed), and for a target edited since its profile plans the refresh
// (a fixed docProfileRefreshDelay after the first save it does not cover;
// see tools.ScheduleDocumentProfileRefresh). Cheap when nothing is due: it
// reads only the profile state. It returns at once.
func (p *DocumentFormatPrecheck) StartProfile(ctx context.Context, ws *types.DocumentWorkspace) {
	if p == nil || p.workspaces == nil || ws == nil || ws.ID == "" || ws.TenantID == 0 {
		return
	}
	start, refresh := tools.DocumentProfileNeed(ctx, ws)
	if !start && !refresh {
		return
	}
	ctx = context.WithValue(logger.CloneContext(context.WithoutCancel(ctx)), types.TenantIDContextKey, ws.TenantID)
	if refresh {
		sessionID, documentID := ws.SessionID, ws.ID
		tools.ScheduleDocumentProfileRefresh(ctx, ws, func() {
			p.profiler(sessionID, p.chatModel(ctx, p.agent(ctx))).Refresh(ctx, documentID)
		})
		return
	}
	if _, busy := p.profiling.LoadOrStore(ws.ID, struct{}{}); busy {
		return
	}
	row := *ws
	go func() {
		defer p.profiling.Delete(row.ID)
		// no model: the profile fails with "no model", shown as such
		p.profiler(row.SessionID, p.chatModel(ctx, p.agent(ctx))).Start(ctx, &row)
	}()
}

// StopProfile cancels a document's planned profile refresh (its tab was
// closed or it became a source); the last profile is kept.
func (p *DocumentFormatPrecheck) StopProfile(documentID string) {
	if p == nil {
		return
	}
	tools.CancelDocumentProfileRefresh(documentID)
}

// Profile reports a document's profile for the API (no hash; stale when
// the document was edited since), or nil when none was made.
func (p *DocumentFormatPrecheck) Profile(ctx context.Context, ws *types.DocumentWorkspace) *types.DocumentProfile {
	if p == nil || ws == nil {
		return nil
	}
	return tools.SessionDocumentProfile(ctx, ws.ID).Public(ws)
}

// Status reports a document's background check, or nil when none ran.
func (p *DocumentFormatPrecheck) Status(ctx context.Context, documentID string) *types.DocumentFormatCheck {
	if p == nil {
		return nil
	}
	return tools.SessionFormatCheck(ctx, documentID)
}

// StartFormatCheck runs the NĐ30 format check of an open target (workspace
// ID) in the background, as the user asked from the editor; the chat is
// not held for it. The editor is saved first (the Word add-in uploads the
// file itself before asking). It returns the check's state at once —
// queued, or the one already under way — and an error when the document
// cannot be checked.
func (p *DocumentFormatPrecheck) StartFormatCheck(ctx context.Context, tenantID uint64, sessionID, documentID string) (*types.DocumentFormatCheck, error) {
	if p == nil || p.workspaces == nil || !p.workspaces.DocumentsEnabled() {
		return nil, errors.New("documents are disabled")
	}
	ws, err := p.workspaces.Get(ctx, tenantID, sessionID, documentID)
	if err != nil {
		return nil, err
	}
	if !ws.IsTarget() || ws.Status != types.DocumentWorkspaceStatusOpen {
		return nil, apperrors.NewBadRequestError("only an open working document can be format-checked")
	}
	agent := p.agent(ctx)
	if agent == nil || !slices.Contains(agent.Config.AllowedTools, tools.ToolCheckDocumentFormat) {
		return nil, apperrors.NewBadRequestError("the document assistant cannot check the format")
	}
	chatModel := p.chatModel(ctx, agent)
	if chatModel == nil {
		return nil, apperrors.NewServiceUnavailableError("no model for the format check")
	}
	ctx = context.WithValue(logger.CloneContext(ctx), types.TenantIDContextKey, tenantID)
	var save func(context.Context) error
	if !ws.IsWordAddin() {
		save = func(ctx context.Context) error {
			_, err := p.workspaces.Snapshot(ctx, tenantID, sessionID, documentID,
				"ai: kiểm tra thể thức", types.DocumentRevisionSourceAI, formatCheckSaveWait)
			return err
		}
	}
	st := tools.NewCheckDocumentFormatToolForWorkspace(p.workspaces, chatModel, sessionID).ForDocument(documentID).
		WithProfiler(p.profiler(sessionID, chatModel)).StartBackground(ctx, save)
	if st == nil {
		return nil, apperrors.NewServiceUnavailableError("the format check could not start")
	}
	return st, nil
}

// FormatFindingMarks returns the underlines of an open target's finished
// format check (one per measured finding that names its paragraph), for
// the editor to show; nothing in the document is changed or saved. An
// error tells the user why there is nothing to mark (no result kept, or
// the document changed since the check).
func (p *DocumentFormatPrecheck) FormatFindingMarks(ctx context.Context, tenantID uint64, sessionID, documentID string) ([]tools.DocumentOp, error) {
	if p == nil || p.workspaces == nil || !p.workspaces.DocumentsEnabled() {
		return nil, errors.New("documents are disabled")
	}
	ws, err := p.workspaces.Get(ctx, tenantID, sessionID, documentID)
	if err != nil {
		return nil, err
	}
	if !ws.IsTarget() {
		return nil, apperrors.NewBadRequestError("only a working document can be marked")
	}
	chatModel := p.chatModel(ctx, p.agent(ctx))
	if chatModel == nil {
		return nil, apperrors.NewServiceUnavailableError("no model for the format check")
	}
	ctx = context.WithValue(ctx, types.TenantIDContextKey, tenantID)
	ops, kept, err := tools.FormatFindingMarks(ctx, p.workspaces, sessionID, documentID, chatModel.GetModelName())
	switch {
	case errors.Is(err, tools.ErrFormatCheckOutdated):
		return nil, apperrors.NewConflictError(err.Error())
	case err != nil:
		return nil, err
	case !kept:
		return nil, apperrors.NewNotFoundError("chưa có kết quả kiểm tra thể thức cho văn bản này")
	}
	return ops, nil
}

// formatCheckSaveWait bounds the editor save before a requested check.
const formatCheckSaveWait = 20 * time.Second

// Report returns the evaluation of a document's finished background check,
// or nil when none is kept.
func (p *DocumentFormatPrecheck) Report(ctx context.Context, documentID string) *types.DocumentFormatReport {
	if p == nil {
		return nil
	}
	return tools.SessionFormatCheckReport(ctx, documentID)
}

// agent loads the document assistant's (tenant) configuration.
func (p *DocumentFormatPrecheck) agent(ctx context.Context) *types.CustomAgent {
	if p.agents == nil {
		return nil
	}
	agent, err := p.agents.GetAgentByID(ctx, types.BuiltinDocumentAssistantID)
	if err != nil || agent == nil {
		logger.Warnf(ctx, "[DocumentFormatPrecheck] document assistant not found: %v", err)
		return nil
	}
	return agent
}

// chatModel resolves the model of the document assistant's profile:
// format_check_model_id, else its chat model.
func (p *DocumentFormatPrecheck) chatModel(ctx context.Context, agent *types.CustomAgent) chat.Chat {
	if agent == nil {
		return nil
	}
	return p.model(ctx, "format check", agent.Config.FormatCheckModelID, agent.Config.ModelID)
}

// model loads the first usable chat model of modelIDs (a dedicated model
// that cannot be loaded falls back to the next one).
func (p *DocumentFormatPrecheck) model(ctx context.Context, purpose string, modelIDs ...string) chat.Chat {
	if p.models == nil {
		return nil
	}
	for _, modelID := range modelIDs {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			continue
		}
		m, err := p.models.GetChatModel(ctx, modelID)
		if err == nil && m != nil {
			return m
		}
		logger.Warnf(ctx, "[DocumentFormatPrecheck] %s model %s unavailable: %v", purpose, modelID, err)
	}
	logger.Warnf(ctx, "[DocumentFormatPrecheck] document assistant has no usable %s model; skipping", purpose)
	return nil
}
