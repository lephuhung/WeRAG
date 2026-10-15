package agent

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/compaction"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// SetContextCheckpointSink enables persisting compaction checkpoints. Nil (the
// default) keeps every compaction local to the turn that ran it.
func (e *AgentEngine) SetContextCheckpointSink(sink types.ContextCheckpointSink) {
	e.checkpointSink = sink
}

// SetCompactionModel has model write this run's compaction summaries (the
// system setting llm.compaction_model_id), sized by its own window, with the
// answering model kept as the fallback. The compaction threshold still
// follows the answering model's window. Nil keeps the answering model.
func (e *AgentEngine) SetCompactionModel(model chat.Chat, modelID string, window int) {
	if e == nil || e.compactor == nil || model == nil {
		return
	}
	e.compactor = e.compactor.WithSummarizer(model, modelID, window)
}

// saveContextCheckpoint writes a compaction that ends on a stored turn onto
// that turn. Best-effort: a failed write costs the next turn one more
// summarization, never this turn anything.
func (e *AgentEngine) saveContextCheckpoint(ctx context.Context, cp *compaction.Checkpoint, round int) {
	if cp == nil || e.checkpointSink == nil {
		return
	}
	err := e.checkpointSink.SaveContextCheckpoint(ctx, cp.TurnID, &types.ContextCheckpoint{
		Summary:   cp.Summary,
		CreatedAt: time.Now(),
		Degraded:  cp.Degraded,
	})
	if err != nil {
		logger.Warnf(ctx, "[Agent][Round-%d] Failed to save context checkpoint on turn %s: %v",
			round, cp.TurnID, err)
		return
	}
	if cp.Degraded {
		logger.Warnf(ctx, "[Agent][Round-%d] Saved a degraded context checkpoint through turn %s "+
			"(raw archive, %d chars): the summarizer failed; the next compaction refines it",
			round, cp.TurnID, len(cp.Summary))
		return
	}
	logger.Infof(ctx, "[Agent][Round-%d] Saved context checkpoint through turn %s (%d chars)",
		round, cp.TurnID, len(cp.Summary))
}
