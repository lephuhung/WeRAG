package agent

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/agent/compaction"
	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
)

// CompactHistoryToBudget fits the stored history of a turn into budget
// before the engine runs, and returns what the turn should start from.
//
// The history budget is a separate knob from the model window: a plan can
// allow far less history per turn than the model could hold, because every
// round of the turn resends it. The loader still reads up to the window, so
// what lies beyond the budget reaches this summary instead of being dropped;
// here it is summarized synchronously — the same structured summary,
// checkpoint and context_compacted event as a mid-turn compaction — keeping
// HistoryKeepRecentTokens(budget) of the newest history verbatim.
//
// It runs one compaction at most. A history that is still over budget
// afterwards can only be one oversized stored turn inside the keep-recent
// tail; its tool outputs are trimmed rather than compacted again, which could
// free nothing more.
func (e *AgentEngine) CompactHistoryToBudget(
	ctx context.Context, history []chat.Message, budget int,
) []chat.Message {
	if e == nil || budget <= 0 || len(history) == 0 {
		return history
	}
	before := e.tokenEstimator.EstimateMessages(history)
	if before <= budget {
		return history
	}
	logger.Infof(ctx, "[Agent][History] Stored history at %d tokens, over the %d history budget; "+
		"compacting before the turn starts", before, budget)

	compacted := history
	if result, ok := e.compactHistory(ctx, history, budget); ok {
		compacted = result
	}
	if after := e.tokenEstimator.EstimateMessages(compacted); after > budget {
		// What survived is the keep-recent tail, which the cut point cannot
		// reach into any further: one stored turn larger than the budget.
		// Its tool outputs get what is left of three quarters of the budget,
		// so the trimmed history does not start the turn at the brim.
		toolBudget := max(budget*3/4-nonToolTokens(compacted, e), minToolResultTokens)
		if trimmed, ok := trimToolResultsToBudget(compacted, e.tokenEstimator, toolBudget); ok {
			logger.Infof(ctx, "[Agent][History] Trimmed tool outputs of the retained history: "+
				"%d → %d tokens (budget %d)", after, e.tokenEstimator.EstimateMessages(trimmed), budget)
			compacted = trimmed
		} else {
			logger.Warnf(ctx, "[Agent][History] Retained history is %d tokens, over the %d budget, "+
				"and has no tool output to trim; the turn starts with it as is", after, budget)
		}
	}
	return compacted
}

// compactHistory runs the turn-start compaction itself and reports whether
// it produced a smaller history.
func (e *AgentEngine) compactHistory(
	ctx context.Context, history []chat.Message, budget int,
) ([]chat.Message, bool) {
	if e.compactor == nil {
		return history, false
	}
	if err := e.checkAbbreviationTurn(ctx); err != nil {
		logger.Warnf(ctx, "[Agent][History] compaction skipped: abbreviation turn invalid: %v", err)
		return history, false
	}
	compactor := e.compactor.WithKeepRecentTokens(HistoryKeepRecentTokens(budget))
	result, err := compactor.Compact(ctx, history, compaction.ReasonHistoryBudget)
	if err != nil {
		if errors.Is(err, compaction.ErrNothingToCompact) {
			logger.Infof(ctx, "[Agent][History] Nothing outside the keep-recent budget to compact")
		} else {
			logger.Warnf(ctx, "[Agent][History] Turn-start compaction failed: %v", err)
		}
		return history, false
	}
	logger.Infof(ctx, "[Agent][History] Compacted (%s): %d → %d tokens, %d → %d messages "+
		"(split_turn=%v, degraded=%v, omitted=%d, keep_recent=%d)",
		result.Reason, result.TokensBefore, result.TokensAfter, result.MessagesBefore, result.MessagesAfter,
		result.SplitTurn, result.Degraded, result.Omitted, compactor.Settings().KeepRecentTokens)
	common.PipelineInfo(ctx, "Agent", "context_compacted", map[string]interface{}{
		"round":         0,
		"reason":        string(result.Reason),
		"tokens_before": result.TokensBefore,
		"tokens_after":  result.TokensAfter,
		"degraded":      result.Degraded,
	})
	e.emitContextCompacted(ctx, result, 0)
	e.saveContextCheckpoint(ctx, result.Checkpoint, 0)
	return result.Messages, true
}

// nonToolTokens is what messages cost apart from tool results, the part
// trimming tool outputs cannot reduce.
func nonToolTokens(messages []chat.Message, e *AgentEngine) int {
	total := 0
	for i := range messages {
		if messages[i].Role != "tool" {
			total += e.tokenEstimator.EstimateMessage(&messages[i])
		}
	}
	return total
}
