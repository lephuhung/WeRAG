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

// DocumentFormatPrecheck runs the NĐ30 format check of a document-assistant
// session's document as soon as it is opened, with the document assistant's
// chat model, so the chat can tell the user the evaluation is ready before
// they ask. The result lands in the check_document_format cache the agent
// tool reads.
type DocumentFormatPrecheck struct {
	workspaces interfaces.DocumentWorkspaceService
	agents     interfaces.CustomAgentService
	models     interfaces.ModelService
	// started holds the sessions already handed to a check, so the
	// editor's polling does not resolve the model again and again.
	started sync.Map
}

// NewDocumentFormatPrecheck wires the background format check.
func NewDocumentFormatPrecheck(
	workspaces interfaces.DocumentWorkspaceService,
	agents interfaces.CustomAgentService,
	models interfaces.ModelService,
) *DocumentFormatPrecheck {
	return &DocumentFormatPrecheck{workspaces: workspaces, agents: agents, models: models}
}

// Start begins the check of the session's document in the background, at
// most once per session in this process. It returns at once.
func (p *DocumentFormatPrecheck) Start(ctx context.Context, tenantID uint64, sessionID string) {
	if p == nil || p.workspaces == nil || !p.workspaces.Enabled() || tenantID == 0 || sessionID == "" {
		return
	}
	if tools.SessionFormatCheck(ctx, sessionID) != nil {
		return
	}
	if _, done := p.started.LoadOrStore(sessionID, struct{}{}); done {
		return
	}
	ctx = context.WithValue(logger.CloneContext(context.WithoutCancel(ctx)), types.TenantIDContextKey, tenantID)
	go func() {
		agent := p.agent(ctx)
		if agent == nil {
			return
		}
		if !agent.Config.FormatCheckOnOpenEnabled() {
			logger.Infof(ctx, "[DocumentFormatPrecheck] format_check_on_open is off; not checking session %s", sessionID)
			return
		}
		chatModel := p.chatModel(ctx, agent)
		if chatModel == nil {
			return
		}
		tools.NewCheckDocumentFormatToolForWorkspace(p.workspaces, chatModel, sessionID).Prewarm(ctx)
	}()
}

// Refresh follows a save of the document made after its background check:
// an edit that left the format fingerprint unchanged keeps the result,
// another one checks again once editing settles (see tools Recheck). Cheap
// when nothing was saved since: it reads only the check state.
func (p *DocumentFormatPrecheck) Refresh(ctx context.Context, ws *types.DocumentWorkspace) {
	if p == nil || ws == nil || ws.Status != types.DocumentWorkspaceStatusOpen ||
		!tools.FormatCheckNeedsRecheck(ctx, ws.SessionID, ws.LastSavedAt) {
		return
	}
	ctx = context.WithValue(logger.CloneContext(context.WithoutCancel(ctx)), types.TenantIDContextKey, ws.TenantID)
	go func() {
		chatModel := p.chatModel(ctx, p.agent(ctx))
		if chatModel == nil {
			return
		}
		tools.NewCheckDocumentFormatToolForWorkspace(p.workspaces, chatModel, ws.SessionID).Recheck(ctx)
	}()
}

// Status reports the session's background check, or nil when none ran.
func (p *DocumentFormatPrecheck) Status(ctx context.Context, sessionID string) *types.DocumentFormatCheck {
	if p == nil {
		return nil
	}
	return tools.SessionFormatCheck(ctx, sessionID)
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
