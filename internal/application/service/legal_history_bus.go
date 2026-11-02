package service

import (
	"context"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
)

// legalHistorySource is implemented by the agent service, which holds the
// database the amendment history reads.
type legalHistorySource interface {
	legalHistoryFunc(kbIDs []string) func(ctx context.Context, chunkIDs []string, answer string) string
}

// legalHistoryFunc returns the amendment-history lookup over the given KBs
// (resolver or previous path, as legalWordingFunc), or nil when the
// database cannot serve it.
func (s *agentService) legalHistoryFunc(
	kbIDs []string,
) func(ctx context.Context, chunkIDs []string, answer string) string {
	if s.db == nil || s.db.Dialector.Name() != "postgres" {
		return nil
	}
	db := s.db
	settings := s.legalSettings
	return func(ctx context.Context, chunkIDs []string, answer string) string {
		now := types.PromptNow()
		history := legalHistoryChoice(ctx, legalResolverOn(ctx, settings),
			func() (string, error) { return LegalAmendmentHistoryResolved(ctx, db, kbIDs, chunkIDs, answer, now) },
			func() string { return LegalAmendmentHistory(ctx, db, kbIDs, chunkIDs, answer, now) })
		return history + LegalValidityNote(ctx, db, answer)
	}
}

// legalWordingFunc returns the current-wording lookup over the given KBs
// (LegalCurrentWordingResolved, or LegalCurrentWording when the switch
// LegalWordingResolverSetting is off or the resolver fails), or nil when
// the database cannot serve it. The switch is read on every call.
func (s *agentService) legalWordingFunc(kbIDs []string) func(ctx context.Context, chunkIDs []string) map[string]string {
	if s.db == nil || s.db.Dialector.Name() != "postgres" || len(kbIDs) == 0 {
		return nil
	}
	db := s.db
	settings := s.legalSettings
	return func(ctx context.Context, chunkIDs []string) map[string]string {
		now := types.PromptNow()
		return legalWordingChoice(ctx, legalResolverOn(ctx, settings),
			func() (map[string]string, error) { return LegalCurrentWordingResolved(ctx, db, kbIDs, chunkIDs, now) },
			func() map[string]string { return LegalCurrentWording(ctx, db, kbIDs, chunkIDs, now) })
	}
}

// legalHistoryBus appends the "Lịch sử sửa đổi" block to the quick answer:
// it collects each final-answer stream and, when the stream ends, emits
// the block on the same answer id before the terminal event, so the stored
// message carries it too. Fallback answers (no retrieval match) pass as is.
type legalHistoryBus struct {
	types.EventBusInterface
	history  func(ctx context.Context, chunkIDs []string, answer string) string
	chunkIDs func() []string

	mu      sync.Mutex
	answers map[string]*strings.Builder
}

func newLegalHistoryBus(
	inner types.EventBusInterface,
	history func(ctx context.Context, chunkIDs []string, answer string) string,
	chunkIDs func() []string,
) types.EventBusInterface {
	if inner == nil || history == nil {
		return inner
	}
	return &legalHistoryBus{
		EventBusInterface: inner, history: history, chunkIDs: chunkIDs,
		answers: map[string]*strings.Builder{},
	}
}

func (b *legalHistoryBus) Emit(ctx context.Context, evt types.Event) error {
	data, ok := evt.Data.(event.AgentFinalAnswerData)
	if evt.Type != types.EventType(event.EventAgentFinalAnswer) || !ok || data.IsFallback {
		return b.EventBusInterface.Emit(ctx, evt)
	}
	b.mu.Lock()
	acc := b.answers[evt.ID]
	if acc == nil {
		acc = &strings.Builder{}
		b.answers[evt.ID] = acc
	}
	acc.WriteString(data.Content)
	answer := acc.String()
	if data.Done {
		delete(b.answers, evt.ID)
	}
	b.mu.Unlock()
	if !data.Done || strings.TrimSpace(answer) == "" || ctx.Err() != nil {
		return b.EventBusInterface.Emit(ctx, evt)
	}
	// no chunks still gets the validity note
	block := b.history(ctx, b.chunkIDs(), answer)
	if block == "" {
		return b.EventBusInterface.Emit(ctx, evt)
	}
	emit := func(d event.AgentFinalAnswerData) {
		e := evt
		e.Data = d
		_ = b.EventBusInterface.Emit(ctx, e)
	}
	if data.Content != "" {
		emit(event.AgentFinalAnswerData{Content: data.Content})
	}
	emit(event.AgentFinalAnswerData{Content: block})
	data.Content = ""
	e := evt
	e.Data = data
	return b.EventBusInterface.Emit(ctx, e)
}

// mergedChunkIDs lists the chunk ids of the pipeline's merged retrieval
// results, the context the quick answer was written from.
func mergedChunkIDs(chatManage *types.ChatManage) func() []string {
	return func() []string {
		seen := map[string]bool{}
		var ids []string
		for _, r := range chatManage.MergeResult {
			if r != nil && r.ID != "" && !seen[r.ID] {
				seen[r.ID] = true
				ids = append(ids, r.ID)
			}
		}
		return ids
	}
}

// searchTargetKBIDs lists the knowledge bases a quick answer searches,
// including those of individually selected documents.
func searchTargetKBIDs(targets types.SearchTargets) []string {
	seen := map[string]bool{}
	var ids []string
	for _, t := range targets {
		if t != nil && t.KnowledgeBaseID != "" && !seen[t.KnowledgeBaseID] {
			seen[t.KnowledgeBaseID] = true
			ids = append(ids, t.KnowledgeBaseID)
		}
	}
	return ids
}
