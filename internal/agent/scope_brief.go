package agent

import (
	"context"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
)

// scopeWrapUpRound is the wrap-up round of a scope-briefed turn: an
// out-of-scope or refused question needs a referral and an orientation,
// not eight rounds of research. The brief asks for that in words; the
// earlier nudge holds the turn to it in code, since a prompt rule alone is
// not reliably followed by every model.
const scopeWrapUpRound = 3

// SetScopeBrief installs the per-turn scope brief
// (domainpacks.Source.ScopeBrief).
func (e *AgentEngine) SetScopeBrief(fn ScopeNoticeFunc) {
	e.scopeBrief = fn
}

// applyScopeBrief adds the scope brief of the user's question to the
// current user message (the last message) and moves the turn's wrap-up
// nudge to scopeWrapUpRound. The brief lives only in this turn's message
// slice: the stored user message and the history later turns rebuild hold
// the question alone, and the system prompt and tool list are untouched,
// so the cached prompt prefix of the turn is too.
func (e *AgentEngine) applyScopeBrief(ctx context.Context, question string, messages []chat.Message) {
	if e.scopeBrief == nil || len(messages) == 0 {
		return
	}
	last := &messages[len(messages)-1]
	if last.Role != "user" || len(last.MultiContent) > 0 {
		return
	}
	brief := e.scopeBrief(ctx, question)
	if brief == "" {
		return
	}
	last.Content += "\n\n" + brief
	e.wrapUpRound = scopeWrapUpRound
	logger.Infof(ctx, "[Agent] Scope brief added (%d chars); wrap-up after %d rounds", len(brief), scopeWrapUpRound)
}
