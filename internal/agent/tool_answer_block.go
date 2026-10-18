package agent

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// appendToolAnswerBlocks streams, after the answer and before the domain
// footer, the answer blocks of the turn's tools (types.ToolDataAnswerBlock):
// for each tool the block of its last successful call, once. The model
// dropped the assumptions of compare_tax_methods and inverted its
// break-even reading (eval 2026-10-18); this block carries the computed
// figures, recommendation, assumptions and formulas as the tool produced
// them. Fixed "no evidence" answers get none.
func (e *AgentEngine) appendToolAnswerBlocks(ctx context.Context, state *types.AgentState, sessionID, answerID string) {
	if e.eventBus == nil || answerID == "" || state == nil ||
		strings.TrimSpace(state.FinalAnswer) == "" || strings.HasPrefix(state.FinalAnswer, ungroundedAnswer) ||
		strings.Contains(state.FinalAnswer, types.ToolAnswerBlockMarker) {
		return
	}
	block := toolAnswerBlocks(state.RoundSteps)
	if block == "" {
		return
	}
	block = types.ToolAnswerBlockMarker + block + "\n"
	logger.Infof(ctx, "[Agent] Appending tool answer block (%d chars)", len(block))
	_ = e.eventBus.Emit(ctx, event.Event{
		ID:        answerID,
		Type:      event.EventAgentFinalAnswer,
		SessionID: sessionID,
		Data:      event.AgentFinalAnswerData{Content: block},
	})
	state.FinalAnswer += block
}

// toolAnswerBlocks joins, per tool, the block of its last successful call,
// tools in the order of those calls.
func toolAnswerBlocks(steps []types.AgentStep) string {
	last := map[string]string{}
	var order []string
	for _, step := range steps {
		for _, call := range step.ToolCalls {
			if call.Result == nil || !call.Result.Success || call.Result.Data == nil {
				continue
			}
			block, _ := call.Result.Data[types.ToolDataAnswerBlock].(string)
			if block = strings.TrimSpace(block); block == "" {
				continue
			}
			if _, seen := last[call.Name]; seen {
				for i, name := range order {
					if name == call.Name {
						order = append(order[:i], order[i+1:]...)
						break
					}
				}
			}
			last[call.Name] = block
			order = append(order, call.Name)
		}
	}
	parts := make([]string, 0, len(order))
	for _, name := range order {
		parts = append(parts, last[name])
	}
	return strings.Join(parts, "\n\n")
}
