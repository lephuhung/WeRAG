package service

import (
	"context"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// DocumentFormatPrecheck runs the NĐ30 format check of each document of a
// document-assistant session as soon as it is opened, with the document assistant's
// chat model, so the chat can tell the user the evaluation is ready before
// they ask. The result lands in the check_document_format cache the agent
// tool reads.
type DocumentFormatPrecheck struct {
	workspaces interfaces.DocumentWorkspaceService
	agents     interfaces.CustomAgentService
	models     interfaces.ModelService
	// started holds the documents already handed to a check, so the
	// editor's polling does not resolve the model again and again.
	started sync.Map
	// profiling holds the documents whose profile job is being handed over
	// (model resolution), for the same reason.
	profiling sync.Map
}

// NewDocumentFormatPrecheck wires the background format check.
func NewDocumentFormatPrecheck(
	workspaces interfaces.DocumentWorkspaceService,
	agents interfaces.CustomAgentService,
	models interfaces.ModelService,
) *DocumentFormatPrecheck {
	p := &DocumentFormatPrecheck{workspaces: workspaces, agents: agents, models: models}
	// a chat upload's profile starts once its text is stored
	if n, ok := workspaces.(interface {
		OnSourceTextReady(func(context.Context, *types.DocumentWorkspace))
	}); ok {
		n.OnSourceTextReady(p.StartProfile)
	}
	return p
}

// Start begins the check of one target (workspace ID) of the session in
// the background, at most once per document in this process; a source is
// skipped (it may be checked once promoted). It returns at once.
func (p *DocumentFormatPrecheck) Start(ctx context.Context, tenantID uint64, sessionID, documentID string) {
	if p == nil || p.workspaces == nil || !p.workspaces.DocumentsEnabled() || tenantID == 0 || sessionID == "" || documentID == "" {
		return
	}
	if tools.SessionFormatCheck(ctx, documentID) != nil {
		return
	}
	if _, done := p.started.LoadOrStore(documentID, struct{}{}); done {
		return
	}
	ctx = context.WithValue(logger.CloneContext(context.WithoutCancel(ctx)), types.TenantIDContextKey, tenantID)
	go func() {
		// a source (chat upload) is never format-checked
		if ws, err := p.workspaces.Get(ctx, tenantID, sessionID, documentID); err != nil || ws.IsSource() {
			p.started.Delete(documentID)
			return
		}
		agent := p.agent(ctx)
		if agent == nil {
			return
		}
		if !agent.Config.FormatCheckOnOpenEnabled() {
			logger.Infof(ctx, "[DocumentFormatPrecheck] format_check_on_open is off; not checking document %s", documentID)
			return
		}
		chatModel := p.chatModel(ctx, agent)
		if chatModel == nil {
			return
		}
		tools.NewCheckDocumentFormatToolForWorkspace(p.workspaces, chatModel, sessionID).ForDocument(documentID).
			WithProfiler(p.profiler(sessionID, chatModel)).Prewarm(ctx)
	}()
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

// Refresh follows a save of the document made after its background check:
// an edit that left the format fingerprint unchanged keeps the result,
// another one checks again once editing settles (see tools Recheck). Cheap
// when nothing was saved since: it reads only the check state.
func (p *DocumentFormatPrecheck) Refresh(ctx context.Context, ws *types.DocumentWorkspace) {
	if p == nil || ws == nil || ws.Status != types.DocumentWorkspaceStatusOpen ||
		!tools.FormatCheckNeedsRecheck(ctx, ws.ID, ws.LastSavedAt) {
		return
	}
	ctx = context.WithValue(logger.CloneContext(context.WithoutCancel(ctx)), types.TenantIDContextKey, ws.TenantID)
	go func() {
		chatModel := p.chatModel(ctx, p.agent(ctx))
		if chatModel == nil {
			return
		}
		tools.NewCheckDocumentFormatToolForWorkspace(p.workspaces, chatModel, ws.SessionID).ForDocument(ws.ID).Recheck(ctx)
	}()
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

// chatModel resolves the model of the document assistant's format check:
// format_check_model_id, else its chat model. A dedicated model that cannot
// be loaded falls back to the chat model.
func (p *DocumentFormatPrecheck) chatModel(ctx context.Context, agent *types.CustomAgent) chat.Chat {
	if agent == nil || p.models == nil {
		return nil
	}
	for _, modelID := range []string{strings.TrimSpace(agent.Config.FormatCheckModelID), strings.TrimSpace(agent.Config.ModelID)} {
		if modelID == "" {
			continue
		}
		m, err := p.models.GetChatModel(ctx, modelID)
		if err == nil && m != nil {
			return m
		}
		logger.Warnf(ctx, "[DocumentFormatPrecheck] chat model %s unavailable: %v", modelID, err)
	}
	logger.Warnf(ctx, "[DocumentFormatPrecheck] document assistant has no usable chat model; skipping")
	return nil
}
