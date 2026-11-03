package service

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/docformat"
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
// judge is the labeling model that confirms the card the rules would show
// (tools.BroadQueryInput.Judge); without one no card is shown, since the
// rules alone carded greetings and short replies.
//
// agentMode says which stream the turn runs on: the agent engine's (a
// completion event closes the turn) or the knowledge-QA pipeline's (the
// final answer closes it, as the people-lookup quick answer does).
func (s *sessionService) broadQueryClarification(ctx context.Context, req *types.QARequest, bus *event.EventBus, agentMode bool, judge docformat.Completer) (bool, error) {
	if s.legalCatalog == nil || judge == nil || req == nil || req.Session == nil || bus == nil {
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
		tools.BroadQueryInput{Query: req.Query, Disabled: !s.inputGate().BroadQuestionGateEnabled(ctx), Judge: judge})
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
	data = withFinalMessage(data, answer)
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

// lazyCompleter resolves its model on the first call (one caller at a
// time: it is not safe for concurrent use): the knowledge-QA
// pipeline knows its chat model only after the gates, and the broad-question
// judge runs on few turns.
type lazyCompleter struct {
	// resolve gets the ctx of the first call, so its lookups share the
	// caller's budget
	resolve func(ctx context.Context) (docformat.Completer, error)
	model   docformat.Completer
	err     error
	done    bool
}

func (l *lazyCompleter) Complete(ctx context.Context, messages []docformat.Message) (string, error) {
	if !l.done {
		l.model, l.err = l.resolve(ctx)
		l.done = true
	}
	if l.err != nil {
		return "", l.err
	}
	return l.model.Complete(ctx, messages)
}

// knowledgeQAJudge is the labeling model of a knowledge-QA turn, resolved
// when first asked: the turn's chat model, or the intent model when one is
// set (sessionService.intentCompleter). Resolving repeats the pipeline's
// knowledge-base lookup (idempotent; its errors only mean no card here and
// are raised again by the pipeline).
func (s *sessionService) knowledgeQAJudge(ctx context.Context, req *types.QARequest) docformat.Completer {
	return &lazyCompleter{resolve: func(ctx context.Context) (docformat.Completer, error) {
		kbIDs, knowledgeIDs, err := s.resolveKnowledgeBases(ctx, req)
		if err != nil {
			return nil, err
		}
		modelID, err := s.resolveChatModelID(ctx, req, kbIDs, knowledgeIDs)
		if err != nil {
			return nil, err
		}
		if modelID == "" {
			return nil, errors.New("no chat model for the turn")
		}
		model, err := s.modelService.GetChatModel(ctx, modelID)
		if err != nil {
			return nil, err
		}
		return s.intentCompleter(ctx, model, modelID), nil
	}}
}
