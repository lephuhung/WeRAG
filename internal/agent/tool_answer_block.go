package agent

import (
	"context"
	"fmt"
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
// them. Fixed "no evidence" answers get none. Reports whether a block
// was appended.
func (e *AgentEngine) appendToolAnswerBlocks(ctx context.Context, state *types.AgentState, sessionID, answerID string) bool {
	if e.eventBus == nil || answerID == "" || state == nil ||
		strings.TrimSpace(state.FinalAnswer) == "" || strings.HasPrefix(state.FinalAnswer, ungroundedAnswer) ||
		strings.Contains(state.FinalAnswer, types.ToolAnswerBlockMarker) {
		return false
	}
	block := toolAnswerBlocks(state.RoundSteps)
	if block == "" {
		return false
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
	return true
}

// toolAnswerBlocks joins the answer blocks of the turn's successful calls.
// A calculation is identified by its rendered block: calls of one tool
// that render the same block count once (the last), whatever the spelling
// of their arguments ("1" vs 1 vs 1.0); distinct blocks of one tool are
// all kept, numbered "Lần tính n/m". Tools come in the order of their last
// call.
func toolAnswerBlocks(steps []types.AgentStep) string {
	type entry struct {
		key   string
		block string
	}
	byTool := map[string][]entry{}
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
			key := block
			entries := byTool[call.Name]
			for i, e := range entries {
				if e.key == key {
					entries = append(entries[:i], entries[i+1:]...)
					break
				}
			}
			byTool[call.Name] = append(entries, entry{key: key, block: block})
			for i, name := range order {
				if name == call.Name {
					order = append(order[:i], order[i+1:]...)
					break
				}
			}
			order = append(order, call.Name)
		}
	}
	var parts []string
	for _, name := range order {
		entries := byTool[name]
		for i, e := range entries {
			if len(entries) > 1 {
				parts = append(parts, fmt.Sprintf("**Lần tính %d/%d**\n%s", i+1, len(entries), e.block))
			} else {
				parts = append(parts, e.block)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}
