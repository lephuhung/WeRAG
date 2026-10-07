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
		chatModel := p.chatModel(ctx)
		if chatModel == nil {
			return
		}
		tools.NewCheckDocumentFormatToolForWorkspace(p.workspaces, chatModel, sessionID).Prewarm(ctx)
	}()
}

// Status reports the session's background check, or nil when none ran.
func (p *DocumentFormatPrecheck) Status(ctx context.Context, sessionID string) *types.DocumentFormatCheck {
	if p == nil {
		return nil
	}
	return tools.SessionFormatCheck(ctx, sessionID)
}

// chatModel resolves the document assistant's configured chat model.
func (p *DocumentFormatPrecheck) chatModel(ctx context.Context) chat.Chat {
	if p.agents == nil || p.models == nil {
		return nil
	}
	agent, err := p.agents.GetAgentByID(ctx, types.BuiltinDocumentAssistantID)
	if err != nil || agent == nil {
		logger.Warnf(ctx, "[DocumentFormatPrecheck] document assistant not found: %v", err)
		return nil
	}
	modelID := strings.TrimSpace(agent.Config.ModelID)
	if modelID == "" {
		logger.Warnf(ctx, "[DocumentFormatPrecheck] document assistant has no chat model; skipping")
		return nil
	}
	m, err := p.models.GetChatModel(ctx, modelID)
	if err != nil || m == nil {
		logger.Warnf(ctx, "[DocumentFormatPrecheck] chat model %s unavailable: %v", modelID, err)
		return nil
	}
	return m
}
