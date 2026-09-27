package chatpipeline

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
)

// PluginAbbreviationResolve enforces the abbreviation gate inside the
// pipeline. Resolution is owned by the QA entry coordinator and sealed into
// the request context; this stage only verifies that the ready resolution
// carried by ChatManage still matches the sealed turn before downstream
// stages consume the query. It performs no dictionary I/O and emits no
// abbreviation tool events — model output can never mint or alter a
// resolution.
type PluginAbbreviationResolve struct{}

func NewPluginAbbreviationResolve(eventManager *EventManager) *PluginAbbreviationResolve {
	res := &PluginAbbreviationResolve{}
	eventManager.Register(res)
	return res
}

func (p *PluginAbbreviationResolve) ActivationEvents() []types.EventType {
	return []types.EventType{types.ABBREVIATION_RESOLVE}
}

func (p *PluginAbbreviationResolve) OnEvent(ctx context.Context,
	eventType types.EventType, chatManage *types.ChatManage, next func() *PluginError,
) *PluginError {
	r := chatManage.AbbreviationResolution
	if r == nil {
		return next()
	}
	if r.Status != types.AbbreviationStatusReady {
		pipelineError(ctx, "AbbreviationResolve", "abbreviation_not_ready", map[string]interface{}{
			"session_id": chatManage.SessionID,
			"status":     r.Status,
		})
		return ErrAbbreviationGate.WithError(types.ErrAbbreviationNotReady)
	}
	if b := chatManage.AbbreviationBinding; b.UserMessageID != "" {
		if err := abbreviation.RequireTurn(ctx, b); err != nil {
			pipelineError(ctx, "AbbreviationResolve", "abbreviation_turn_invalid", map[string]interface{}{
				"session_id": chatManage.SessionID,
				"error":      err.Error(),
			})
			return ErrAbbreviationGate.WithError(err)
		}
	}
	if strings.TrimSpace(chatManage.RewriteQuery) == "" {
		chatManage.RewriteQuery = r.EffectiveQuery
	}
	return next()
}
