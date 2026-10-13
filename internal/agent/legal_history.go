package agent

import (
	"context"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// LegalHistoryFunc returns the amendment history of the legal provisions in
// the given retrieved chunks that the answer relies on — a Markdown block
// appended to the answer — or "" when there is none. It reads the
// amendment metadata stored at ingest, so the history does not depend on
// the model following a prompt rule.
type LegalHistoryFunc func(ctx context.Context, chunkIDs []string, answer string) string

// SetLegalHistory installs the amendment-history appender.
func (e *AgentEngine) SetLegalHistory(fn LegalHistoryFunc) {
	e.legalHistory = fn
}

// maxHistoryChunkIDs bounds the chunks inspected for one answer.
const maxHistoryChunkIDs = 200

// appendLegalHistory streams the amendment history after the answer, on the
// answer's own event so the stored message carries it too.
func (e *AgentEngine) appendLegalHistory(ctx context.Context, state *types.AgentState, sessionID, answerID string) {
	if e.legalHistory == nil || e.eventBus == nil || answerID == "" || state.FinalAnswer == "" {
		return
	}
	ids := toolResultChunkIDs(state.RoundSteps)
	if len(ids) == 0 {
		return
	}
	block := e.legalHistory(ctx, ids, state.FinalAnswer)
	if block == "" {
		return
	}
	logger.Infof(ctx, "[Agent] Appending legal amendment history (%d chars)", len(block))
	_ = e.eventBus.Emit(ctx, event.Event{
		ID:        answerID,
		Type:      event.EventAgentFinalAnswer,
		SessionID: sessionID,
		Data:      event.AgentFinalAnswerData{Content: block},
	})
	state.FinalAnswer += block
}

// toolResultChunkIDs collects, in order, the chunk ids of every row the
// turn's tools returned ("results" / "chunks" lists).
func toolResultChunkIDs(steps []types.AgentStep) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(row map[string]interface{}) {
		id, _ := row["chunk_id"].(string)
		if id != "" && !seen[id] && len(ids) < maxHistoryChunkIDs {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, step := range steps {
		for _, call := range step.ToolCalls {
			if call.Result == nil || !call.Result.Success {
				continue
			}
			for _, key := range []string{"results", "chunks"} {
				switch rows := call.Result.Data[key].(type) {
				case []map[string]interface{}:
					for _, r := range rows {
						add(r)
					}
				case []interface{}:
					for _, r := range rows {
						if m, ok := r.(map[string]interface{}); ok {
							add(m)
						}
					}
				}
			}
		}
	}
	return ids
}
