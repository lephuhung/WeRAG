package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// inputGate builds the gate over the platform settings.
func (s *sessionService) inputGate() *inputGate {
	return newInputGate(s.systemSettings)
}

// broadQueryClarification answers a chat turn whose short generic
// question spans dozens of catalogue documents with the narrowing card
// (tools.BroadQueryClarification); the engine never starts and true is
// returned. The document assistant has its own gates; a turn with a file,
// an image or a highlighted passage is anchored by it and left alone.
//
// agentMode says which stream the turn runs on: the agent engine's (a
// completion event closes the turn) or the knowledge-QA pipeline's (the
// final answer closes it, as the people-lookup quick answer does).
func (s *sessionService) broadQueryClarification(ctx context.Context, req *types.QARequest, bus *event.EventBus, agentMode bool) (bool, error) {
	if s.legalCatalog == nil || req == nil || req.Session == nil || bus == nil {
		return false, nil
	}
	if req.CustomAgent != nil && req.CustomAgent.ID == types.BuiltinDocumentAssistantID {
		return false, nil
	}
	if len(req.Attachments) > 0 || len(req.ImageURLs) > 0 || req.ImageDescription != "" || req.QuotedContext != "" ||
		req.DocumentSelection.Normalized() != nil || len(req.MentionedDocumentIDs) > 0 {
		return false, nil
	}
	card := tools.BroadQueryClarification(ctx, s.legalCatalog, req.Session.ID,
		tools.BroadQueryInput{Query: req.Query, Disabled: !s.inputGate().BroadQuestionGateEnabled(ctx)})
	if card == nil {
		return false, nil
	}
	tools.MarkBroadQueryAsked(ctx, req.Session.ID, card)
	logger.Infof(ctx, "Broad question %q over %d catalogue documents: answering with the narrowing card", card.Topic, card.Total)
	if agentMode {
		return true, emitPipelineCard(ctx, bus, req, tools.BroadQueryClarificationType, "broadq-", card.Message(), card.Data())
	}
	return true, emitPipelineCardNormal(ctx, bus, req, tools.BroadQueryClarificationType, "broadq-", card.Message(), card.Data())
}

// emitPipelineCardNormal answers a knowledge-QA (non-agent) turn with a
// card the pipeline made: a tool call and its result carry the payload,
// then the sentence as the final answer and the done marker, the way the
// people-lookup quick answer ends such a turn.
func emitPipelineCardNormal(ctx context.Context, bus *event.EventBus, req *types.QARequest, toolName, idPrefix, answer string, data map[string]interface{}) error {
	sessionID := req.Session.ID
	// the quick-answer timeline recorder adds the pipeline prefix itself
	callID := idPrefix + req.AssistantMessageID
	if err := bus.Emit(ctx, event.Event{Type: event.EventAgentToolCall, SessionID: sessionID,
		Data: event.AgentToolCallData{ToolCallID: callID, ToolName: toolName, Arguments: map[string]interface{}{"query": req.Query}}}); err != nil {
		return err
	}
	if err := bus.Emit(ctx, event.Event{Type: event.EventAgentToolResult, SessionID: sessionID,
		Data: event.AgentToolResultData{ToolCallID: callID, ToolName: toolName, Output: answer, Success: true, Data: data}}); err != nil {
		return err
	}
	if err := bus.Emit(ctx, event.Event{Type: event.EventAgentFinalAnswer, SessionID: sessionID,
		Data: event.AgentFinalAnswerData{Content: answer}}); err != nil {
		return err
	}
	return bus.Emit(ctx, event.Event{Type: event.EventAgentFinalAnswer, SessionID: sessionID,
		Data: event.AgentFinalAnswerData{Done: true}})
}
