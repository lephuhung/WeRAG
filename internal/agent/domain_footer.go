package agent

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// An agent with domain packs ends every model-written answer with the packs'
// footer (disclaimer, overdue-review line). It is code-appended like the
// amendment history: a sentence only in SKILL.md is one Qwen drops
// (docs/domain-pack-standard.md §6).

// DomainFooterFunc returns the footer block for answer, "" when there is
// none. It is asked per answer so the overdue line follows the clock, and
// it leaves out footer text the answer already carries.
type DomainFooterFunc func(ctx context.Context, answer string) string

// SetDomainFooter installs the domain-pack footer.
func (e *AgentEngine) SetDomainFooter(fn DomainFooterFunc) {
	e.domainFooter = fn
}

// appendDomainFooter streams the footer as the answer's last block, on the
// answer's own event and before its Done, so the stored message carries it.
// Fixed answers written by code (no evidence, fallbacks) get none: they are
// not advice from the pack.
func (e *AgentEngine) appendDomainFooter(ctx context.Context, state *types.AgentState, sessionID, answerID string) {
	if e.domainFooter == nil || e.eventBus == nil || answerID == "" || state == nil ||
		strings.TrimSpace(state.FinalAnswer) == "" || strings.HasPrefix(state.FinalAnswer, ungroundedAnswer) {
		return
	}
	block := e.domainFooter(ctx, state.FinalAnswer)
	if block == "" {
		return
	}
	logger.Infof(ctx, "[Agent] Appending domain-pack footer (%d chars)", len(block))
	_ = e.eventBus.Emit(ctx, event.Event{
		ID:        answerID,
		Type:      event.EventAgentFinalAnswer,
		SessionID: sessionID,
		Data:      event.AgentFinalAnswerData{Content: block},
	})
	state.FinalAnswer += block
}
