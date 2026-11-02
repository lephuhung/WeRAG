package service

import (
	"context"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// legalCandidatesClarification answers a document-assistant turn that
// names a legal document by a bare number the catalogue holds several of
// ("Nghị định 53") with the candidates card (tools.BuildLegalCandidates);
// the engine never starts and true is returned. A highlighted passage
// anchors the request elsewhere, so such turns are left alone.
func (s *sessionService) legalCandidatesClarification(ctx context.Context, req *types.QARequest, bus *event.EventBus) (bool, error) {
	if s.legalCatalog == nil || req == nil || req.Session == nil || bus == nil ||
		req.CustomAgent == nil || req.CustomAgent.ID != types.BuiltinDocumentAssistantID {
		return false, nil
	}
	if sel := req.DocumentSelection.Normalized(); sel != nil {
		return false, nil
	}
	card, err := tools.BuildLegalCandidates(ctx, s.legalCatalog, req.Query)
	if err != nil {
		logger.Warnf(ctx, "legal candidates: %v", err)
		return false, nil
	}
	if card == nil {
		return false, nil
	}
	logger.Infof(ctx, "Legal reference %q fits %d documents: answering with the candidates card", card.Reference.Text, card.Total)
	return true, emitPipelineCard(ctx, bus, req, tools.LegalCandidatesType, "legalpick-", card.Message(), card.Data())
}

// emitPipelineCard answers the turn with a card the pipeline made (no
// model ran): the payload as a tool result, the sentence as the final
// answer, and the completion carrying the step so the card renders from
// history. Same shape as EmitDocumentScopeClarification.
func emitPipelineCard(ctx context.Context, bus *event.EventBus, req *types.QARequest, toolName, idPrefix, answer string, data map[string]interface{}) error {
	sessionID := req.Session.ID
	data = withFinalMessage(data, answer)
	callID := types.PipelineToolCallIDPrefix + idPrefix + req.AssistantMessageID
	if err := bus.Emit(ctx, event.Event{Type: event.EventAgentToolResult, SessionID: sessionID,
		Data: event.AgentToolResultData{ToolCallID: callID, ToolName: toolName,
			Output: answer, Success: true, Data: data}}); err != nil {
		return err
	}
	if err := bus.Emit(ctx, event.Event{Type: event.EventAgentFinalAnswer, SessionID: sessionID,
		Data: event.AgentFinalAnswerData{Content: answer, Done: true}}); err != nil {
		return err
	}
	step := types.AgentStep{Timestamp: time.Now(), ToolCalls: []types.ToolCall{{
		ID: callID, Name: toolName, Args: map[string]interface{}{},
		Result: &types.ToolResult{Success: true, Output: answer, Data: data},
	}}}
	return bus.Emit(ctx, event.Event{Type: event.EventAgentComplete, SessionID: sessionID,
		Data: event.AgentCompleteData{SessionID: sessionID, MessageID: req.AssistantMessageID,
			FinalAnswer: answer, AgentSteps: []types.AgentStep{step}, TotalSteps: 1}})
}

// withFinalMessage returns a copy of a pipeline card's data carrying the
// fixed answer under "final_message" (agent.ToolResultFinalMessageKey), the
// key in-loop tools use for a code-written answer: a client or an eval can
// tell such an answer from a model-written one without a list of card
// names. data itself is not changed.
func withFinalMessage(data map[string]interface{}, answer string) map[string]interface{} {
	out := make(map[string]interface{}, len(data)+1)
	for k, v := range data {
		out[k] = v
	}
	if strings.TrimSpace(answer) != "" {
		out["final_message"] = answer
	}
	return out
}
