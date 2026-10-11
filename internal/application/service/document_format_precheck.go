package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/config"
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
// underline the mistakes in red. ok is false when the model answered
// nothing. An agent that may not check spelling gets no ops.
func (p *DocumentFormatPrecheck) ReviewSpelling(
	ctx context.Context, tenantID uint64, sessionID, documentID string, paras []tools.LiveParagraph,
) (ops []tools.DocumentOp, found int, ok bool, err error) {
	if p == nil || p.workspaces == nil || !p.workspaces.DocumentsEnabled() {
		return nil, 0, false, errors.New("documents are disabled")
	}
	ws, err := p.workspaces.Get(ctx, tenantID, sessionID, documentID)
	if err != nil {
		return nil, 0, false, err
	}
	if !ws.IsTarget() || ws.Status != types.DocumentWorkspaceStatusOpen || len(paras) == 0 {
		return nil, 0, true, nil
	}
	agent := p.agent(ctx)
	if agent == nil || !slices.Contains(agent.Config.AllowedTools, tools.ToolCheckSpelling) {
		return nil, 0, true, nil
	}
	spellModel := p.model(ctx, "spellcheck", agent.Config.SpellcheckModelID, agent.Config.ModelID)
	if spellModel == nil {
		return nil, 0, false, nil
	}
	ops, found, ok = tools.NewCheckSpellingTool(p.workspaces, spellModel, sessionID).
		WithPrompt(p.spellcheckPrompt()).LiveMarks(ctx, paras)
	return ops, found, ok, nil
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
