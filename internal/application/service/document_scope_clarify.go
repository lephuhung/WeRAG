package service

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
)

// documentScopeClarification runs the long-document gate of a
// document-assistant turn (see tools.DocumentScopeClarification) after the
// router and before the document text is built. When it asks, the turn is
// answered here and the engine never starts: true is returned.
func (s *sessionService) documentScopeClarification(ctx context.Context, req *types.QARequest, bus *event.EventBus) (bool, error) {
	if s.documentWorkspaces == nil || !s.documentWorkspaces.Enabled() || req == nil || req.Session == nil || bus == nil {
		return false, nil
	}
	disabled := req.CustomAgent != nil && !req.CustomAgent.Config.AskScopeForLongDocumentsEnabled()
	c := tools.DocumentScopeClarification(ctx, s.documentWorkspaces, req.Session.TenantID, req.Session.ID,
		tools.ScopeClarificationInput{Query: req.Query, Disabled: disabled})
	if c == nil {
		return false, nil
	}
	return true, EmitDocumentScopeClarification(ctx, bus, req, c)
}

// EmitDocumentScopeClarification answers the turn with the gate's question:
// the card payload as a tool result (a pipeline call the model never made,
// so history replays only the answer), the sentence as the final answer,
// and the completion carrying the step so the card renders from history.
func EmitDocumentScopeClarification(ctx context.Context, bus *event.EventBus, req *types.QARequest, c *tools.ScopeClarification) error {
	sessionID := req.Session.ID
	answer := c.Message()
	callID := types.PipelineToolCallIDPrefix + "docscope-" + req.AssistantMessageID
	data := c.Data()
	if err := bus.Emit(ctx, event.Event{Type: event.EventAgentToolResult, SessionID: sessionID,
		Data: event.AgentToolResultData{ToolCallID: callID, ToolName: tools.DocumentScopeClarificationType,
			Output: answer, Success: true, Data: data}}); err != nil {
		return err
	}
	if err := bus.Emit(ctx, event.Event{Type: event.EventAgentFinalAnswer, SessionID: sessionID,
		Data: event.AgentFinalAnswerData{Content: answer, Done: true}}); err != nil {
		return err
	}
	step := types.AgentStep{Timestamp: time.Now(), ToolCalls: []types.ToolCall{{
		ID: callID, Name: tools.DocumentScopeClarificationType, Args: map[string]interface{}{},
		Result: &types.ToolResult{Success: true, Output: answer, Data: data},
	}}}
	return bus.Emit(ctx, event.Event{Type: event.EventAgentComplete, SessionID: sessionID,
		Data: event.AgentCompleteData{SessionID: sessionID, MessageID: req.AssistantMessageID,
			FinalAnswer: answer, AgentSteps: []types.AgentStep{step}, TotalSteps: 1}})
}
