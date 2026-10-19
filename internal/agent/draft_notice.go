package agent

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// DraftNoticeFunc returns the draft notice for the model's own text: one
// line per pack draft the text names in a paragraph that does not say
// "dự thảo" (domainpacks.Source.DraftNotice), "" when none. Qwen dropped the
// label in 46 % of eval answers although SKILL.md asks for it (2026-10-18),
// so the label is code (doc §4 layer d).
type DraftNoticeFunc func(ctx context.Context, modelText string) string

// SetDraftNotice installs the draft-notice check.
func (e *AgentEngine) SetDraftNotice(fn DraftNoticeFunc) {
	e.draftNotice = fn
}

// appendDraftNotice streams the notice after the model's answer and the
// engine notes, before the tool answer block and the footer, on the
// answer's own event. modelText is the answer as the model wrote it.
// Code-written answers ("no evidence") get none.
func (e *AgentEngine) appendDraftNotice(ctx context.Context, state *types.AgentState, sessionID, answerID, modelText string) {
	if e.draftNotice == nil || e.eventBus == nil || answerID == "" || state == nil ||
		strings.TrimSpace(modelText) == "" || strings.HasPrefix(modelText, ungroundedAnswer) {
		return
	}
	block := e.draftNotice(ctx, modelText)
	if block == "" {
		return
	}
	logger.Infof(ctx, "[Agent] Appending draft notice (%d chars)", len(block))
	_ = e.eventBus.Emit(ctx, event.Event{
		ID:        answerID,
		Type:      event.EventAgentFinalAnswer,
		SessionID: sessionID,
		Data:      event.AgentFinalAnswerData{Content: block},
	})
	state.FinalAnswer += block
}

// ScopeNoticeFunc returns the scope notice for the user's question: the
// pack exclusions (see a lawyer) and refusals it touches, "" when none
// (domainpacks.Source.ScopeNotice). Matched on the question in code, never
// left to the model.
type ScopeNoticeFunc func(ctx context.Context, question string) string

// SetScopeNotice installs the scope check.
func (e *AgentEngine) SetScopeNotice(fn ScopeNoticeFunc) {
	e.scopeNotice = fn
}

// appendScopeNotice streams the scope notice after the draft notice and
// before the tool answer block and the footer. Code-written answers get
// none.
func (e *AgentEngine) appendScopeNotice(ctx context.Context, state *types.AgentState, sessionID, answerID, question string) {
	if e.scopeNotice == nil || e.eventBus == nil || answerID == "" || state == nil ||
		strings.TrimSpace(state.FinalAnswer) == "" || strings.HasPrefix(state.FinalAnswer, ungroundedAnswer) {
		return
	}
	block := e.scopeNotice(ctx, question)
	if block == "" {
		return
	}
	logger.Infof(ctx, "[Agent] Appending scope notice (%d chars)", len(block))
	_ = e.eventBus.Emit(ctx, event.Event{
		ID:        answerID,
		Type:      event.EventAgentFinalAnswer,
		SessionID: sessionID,
		Data:      event.AgentFinalAnswerData{Content: block},
	})
	state.FinalAnswer += block
}
